package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/duggan/bewitch/internal/store"
)

const (
	// maxHistorySpan caps the requested time range so an absurd start/end can't
	// force a full table + full-Parquet-union scan that holds a connection-pool
	// slot. Comfortably wider than any realistic retention window.
	maxHistorySpan = 400 * 24 * time.Hour
	// maxProcessNames bounds the ?names list on the process-history endpoint so a
	// huge comma-separated value (in the request line, outside the body cap) can't
	// build a multi-MB SQL string and placeholder/arg slice per request.
	maxProcessNames = 64
)

// querySource determines where to query data from based on archive configuration.
type querySource int

const (
	querySourceDuckDB querySource = iota
	querySourceParquet
	querySourceBoth
)

func sourceLabel(s querySource) string {
	switch s {
	case querySourceDuckDB:
		return "duckdb"
	case querySourceParquet:
		return "parquet"
	case querySourceBoth:
		return "both"
	default:
		return "unknown"
	}
}

// getQuerySource determines whether to query DuckDB, Parquet, or both.
// Returns querySourceDuckDB if no Parquet files exist (e.g., after unarchive).
func (s *Server) getQuerySource(start, end time.Time) querySource {
	if s.archivePath == "" || s.archiveThreshold == 0 {
		return querySourceDuckDB
	}
	if !s.hasAnyParquetFiles() {
		return querySourceDuckDB
	}
	archiveCutoff := time.Now().Add(-s.archiveThreshold)
	if start.After(archiveCutoff) || start.Equal(archiveCutoff) {
		return querySourceDuckDB
	}
	if end.Before(archiveCutoff) {
		return querySourceParquet
	}
	return querySourceBoth
}

// getQuerySourceForTable is like getQuerySource but downgrades to DuckDB-only
// when the specific table has no archived Parquet files. This prevents errors
// when some tables have been archived but others (e.g., newly added gpu_metrics)
// have not.
func (s *Server) getQuerySourceForTable(start, end time.Time, table string) querySource {
	source := s.getQuerySource(start, end)
	if source != querySourceDuckDB && !store.HasParquetFiles(s.archivePath, table) {
		return querySourceDuckDB
	}
	return source
}

// hasAnyParquetFiles returns true if any metric table has archived Parquet files.
func (s *Server) hasAnyParquetFiles() bool {
	for _, table := range archiveViewTables {
		if store.HasParquetFiles(s.archivePath, table) {
			return true
		}
	}
	return false
}

// archiveScan reads every archived Parquet file for table with the table's
// current schema (see store.ArchiveScan for why plain read_parquet breaks once
// a migration has added columns).
func (s *Server) archiveScan(table string) string {
	return store.ArchiveScan(table, store.ParquetLiteral(s.parquetPath(table)))
}

// archiveScanForRange is archiveScan limited to files overlapping [start, end].
func (s *Server) archiveScanForRange(table string, start, end time.Time) string {
	return store.ArchiveScan(table, s.parquetPathForRange(table, start, end))
}

// archiveScanFile reads a single archived Parquet file (the dimension snapshots).
func (s *Server) archiveScanFile(table, path string) string {
	return store.ArchiveScan(table, store.ParquetLiteral(path))
}

// parquetPath returns the glob path for a table's Parquet files.
func (s *Server) parquetPath(table string) string {
	return filepath.Join(s.archivePath, table, "*.parquet")
}

