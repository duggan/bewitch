package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// CustomMetric is one live numeric value from a custom HTTP source.
type CustomMetric struct {
	Source string  `json:"source"`
	Name   string  `json:"name"`
	Unit   string  `json:"unit,omitempty"`
	Value  float64 `json:"value"`
}

// CustomStatus is one live non-numeric field from a custom HTTP source.
type CustomStatus struct {
	Source string `json:"source"`
	Label  string `json:"label"`
	Value  string `json:"value"`
	Badge  string `json:"badge,omitempty"` // ""|ok|warn|crit
}

// CustomResponse is the body of GET /api/metrics/custom.
type CustomResponse struct {
	Metrics []CustomMetric `json:"metrics"`
	Status  []CustomStatus `json:"status"`
}

// CustomFieldInfo describes a declared metric or status field for the catalog.
type CustomFieldInfo struct {
	Name string `json:"name"`           // metric name or status label
	Unit string `json:"unit,omitempty"` // metrics only
}

// CustomSourceInfo is the static spec for one source — what the TUI renders the
// Services tab from. Contains no secrets (auth is never exposed over the API).
type CustomSourceInfo struct {
	Name    string            `json:"name"`
	Metrics []CustomFieldInfo `json:"metrics"`
	Status  []CustomFieldInfo `json:"status"`
}

// CustomCatalogResponse is the body of GET /api/custom/sources.
type CustomCatalogResponse struct {
	Sources []CustomSourceInfo `json:"sources"`
}

// SetCustomCatalog stores the static source catalog (set once at startup).
func (s *Server) SetCustomCatalog(sources []CustomSourceInfo) {
	s.customCatalog = sources
}

// SetCustomSnapshot merges one source's latest metrics and status into the
// cache. Each per-source collector pushes only its own rows, so this replaces
// that source's entries and leaves the others intact.
func (s *Server) SetCustomSnapshot(source string, metrics []CustomMetric, status []CustomStatus) {
	s.metricsMu.Lock()
	mc := s.metricsSnapshot
	if mc == nil {
		mc = &metricsCache{}
		s.metricsSnapshot = mc
	}
	mc.gen++
	if mc.custom == nil {
		mc.custom = make(map[string][]CustomMetric)
	}
	if mc.customStatus == nil {
		mc.customStatus = make(map[string][]CustomStatus)
	}
	mc.custom[source] = metrics
	mc.customStatus[source] = status
	mc.dash = nil
	s.metricsMu.Unlock()
}

// getCachedCustom flattens the per-source cache into stable, source-sorted slices.
func (s *Server) getCachedCustom() ([]CustomMetric, []CustomStatus, uint64) {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()
	mc := s.metricsSnapshot
	if mc == nil {
		return nil, nil, 0
	}
	seen := make(map[string]bool)
	var sources []string
	for src := range mc.custom {
		if !seen[src] {
			seen[src] = true
			sources = append(sources, src)
		}
	}
	for src := range mc.customStatus {
		if !seen[src] {
			seen[src] = true
			sources = append(sources, src)
		}
	}
	sort.Strings(sources)
	var metrics []CustomMetric
	var status []CustomStatus
	for _, src := range sources {
		metrics = append(metrics, mc.custom[src]...)
		status = append(status, mc.customStatus[src]...)
	}
	return metrics, status, mc.gen
}

func (s *Server) handleMetricsCustom(w http.ResponseWriter, r *http.Request) {
	metrics, status, gen := s.getCachedCustom()
	if metrics == nil && status == nil {
		// Live-only (like the process snapshot): nothing cached yet.
		writeJSON(w, http.StatusOK, CustomResponse{Metrics: []CustomMetric{}, Status: []CustomStatus{}})
		return
	}
	if metrics == nil {
		metrics = []CustomMetric{}
	}
	if status == nil {
		status = []CustomStatus{}
	}
	serveCached(w, r, CustomResponse{Metrics: metrics, Status: status}, gen)
}

func (s *Server) handleCustomCatalog(w http.ResponseWriter, r *http.Request) {
	sources := s.customCatalog
	if sources == nil {
		sources = []CustomSourceInfo{}
	}
	writeJSON(w, http.StatusOK, CustomCatalogResponse{Sources: sources})
}

// handleHistoryCustom returns a single time series for one source/metric. It
// cannot reuse handleDimHistory (that's dimension-FK keyed); custom_metrics
// denormalizes source/metric as VARCHAR, so the query is hand-written but mirrors
// the archive-aware preamble used by the other history handlers.
func (s *Server) handleHistoryCustom(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	metric := r.URL.Query().Get("metric")
	if source == "" || metric == "" {
		writeError(w, r, http.StatusBadRequest, "source and metric query params are required")
		return
	}
	if s.tryHistoryCache(r, w) {
		return
	}
	start, end := parseTimeRange(r)
	bucket := bucketInterval(start, end)
	src := s.getQuerySourceForTable(start, end, "custom_metrics")

	sel := fmt.Sprintf(`SELECT time_bucket(INTERVAL '%s', ts) AS bucket, AVG(value) AS v`, bucket)
	where := "WHERE source = ? AND metric = ? AND ts BETWEEN ? AND ?"
	group := "GROUP BY bucket"

	var query string
	var args []interface{}
	switch src {
	case querySourceParquet:
		query = fmt.Sprintf(`%s FROM read_parquet('%s') %s %s ORDER BY bucket`,
			sel, s.parquetPath("custom_metrics"), where, group)
		args = []interface{}{source, metric, start, end}
	case querySourceBoth:
		query = fmt.Sprintf(`%s FROM (
			SELECT ts, source, metric, value FROM custom_metrics WHERE source = ? AND metric = ? AND ts BETWEEN ? AND ?
			UNION ALL
			SELECT ts, source, metric, value FROM read_parquet('%s') WHERE source = ? AND metric = ? AND ts BETWEEN ? AND ?
		) %s ORDER BY bucket`, sel, s.parquetPath("custom_metrics"), group)
		args = []interface{}{source, metric, start, end, source, metric, start, end}
	default:
		query = fmt.Sprintf(`%s FROM custom_metrics %s %s ORDER BY bucket`, sel, where, group)
		args = []interface{}{source, metric, start, end}
	}

	rows, err := s.dbFn().Query(query, args...)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	defer logRowsErr("history/custom", rows)

	ser := TimeSeries{Label: metric}
	for rows.Next() {
		var ts time.Time
		var v sql.NullFloat64
		if err := rows.Scan(&ts, &v); err != nil {
			continue
		}
		ser.Points = append(ser.Points, TimeSeriesPoint{TimestampNS: ts.UnixNano(), Value: nf(v)})
	}
	s.writeHistoryData(r, w, []TimeSeries{ser})
}
