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
		query = fmt.Sprintf(`%s FROM read_parquet('%s') %s %s ORDER BY bucket`,
			baseSelect, s.parquetPath("cpu_metrics"), baseWhere, baseGroup)
		args = []interface{}{start, end}
	case querySourceBoth:
		query = fmt.Sprintf(`%s FROM (
			SELECT ts, user_pct, system_pct, iowait_pct, core FROM cpu_metrics WHERE core = -1 AND ts BETWEEN ? AND ?
			UNION ALL
			SELECT ts, user_pct, system_pct, iowait_pct, core FROM read_parquet('%s') WHERE core = -1 AND ts BETWEEN ? AND ?
		) %s ORDER BY bucket`, baseSelect, s.parquetPath("cpu_metrics"), baseGroup)
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
	baseWhere := "WHERE ts BETWEEN to_timestamp(?) AND to_timestamp(?)"
	baseGroup := "GROUP BY bucket"

	switch source {
	case querySourceDuckDB:
		query = fmt.Sprintf(`%s FROM memory_metrics %s %s ORDER BY bucket`, baseSelect, baseWhere, baseGroup)
		args = []interface{}{start.Unix(), end.Unix()}
	case querySourceParquet:
		query = fmt.Sprintf(`%s FROM read_parquet('%s') %s %s ORDER BY bucket`,
			baseSelect, s.parquetPath("memory_metrics"), baseWhere, baseGroup)
		args = []interface{}{start.Unix(), end.Unix()}
	case querySourceBoth:
		query = fmt.Sprintf(`%s FROM (
			SELECT * FROM memory_metrics WHERE ts BETWEEN to_timestamp(?) AND to_timestamp(?)
			UNION ALL
			SELECT * FROM read_parquet('%s') WHERE ts BETWEEN to_timestamp(?) AND to_timestamp(?)
		) %s ORDER BY bucket`, baseSelect, s.parquetPath("memory_metrics"), baseGroup)
		args = []interface{}{start.Unix(), end.Unix(), start.Unix(), end.Unix()}
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
	whereFrag := "WHERE m.ts BETWEEN to_timestamp(?) AND to_timestamp(?)"
	groupFrag := "GROUP BY bucket, d.value ORDER BY bucket"

	var query string
	var args []interface{}

	switch source {
	case querySourceDuckDB:
		query = fmt.Sprintf(`%s FROM %s m %s %s %s`,
			selectFrag, spec.table, joinFrag, whereFrag, groupFrag)
		args = []interface{}{start.Unix(), end.Unix()}
	case querySourceParquet:
		pqJoin := fmt.Sprintf(`JOIN read_parquet('%s') d ON d.category = '%s' AND d.id = m.%s`,
			s.dimensionParquetPath(), spec.dimCat, spec.dimFK)
		query = fmt.Sprintf(`%s FROM read_parquet('%s') m %s %s %s`,
			selectFrag, s.parquetPath(spec.table), pqJoin, whereFrag, groupFrag)
		args = []interface{}{start.Unix(), end.Unix()}
	case querySourceBoth:
		query = fmt.Sprintf(`%s FROM (
			SELECT %s FROM %s WHERE ts BETWEEN to_timestamp(?) AND to_timestamp(?)
			UNION ALL
			SELECT %s FROM read_parquet('%s') WHERE ts BETWEEN to_timestamp(?) AND to_timestamp(?)
		) m %s %s`,
			selectFrag,
			spec.unionCols, spec.table,
			spec.unionCols, s.parquetPath(spec.table),
			joinFrag, groupFrag)
		args = []interface{}{start.Unix(), end.Unix(), start.Unix(), end.Unix()}
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
	whereFrag := "WHERE m.ts BETWEEN to_timestamp(?) AND to_timestamp(?)"
	groupFrag := "GROUP BY bucket, d.value ORDER BY bucket"

	switch source {
	case querySourceDuckDB:
		query = fmt.Sprintf(`%s FROM network_metrics m %s %s %s`,
			selectFrag, joinFrag, whereFrag, groupFrag)
		args = []interface{}{start.Unix(), end.Unix()}
	case querySourceParquet:
		pqJoin := fmt.Sprintf(`JOIN read_parquet('%s') d ON d.category = 'interface' AND d.id = m.interface_id`,
			s.dimensionParquetPath())
		query = fmt.Sprintf(`%s FROM read_parquet('%s') m %s %s %s`,
			selectFrag, s.parquetPath("network_metrics"), pqJoin, whereFrag, groupFrag)
		args = []interface{}{start.Unix(), end.Unix()}
	case querySourceBoth:
		query = fmt.Sprintf(`%s FROM (
			SELECT ts, interface_id, rx_bytes_sec, tx_bytes_sec FROM network_metrics WHERE ts BETWEEN to_timestamp(?) AND to_timestamp(?)
			UNION ALL
			SELECT ts, interface_id, rx_bytes_sec, tx_bytes_sec FROM read_parquet('%s') WHERE ts BETWEEN to_timestamp(?) AND to_timestamp(?)
		) m %s %s`,
			selectFrag, s.parquetPath("network_metrics"), joinFrag, groupFrag)
		args = []interface{}{start.Unix(), end.Unix(), start.Unix(), end.Unix()}
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
		filterNames = strings.Split(namesParam, ",")
	}

	var query string
	var args []interface{}

	if len(filterNames) > 0 {
		query, args = s.buildProcessHistoryByName(filterNames, start, end, bucket, source)
	} else {
		query, args = s.buildProcessHistoryTopCPU(start, end, bucket, source)
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
		var pid int32
		var name string
		var cpuAvg float64
		if err := rows.Scan(&ts, &pid, &name, &cpuAvg); err != nil {
			log.Debugf("history/process: scan error: %v", err)
			continue
		}
		// Use name as label (pid can change if process restarts)
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

	log.Debugf("history/process: %s source=%s rows=%d series=%d", time.Since(queryStart), sourceLabel(source), rowCount, len(series))
	s.writeHistoryData(r, w, series)
}

// buildProcessHistoryTopCPU returns the query and args for fetching top N
// processes by average CPU over the time range (the default behavior).
// Uses a single-scan pattern: bucket all PIDs first, then rank from the
// already-aggregated data to avoid scanning process_metrics twice.
func (s *Server) buildProcessHistoryTopCPU(start, end time.Time, bucket string, source querySource) (string, []interface{}) {
	switch source {
	case querySourceDuckDB:
		return fmt.Sprintf(`WITH bucketed AS (
			SELECT time_bucket(INTERVAL '%s', pm.ts) AS bucket, pm.pid,
				AVG(pm.cpu_user_pct + pm.cpu_system_pct) AS cpu_avg
			FROM process_metrics pm
			WHERE pm.ts BETWEEN to_timestamp(?) AND to_timestamp(?)
			GROUP BY bucket, pm.pid
		),
		pid_total AS (
			SELECT pid FROM bucketed GROUP BY pid ORDER BY AVG(cpu_avg) DESC LIMIT 10
		)
		SELECT b.bucket, b.pid,
			COALESCE(pi.name, CAST(b.pid AS VARCHAR)) AS name, b.cpu_avg
		FROM bucketed b
		JOIN pid_total pt ON b.pid = pt.pid
		LEFT JOIN (SELECT DISTINCT ON (pid) pid, name FROM process_info ORDER BY pid, first_seen DESC) pi
			ON b.pid = pi.pid
		ORDER BY b.bucket`, bucket),
			[]interface{}{start.Unix(), end.Unix()}
	case querySourceParquet:
		return fmt.Sprintf(`WITH bucketed AS (
			SELECT time_bucket(INTERVAL '%s', pm.ts) AS bucket, pm.pid,
				AVG(pm.cpu_user_pct + pm.cpu_system_pct) AS cpu_avg
			FROM read_parquet(%s) pm
			WHERE pm.ts BETWEEN to_timestamp(?) AND to_timestamp(?)
			GROUP BY bucket, pm.pid
		),
		pid_total AS (
			SELECT pid FROM bucketed GROUP BY pid ORDER BY AVG(cpu_avg) DESC LIMIT 10
		)
		SELECT b.bucket, b.pid,
			COALESCE(pi.name, CAST(b.pid AS VARCHAR)) AS name, b.cpu_avg
		FROM bucketed b
		JOIN pid_total pt ON b.pid = pt.pid
		LEFT JOIN (SELECT DISTINCT ON (pid) pid, name FROM read_parquet('%s') ORDER BY pid, first_seen DESC) pi
			ON b.pid = pi.pid
		ORDER BY b.bucket`, bucket, s.parquetPathForRange("process_metrics", start, end), s.processInfoParquetPath()),
			[]interface{}{start.Unix(), end.Unix()}
	default: // querySourceBoth
		// Aggregate each source independently then combine. This avoids
		// materializing all raw rows into an all_metrics CTE before
		// bucketing — each source produces far fewer pre-aggregated rows.
		return fmt.Sprintf(`WITH bucketed AS (
			SELECT time_bucket(INTERVAL '%s', ts) AS bucket, pid,
				AVG(cpu_user_pct + cpu_system_pct) AS cpu_avg
			FROM process_metrics
			WHERE ts BETWEEN to_timestamp(?) AND to_timestamp(?)
			GROUP BY bucket, pid
			UNION ALL
			SELECT time_bucket(INTERVAL '%s', ts) AS bucket, pid,
				AVG(cpu_user_pct + cpu_system_pct) AS cpu_avg
			FROM read_parquet(%s)
			WHERE ts BETWEEN to_timestamp(?) AND to_timestamp(?)
			GROUP BY bucket, pid
		),
		pid_total AS (
			SELECT pid FROM bucketed GROUP BY pid ORDER BY AVG(cpu_avg) DESC LIMIT 10
		)
		SELECT b.bucket, b.pid,
			COALESCE(pi.name, CAST(b.pid AS VARCHAR)) AS name, b.cpu_avg
		FROM bucketed b
		JOIN pid_total pt ON b.pid = pt.pid
		LEFT JOIN (SELECT DISTINCT ON (pid) pid, name FROM process_info ORDER BY pid, first_seen DESC) pi
			ON b.pid = pi.pid
		ORDER BY b.bucket`, bucket, bucket, s.parquetPathForRange("process_metrics", start, end)),
			[]interface{}{start.Unix(), end.Unix(), start.Unix(), end.Unix()}
	}
}

// buildProcessHistoryByName returns the query and args for fetching history
// for specific processes identified by name (used for pinned process charts).
func (s *Server) buildProcessHistoryByName(names []string, start, end time.Time, bucket string, source querySource) (string, []interface{}) {
	// Build SQL placeholders and args for the IN clause.
	placeholders := make([]string, len(names))
	nameArgs := make([]interface{}, len(names))
	for i, n := range names {
		placeholders[i] = "?"
		nameArgs[i] = n
	}
	inClause := strings.Join(placeholders, ", ")

	switch source {
	case querySourceDuckDB:
		args := append(nameArgs, start.Unix(), end.Unix())
		return fmt.Sprintf(`WITH target_pids AS (
			SELECT DISTINCT pid FROM process_info
			WHERE name IN (%s)
		),
		bucketed AS (
			SELECT time_bucket(INTERVAL '%s', pm.ts) AS bucket, pm.pid,
				AVG(pm.cpu_user_pct + pm.cpu_system_pct) AS cpu_avg
			FROM process_metrics pm
			WHERE pm.pid IN (SELECT pid FROM target_pids)
				AND pm.ts BETWEEN to_timestamp(?) AND to_timestamp(?)
			GROUP BY bucket, pm.pid
		)
		SELECT b.bucket, b.pid,
			COALESCE(pi.name, CAST(b.pid AS VARCHAR)) AS name, b.cpu_avg
		FROM bucketed b
		LEFT JOIN (SELECT DISTINCT ON (pid) pid, name FROM process_info ORDER BY pid, first_seen DESC) pi
			ON b.pid = pi.pid
		ORDER BY b.bucket`, inClause, bucket), args
	case querySourceParquet:
		args := append(nameArgs, start.Unix(), end.Unix())
		return fmt.Sprintf(`WITH target_pids AS (
			SELECT DISTINCT pid FROM read_parquet('%s')
			WHERE name IN (%s)
		),
		bucketed AS (
			SELECT time_bucket(INTERVAL '%s', pm.ts) AS bucket, pm.pid,
				AVG(pm.cpu_user_pct + pm.cpu_system_pct) AS cpu_avg
			FROM read_parquet(%s) pm
			WHERE pm.pid IN (SELECT pid FROM target_pids)
				AND pm.ts BETWEEN to_timestamp(?) AND to_timestamp(?)
			GROUP BY bucket, pm.pid
		)
		SELECT b.bucket, b.pid,
			COALESCE(pi.name, CAST(b.pid AS VARCHAR)) AS name, b.cpu_avg
		FROM bucketed b
		LEFT JOIN (SELECT DISTINCT ON (pid) pid, name FROM read_parquet('%s') ORDER BY pid, first_seen DESC) pi
			ON b.pid = pi.pid
		ORDER BY b.bucket`, s.processInfoParquetPath(), inClause, bucket, s.parquetPathForRange("process_metrics", start, end), s.processInfoParquetPath()), args
	default: // querySourceBoth
		// Aggregate each source independently then combine (same pattern
		// as buildProcessHistoryTopCPU — avoids all_metrics CTE).
		args := make([]interface{}, 0, len(nameArgs)+4)
		args = append(args, nameArgs...)
		args = append(args, start.Unix(), end.Unix(), start.Unix(), end.Unix())
		return fmt.Sprintf(`WITH target_pids AS (
			SELECT DISTINCT pid FROM process_info
			WHERE name IN (%s)
		),
		bucketed AS (
			SELECT time_bucket(INTERVAL '%s', ts) AS bucket, pid,
				AVG(cpu_user_pct + cpu_system_pct) AS cpu_avg
			FROM process_metrics
			WHERE ts BETWEEN to_timestamp(?) AND to_timestamp(?)
				AND pid IN (SELECT pid FROM target_pids)
			GROUP BY bucket, pid
			UNION ALL
			SELECT time_bucket(INTERVAL '%s', ts) AS bucket, pid,
				AVG(cpu_user_pct + cpu_system_pct) AS cpu_avg
			FROM read_parquet(%s)
			WHERE ts BETWEEN to_timestamp(?) AND to_timestamp(?)
				AND pid IN (SELECT pid FROM target_pids)
			GROUP BY bucket, pid
		)
		SELECT b.bucket, b.pid,
			COALESCE(pi.name, CAST(b.pid AS VARCHAR)) AS name, b.cpu_avg
		FROM bucketed b
		LEFT JOIN (SELECT DISTINCT ON (pid) pid, name FROM process_info ORDER BY pid, first_seen DESC) pi
			ON b.pid = pi.pid
		ORDER BY b.bucket`, inClause, bucket, bucket, s.parquetPathForRange("process_metrics", start, end)), args
	}
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