// parquetPathForRange returns a read_parquet()-compatible expression that
// only includes Parquet files whose date overlaps [start, end]. Files are
// named YYYY-MM-DD.parquet (daily) or YYYY-MM.parquet (monthly). If no files
// match, returns the glob path as a fallback (DuckDB handles empty globs).
//
// The returned string is already single-quoted for direct use in SQL, e.g.:
//
//	read_parquet(%s)  — NOT read_parquet('%s')
func (s *Server) parquetPathForRange(table string, start, end time.Time) string {
	dir := filepath.Join(s.archivePath, table)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "'" + s.parquetPath(table) + "'"
	}
	startDate := start.Truncate(24 * time.Hour)
	endDate := end.Truncate(24 * time.Hour).Add(24 * time.Hour) // inclusive

	var files []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".parquet" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".parquet")
		// Parse YYYY-MM-DD (daily) or YYYY-MM (monthly)
		var fileStart, fileEnd time.Time
		if t, err := time.Parse("2006-01-02", name); err == nil {
			fileStart = t
			fileEnd = t.Add(24 * time.Hour)
		} else if t, err := time.Parse("2006-01", name); err == nil {
			fileStart = t
			fileEnd = t.AddDate(0, 1, 0)
		} else {
			// Unknown format — include it to be safe
			files = append(files, filepath.Join(dir, e.Name()))
			continue
		}
		// Include file if its date range overlaps [start, end]
		if fileStart.Before(endDate) && fileEnd.After(startDate) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	if len(files) == 0 {
		return "'" + s.parquetPath(table) + "'"
	}
	if len(files) == 1 {
		return "'" + files[0] + "'"
	}
	// DuckDB read_parquet accepts a list: ['file1', 'file2']
	quoted := make([]string, len(files))
	for i, f := range files {
		quoted[i] = "'" + f + "'"
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// dimensionParquetPath returns the path to the dimension_values Parquet file.
func (s *Server) dimensionParquetPath() string {
	return filepath.Join(s.archivePath, "dimension_values.parquet")
}

// processInfoParquetPath returns the path to the process_info Parquet file.
func (s *Server) processInfoParquetPath() string {
	return filepath.Join(s.archivePath, "process_info.parquet")
}

// TimeSeriesPoint is a single data point in a time series.
type TimeSeriesPoint struct {
	TimestampNS int64   `json:"timestamp_ns"`
	Value       float64 `json:"value"`
}

// TimeSeries is a labeled sequence of time-series data points.
type TimeSeries struct {
	Label  string            `json:"label"`
	Points []TimeSeriesPoint `json:"points"`
}

func bucketInterval(start, end time.Time) string {
	d := end.Sub(start)
	switch {
	case d <= time.Hour:
		return "1 minute"
	case d <= 24*time.Hour:
		return "10 minutes"
	case d <= 7*24*time.Hour:
		return "1 hour"
	default:
		return "6 hours"
	}
}

func parseTimeRange(r *http.Request) (time.Time, time.Time) {
	now := time.Now()
	end := now
	start := now.Add(-7 * 24 * time.Hour)

	if v := r.URL.Query().Get("start"); v != "" {
		if sec, err := strconv.ParseInt(v, 10, 64); err == nil {
			start = time.Unix(sec, 0)
		}
	}
	if v := r.URL.Query().Get("end"); v != "" {
		if sec, err := strconv.ParseInt(v, 10, 64); err == nil {
			end = time.Unix(sec, 0)
		}
	}
	// Normalize attacker-supplied ranges: a reversed range would scan everything,
	// and an unbounded span forces a full table/Parquet scan. Swap if reversed and
	// clamp to maxHistorySpan.
	if end.Before(start) {
		start, end = end, start
	}
	if end.Sub(start) > maxHistorySpan {
		start = end.Add(-maxHistorySpan)
	}
	return start, end
}

func (s *Server) handleHistoryCPU(w http.ResponseWriter, r *http.Request) {
	if s.tryHistoryCache(r, w) {
		return
	}
	start, end := parseTimeRange(r)
	bucket := bucketInterval(start, end)
	source := s.getQuerySource(start, end)

	var query string
	var args []interface{}

	baseSelect := fmt.Sprintf(`SELECT time_bucket(INTERVAL '%s', ts) AS bucket,
		AVG(user_pct) AS user_avg,
		AVG(system_pct) AS system_avg,
		AVG(iowait_pct) AS iowait_avg`, bucket)
	baseWhere := "WHERE core = -1 AND ts BETWEEN ? AND ?"
	baseGroup := "GROUP BY bucket"

	switch source {
	case querySourceDuckDB:
		query = fmt.Sprintf(`%s FROM cpu_metrics %s %s ORDER BY bucket`, baseSelect, baseWhere, baseGroup)
		args = []interface{}{start, end}
	case querySourceParquet:
		query = fmt.Sprintf(`%s FROM %s %s %s ORDER BY bucket`,
			baseSelect, s.archiveScan("cpu_metrics"), baseWhere, baseGroup)
		args = []interface{}{start, end}
	case querySourceBoth:
		query = fmt.Sprintf(`%s FROM (
			SELECT ts, user_pct, system_pct, iowait_pct, core FROM cpu_metrics WHERE core = -1 AND ts BETWEEN ? AND ?
			UNION ALL
			SELECT ts, user_pct, system_pct, iowait_pct, core FROM %s WHERE core = -1 AND ts BETWEEN ? AND ?
		) %s ORDER BY bucket`, baseSelect, s.archiveScan("cpu_metrics"), baseGroup)
		args = []interface{}{start, end, start, end}
	}

	queryStart := time.Now()
	rows, err := s.dbFn().Query(query, args...)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var userSeries, sysSeries, ioSeries TimeSeries
	userSeries.Label = "cpu_user"
	sysSeries.Label = "cpu_system"
	ioSeries.Label = "cpu_iowait"

	for rows.Next() {
		var ts time.Time
		var userAvg, sysAvg, ioAvg float64
		if err := rows.Scan(&ts, &userAvg, &sysAvg, &ioAvg); err != nil {
			log.Debugf("history/cpu: scan error: %v", err)
			continue
		}
		ns := ts.UnixNano()
		userSeries.Points = append(userSeries.Points, TimeSeriesPoint{ns, userAvg})
		sysSeries.Points = append(sysSeries.Points, TimeSeriesPoint{ns, sysAvg})
		ioSeries.Points = append(ioSeries.Points, TimeSeriesPoint{ns, ioAvg})
	}

	if err := rows.Err(); err != nil {
		log.Debugf("history/cpu: row iteration error (results may be truncated): %v", err)
	}
	log.Debugf("history/cpu: %s source=%s rows=%d", time.Since(queryStart), sourceLabel(source), len(userSeries.Points))
	s.writeHistoryData(r, w, []TimeSeries{userSeries, sysSeries, ioSeries})
}

func (s *Server) handleHistoryMemory(w http.ResponseWriter, r *http.Request) {
	if s.tryHistoryCache(r, w) {
		return
	}
	start, end := parseTimeRange(r)
	bucket := bucketInterval(start, end)
	source := s.getQuerySource(start, end)

	var query string
	var args []interface{}

	baseSelect := fmt.Sprintf(`SELECT time_bucket(INTERVAL '%s', ts) AS bucket,
		AVG(CAST(used_bytes AS DOUBLE) / NULLIF(total_bytes, 0) * 100) AS used_pct,
		AVG(CAST(swap_used_bytes AS DOUBLE) / NULLIF(swap_total_bytes, 0) * 100) AS swap_pct`, bucket)
	baseWhere := "WHERE ts BETWEEN ? AND ?"
	baseGroup := "GROUP BY bucket"

	switch source {
	case querySourceDuckDB:
		query = fmt.Sprintf(`%s FROM memory_metrics %s %s ORDER BY bucket`, baseSelect, baseWhere, baseGroup)
		args = []interface{}{start, end}
	case querySourceParquet:
		query = fmt.Sprintf(`%s FROM %s %s %s ORDER BY bucket`,
			baseSelect, s.archiveScan("memory_metrics"), baseWhere, baseGroup)
		args = []interface{}{start, end}
	case querySourceBoth:
		query = fmt.Sprintf(`%s FROM (
			SELECT * FROM memory_metrics WHERE ts BETWEEN ? AND ?
			UNION ALL BY NAME
			SELECT * FROM %s WHERE ts BETWEEN ? AND ?
		) %s ORDER BY bucket`, baseSelect, s.archiveScan("memory_metrics"), baseGroup)
		args = []interface{}{start, end, start, end}
	}

	queryStart := time.Now()
	rows, err := s.dbFn().Query(query, args...)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var memSeries, swapSeries TimeSeries
	memSeries.Label = "mem_used_pct"
	swapSeries.Label = "swap_used_pct"

	for rows.Next() {
		var ts time.Time
		var usedPct float64
		var swapPct *float64
		if err := rows.Scan(&ts, &usedPct, &swapPct); err != nil {
			log.Debugf("history/memory: scan error: %v", err)
			continue
		}
		ns := ts.UnixNano()
		memSeries.Points = append(memSeries.Points, TimeSeriesPoint{ns, usedPct})
		swap := 0.0
		if swapPct != nil {
			swap = *swapPct
		}
		swapSeries.Points = append(swapSeries.Points, TimeSeriesPoint{ns, swap})
	}

	if err := rows.Err(); err != nil {
		log.Debugf("history/memory: row iteration error (results may be truncated): %v", err)
	}
	log.Debugf("history/memory: %s source=%s rows=%d", time.Since(queryStart), sourceLabel(source), len(memSeries.Points))
	s.writeHistoryData(r, w, []TimeSeries{memSeries, swapSeries})
}

// dimHistorySpec describes how to query dimension-keyed history data.
// Used by temperature, power, GPU, and disk handlers.
type dimHistorySpec struct {
	metric    string // log label, e.g. "temperature"
	table     string // e.g. "temperature_metrics"
	dimCat    string // dimension category, e.g. "sensor"
	dimFK     string // FK column, e.g. "sensor_id"
	aggExpr   string // aggregate expression, e.g. "AVG(m.temp_celsius)"
	unionCols string // columns for UNION ALL, e.g. "ts, sensor_id, temp_celsius"
	// perTable uses getQuerySourceForTable instead of getQuerySource.
	perTable bool
	// labelFn transforms the dimension value into a series label.
	// If nil, the dimension value is used as-is.
	labelFn func(dimValue string) string
}

// handleDimHistory executes a dimension-keyed history query and returns the
// result as sorted time series. Each row scans (bucket, dim_value, agg_value).
func (s *Server) handleDimHistory(w http.ResponseWriter, r *http.Request, spec dimHistorySpec) {
	if s.tryHistoryCache(r, w) {
		return
	}
	start, end := parseTimeRange(r)
	bucket := bucketInterval(start, end)

	var source querySource
	if spec.perTable {
		source = s.getQuerySourceForTable(start, end, spec.table)
	} else {
		source = s.getQuerySource(start, end)
	}

	selectFrag := fmt.Sprintf(`SELECT time_bucket(INTERVAL '%s', m.ts) AS bucket,
		d.value AS dim_label,
		%s AS agg_value`, bucket, spec.aggExpr)
	joinFrag := fmt.Sprintf(`JOIN dimension_values d ON d.category = '%s' AND d.id = m.%s`, spec.dimCat, spec.dimFK)
	whereFrag := "WHERE m.ts BETWEEN ? AND ?"
	groupFrag := "GROUP BY bucket, d.value ORDER BY bucket"

	var query string
	var args []interface{}

	switch source {
	case querySourceDuckDB:
		query = fmt.Sprintf(`%s FROM %s m %s %s %s`,
			selectFrag, spec.table, joinFrag, whereFrag, groupFrag)
		args = []interface{}{start, end}
	case querySourceParquet:
		pqJoin := fmt.Sprintf(`JOIN %s d ON d.category = '%s' AND d.id = m.%s`,
			s.archiveScanFile("dimension_values", s.dimensionParquetPath()), spec.dimCat, spec.dimFK)
		query = fmt.Sprintf(`%s FROM %s m %s %s %s`,
			selectFrag, s.archiveScan(spec.table), pqJoin, whereFrag, groupFrag)
		args = []interface{}{start, end}
	case querySourceBoth:
		query = fmt.Sprintf(`%s FROM (
			SELECT %s FROM %s WHERE ts BETWEEN ? AND ?
			UNION ALL
			SELECT %s FROM %s WHERE ts BETWEEN ? AND ?
		) m %s %s`,
			selectFrag,
			spec.unionCols, spec.table,
			spec.unionCols, s.archiveScan(spec.table),
			joinFrag, groupFrag)
		args = []interface{}{start, end, start, end}
	}

	queryStart := time.Now()
	rows, err := s.dbFn().Query(query, args...)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var rowCount int
	seriesMap := make(map[string]*TimeSeries)
	for rows.Next() {
		rowCount++
		var ts time.Time
		var dimValue string
		var aggValue *float64
		if err := rows.Scan(&ts, &dimValue, &aggValue); err != nil {
			log.Debugf("history/%s: scan error: %v", spec.metric, err)
			continue
		}
		label := dimValue
		if spec.labelFn != nil {
			label = spec.labelFn(dimValue)
		}
		ser, ok := seriesMap[label]
		if !ok {
			ser = &TimeSeries{Label: label}
			seriesMap[label] = ser
		}
		v := 0.0
		if aggValue != nil {
			v = *aggValue
		}
		ser.Points = append(ser.Points, TimeSeriesPoint{ts.UnixNano(), v})
	}

	series := make([]TimeSeries, 0, len(seriesMap))
	for _, ser := range seriesMap {
		series = append(series, *ser)
	}
	sort.Slice(series, func(i, j int) bool { return series[i].Label < series[j].Label })

	if err := rows.Err(); err != nil {
		log.Debugf("history/%s: row iteration error (results may be truncated): %v", spec.metric, err)
	}
	log.Debugf("history/%s: %s source=%s rows=%d series=%d", spec.metric, time.Since(queryStart), sourceLabel(source), rowCount, len(series))
	s.writeHistoryData(r, w, series)
}

func (s *Server) handleHistoryDisk(w http.ResponseWriter, r *http.Request) {
	s.handleDimHistory(w, r, dimHistorySpec{
		metric:    "disk",
		table:     "disk_metrics",
		dimCat:    "mount",
		dimFK:     "mount_id",
		aggExpr:   "AVG(CAST(m.used_bytes AS DOUBLE) / NULLIF(m.total_bytes, 0) * 100)",
		unionCols: "ts, mount_id, used_bytes, total_bytes",
		labelFn:   func(v string) string { return "disk_" + v },
	})
}

func (s *Server) handleHistoryTemperature(w http.ResponseWriter, r *http.Request) {
	s.handleDimHistory(w, r, dimHistorySpec{
		metric:    "temperature",
		table:     "temperature_metrics",
		dimCat:    "sensor",
		dimFK:     "sensor_id",
		aggExpr:   "AVG(m.temp_celsius)",
		unionCols: "ts, sensor_id, temp_celsius",
	})
}

func (s *Server) handleHistoryNetwork(w http.ResponseWriter, r *http.Request) {
	if s.tryHistoryCache(r, w) {
		return
	}
	start, end := parseTimeRange(r)
	bucket := bucketInterval(start, end)
	source := s.getQuerySource(start, end)

	var query string
	var args []interface{}

	selectFrag := fmt.Sprintf(`SELECT time_bucket(INTERVAL '%s', m.ts) AS bucket,
		d.value AS interface,
		AVG(m.rx_bytes_sec) AS rx_avg,
		AVG(m.tx_bytes_sec) AS tx_avg`, bucket)
	joinFrag := "JOIN dimension_values d ON d.category = 'interface' AND d.id = m.interface_id"
	whereFrag := "WHERE m.ts BETWEEN ? AND ?"
	groupFrag := "GROUP BY bucket, d.value ORDER BY bucket"

	switch source {
	case querySourceDuckDB:
		query = fmt.Sprintf(`%s FROM network_metrics m %s %s %s`,
			selectFrag, joinFrag, whereFrag, groupFrag)
		args = []interface{}{start, end}
	case querySourceParquet:
		pqJoin := fmt.Sprintf(`JOIN %s d ON d.category = 'interface' AND d.id = m.interface_id`,
			s.archiveScanFile("dimension_values", s.dimensionParquetPath()))
		query = fmt.Sprintf(`%s FROM %s m %s %s %s`,
			selectFrag, s.archiveScan("network_metrics"), pqJoin, whereFrag, groupFrag)
		args = []interface{}{start, end}
	case querySourceBoth:
		query = fmt.Sprintf(`%s FROM (
			SELECT ts, interface_id, rx_bytes_sec, tx_bytes_sec FROM network_metrics WHERE ts BETWEEN ? AND ?
			UNION ALL
			SELECT ts, interface_id, rx_bytes_sec, tx_bytes_sec FROM %s WHERE ts BETWEEN ? AND ?
		) m %s %s`,
			selectFrag, s.archiveScan("network_metrics"), joinFrag, groupFrag)
		args = []interface{}{start, end, start, end}
	}

	queryStart := time.Now()
	rows, err := s.dbFn().Query(query, args...)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var rowCount int
	seriesMap := make(map[string]*TimeSeries)
	for rows.Next() {
		rowCount++
		var ts time.Time
		var iface string
		var rxAvg, txAvg float64
		if err := rows.Scan(&ts, &iface, &rxAvg, &txAvg); err != nil {
			log.Debugf("history/network: scan error: %v", err)
			continue
		}
		ns := ts.UnixNano()
		rxLabel := iface + "_rx"
		txLabel := iface + "_tx"
		if _, ok := seriesMap[rxLabel]; !ok {
			seriesMap[rxLabel] = &TimeSeries{Label: rxLabel}
		}
		if _, ok := seriesMap[txLabel]; !ok {
			seriesMap[txLabel] = &TimeSeries{Label: txLabel}
		}
		seriesMap[rxLabel].Points = append(seriesMap[rxLabel].Points, TimeSeriesPoint{ns, rxAvg})
		seriesMap[txLabel].Points = append(seriesMap[txLabel].Points, TimeSeriesPoint{ns, txAvg})
	}

	series := make([]TimeSeries, 0, len(seriesMap))
	for _, ser := range seriesMap {
		series = append(series, *ser)
	}
	sort.Slice(series, func(i, j int) bool { return series[i].Label < series[j].Label })

	if err := rows.Err(); err != nil {
		log.Debugf("history/network: row iteration error (results may be truncated): %v", err)
	}
	log.Debugf("history/network: %s source=%s rows=%d series=%d", time.Since(queryStart), sourceLabel(source), rowCount, len(series))
	s.writeHistoryData(r, w, series)
}

func (s *Server) handleHistoryPower(w http.ResponseWriter, r *http.Request) {
	s.handleDimHistory(w, r, dimHistorySpec{
		metric:    "power",
		table:     "power_metrics",
		dimCat:    "zone",
		dimFK:     "zone_id",
		aggExpr:   "AVG(m.watts)",
		unionCols: "ts, zone_id, watts",
	})
}

func (s *Server) handleHistoryGPU(w http.ResponseWriter, r *http.Request) {
	s.handleDimHistory(w, r, dimHistorySpec{
		metric:    "gpu",
		table:     "gpu_metrics",
		dimCat:    "gpu",
		dimFK:     "gpu_id",
		aggExpr:   "AVG(m.utilization_pct)",
		unionCols: "ts, gpu_id, utilization_pct",
		perTable:  true,
	})
}

func (s *Server) handleHistoryProcess(w http.ResponseWriter, r *http.Request) {
	if s.tryHistoryCache(r, w) {
		return
	}
	start, end := parseTimeRange(r)
	bucket := bucketInterval(start, end)
	source := s.getQuerySource(start, end)

	// Optional: filter by specific process names instead of top-N by CPU.
	namesParam := r.URL.Query().Get("names")
	var filterNames []string
	if namesParam != "" {
		// Drop empties and cap the count: each name becomes a placeholder + bound
		// arg, and the value lives in the (uncapped) request line.
		for _, n := range strings.Split(namesParam, ",") {
			if n = strings.TrimSpace(n); n == "" {
				continue
			}
			filterNames = append(filterNames, n)
			if len(filterNames) >= maxProcessNames {
				break
			}
		}
	}

	query, args := s.buildProcessHistory(filterNames, start, end, bucket, source)

	queryStart := time.Now()
	rows, err := s.dbFn().Query(query, args...)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var rowCount int
	seriesMap := make(map[string]*TimeSeries)
	for rows.Next() {
		rowCount++
		var ts time.Time
		var name string
		var cpuAvg float64
		if err := rows.Scan(&ts, &name, &cpuAvg); err != nil {
			log.Debugf("history/process: scan error: %v", err)
			continue
		}
		label := name
		ser, ok := seriesMap[label]
		if !ok {
			ser = &TimeSeries{Label: label}
			seriesMap[label] = ser
		}
		ser.Points = append(ser.Points, TimeSeriesPoint{ts.UnixNano(), cpuAvg})
	}

	series := make([]TimeSeries, 0, len(seriesMap))
	for _, ser := range seriesMap {
		series = append(series, *ser)
	}
	// Sort by total CPU (highest first)
	sort.Slice(series, func(i, j int) bool {
		var sumI, sumJ float64
		for _, p := range series[i].Points {
			sumI += p.Value
		}
		for _, p := range series[j].Points {
			sumJ += p.Value
		}
		return sumI > sumJ
	})

	if err := rows.Err(); err != nil {
		log.Debugf("history/process: row iteration error (results may be truncated): %v", err)
	}
	log.Debugf("history/process: %s source=%s rows=%d series=%d", time.Since(queryStart), sourceLabel(source), rowCount, len(series))
	s.writeHistoryData(r, w, series)
}

// processHistoryTopN is how many process names the default (unfiltered)
// process history returns.
const processHistoryTopN = 10

// buildProcessHistory returns the query and args for process CPU history,
// grouped by process name. With names empty it returns the top
// processHistoryTopN names by total CPU over the window; otherwise just the
// named processes (pinned-process charts).
//
// Semantics, per (bucket, name): the summed CPU% of every instance of that name,
// averaged over all collection samples in the bucket. process_metrics stores
// only the enriched (top-N + pinned) processes each cycle, so a name absent
// from a sample contributed ~0 and is counted as 0 rather than skipped.
// Ranking by the SUM of those bucket values (∝ CPU-time used in the window)
// lets steady heavy processes outrank one-off bursts. The previous query
// ranked PIDs by the AVG of their bucket averages, so a short-lived PID seen in
// a single busy bucket outranked a process busy all day, and long ranges filled
// up with one-point series. Grouping by name (series are labelled by name)
// also merges repeated short-lived processes instead of emitting duplicate
// points per bucket. Names are resolved on (pid, start_time) so a reused PID
// can't inherit another process's name.
//
// Result columns: bucket, name, cpu_avg.
func (s *Server) buildProcessHistory(names []string, start, end time.Time, bucket string, source querySource) (string, []interface{}) {
	metrics := "process_metrics"
	info := "process_info"
	infoArchive := ""
	if _, err := os.Stat(s.processInfoParquetPath()); err == nil && source != querySourceDuckDB {
		infoArchive = s.archiveScanFile("process_info", s.processInfoParquetPath())
	}
	switch source {
	case querySourceParquet:
		metrics = s.archiveScanForRange("process_metrics", start, end)
	case querySourceBoth:
		metrics = fmt.Sprintf("(SELECT * FROM process_metrics UNION ALL BY NAME SELECT * FROM %s)",
			s.archiveScanForRange("process_metrics", start, end))
	}
	if infoArchive != "" {
		// Live process_info plus the archived snapshot, one name per (pid, start_time).
		info = fmt.Sprintf(`(SELECT DISTINCT ON (pid, start_time) pid, start_time, name
			FROM (SELECT pid, start_time, name, first_seen FROM process_info
				UNION ALL BY NAME
				SELECT pid, start_time, name, first_seen FROM %s)
			ORDER BY pid, start_time, first_seen DESC)`, infoArchive)
	}

	args := []interface{}{start, end}
	selectNames := fmt.Sprintf(`SELECT name FROM bucketed GROUP BY name ORDER BY SUM(cpu_avg) DESC LIMIT %d`, processHistoryTopN)
	if len(names) > 0 {
		placeholders := make([]string, len(names))
		for i, n := range names {
			placeholders[i] = "?"
			args = append(args, n)
		}
		selectNames = fmt.Sprintf(`SELECT DISTINCT name FROM bucketed WHERE name IN (%s)`, strings.Join(placeholders, ", "))
	}

	return fmt.Sprintf(`WITH samples AS (
			SELECT time_bucket(INTERVAL '%s', pm.ts) AS bucket, pm.ts,
				COALESCE(pi.name, CAST(pm.pid AS VARCHAR)) AS name,
				pm.cpu_user_pct + pm.cpu_system_pct AS cpu
			FROM %s pm
			LEFT JOIN %s pi ON pi.pid = pm.pid AND pi.start_time = pm.start_time
			WHERE pm.ts BETWEEN ? AND ?
		),
		bucket_samples AS (
			SELECT bucket, COUNT(DISTINCT ts) AS n FROM samples GROUP BY bucket
		),
		bucketed AS (
			SELECT sm.bucket, sm.name, SUM(sm.cpu) / bs.n AS cpu_avg
			FROM samples sm JOIN bucket_samples bs ON bs.bucket = sm.bucket
			GROUP BY sm.bucket, sm.name, bs.n
		),
		chosen AS (%s)
		SELECT b.bucket, b.name, b.cpu_avg
		FROM bucketed b JOIN chosen c ON c.name = b.name
		ORDER BY b.bucket`, bucket, metrics, info, selectNames), args
}

const historyCacheTTL = 10 * time.Second

// historyCacheKey returns a cache key that quantizes the start/end query
// parameters to the cache TTL so that requests with slightly different
// rolling timestamps (e.g. every 2s TUI tick) share a cache entry.
func historyCacheKey(r *http.Request) string {
	q := r.URL.Query()
	start := q.Get("start")
	end := q.Get("end")
	if start == "" && end == "" {
		return r.URL.Path
	}
	// Quantize timestamps to the TTL so rolling windows hit the same key.
	quantize := int64(historyCacheTTL.Seconds())
	if quantize < 1 {
		quantize = 1
	}
	var qs, qe string
	if s, err := strconv.ParseInt(start, 10, 64); err == nil {
		qs = strconv.FormatInt(s/quantize*quantize, 10)
	}
	if e, err := strconv.ParseInt(end, 10, 64); err == nil {
		qe = strconv.FormatInt(e/quantize*quantize, 10)
	}
	key := r.URL.Path + "?" + qs + ":" + qe
	if names := q.Get("names"); names != "" {
		key += ":" + names
	}
	// Custom history is keyed by source+metric on a shared path; without these
	// two different series would collide on the same cache entry.
	if source := q.Get("source"); source != "" {
		key += ":" + source + "/" + q.Get("metric")
	}
	return key
}

// tryHistoryCache serves a cached history response if one exists and is still valid.
// Uses the quantized cache key as an ETag for client-side change detection.
func (s *Server) tryHistoryCache(r *http.Request, w http.ResponseWriter) bool {
	key := historyCacheKey(r)
	s.historyCacheMu.RLock()
	entry, ok := s.historyCache[key]
	s.historyCacheMu.RUnlock()
	if ok && time.Now().Before(entry.expires) {
		if r.Header.Get("If-None-Match") == key {
			w.WriteHeader(http.StatusNotModified)
			return true
		}
		w.Header().Set("ETag", key)
		writeJSON(w, http.StatusOK, entry.data)
		return true
	}
	return false
}

func (s *Server) writeHistoryData(r *http.Request, w http.ResponseWriter, series []TimeSeries) {
	resp := HistoryResponse{Series: series}

	key := historyCacheKey(r)
	s.historyCacheMu.Lock()
	s.historyCache[key] = &historyCacheEntry{
		data:    resp,
		expires: time.Now().Add(historyCacheTTL),
	}
	s.historyCacheMu.Unlock()

	w.Header().Set("ETag", key)
	writeJSON(w, http.StatusOK, resp)
}
