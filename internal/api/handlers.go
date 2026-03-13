package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/duckdb/duckdb-go/v2"
	"github.com/ross/bewitch/internal/alert"
)

func truncateSQL(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(s.startTime).Seconds()
	interval := s.cfg.Daemon.DefaultInterval
	writeJSON(w, http.StatusOK,
		StatusResponse{Status: "ok", UptimeSec: uptime, DefaultInterval: interval, CollectorIntervals: s.collectorIntervals})
}

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	ackFilter := r.URL.Query().Get("ack")
	query := "SELECT id, ts, rule_name, severity, message, acknowledged FROM alerts ORDER BY ts DESC LIMIT 100"
	if ackFilter == "false" {
		query = "SELECT id, ts, rule_name, severity, message, acknowledged FROM alerts WHERE acknowledged = false ORDER BY ts DESC LIMIT 100"
	}

	queryStart := time.Now()
	rows, err := s.dbFn().Query(query)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var alerts []AlertMetric
	for rows.Next() {
		var a AlertMetric
		if err := rows.Scan(&a.ID, &a.Timestamp, &a.RuleName, &a.Severity, &a.Message, &a.Acknowledged); err != nil {
			continue
		}
		alerts = append(alerts, a)
	}
	if alerts == nil {
		alerts = []AlertMetric{}
	}
	log.Debugf("alerts: %s rows=%d", time.Since(queryStart), len(alerts))
	writeJSON(w, http.StatusOK, AlertsResponse{Alerts: alerts})
}

func (s *Server) handleAckAlert(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid id")
		return
	}

	result, err := s.dbFn().Exec("UPDATE alerts SET acknowledged = true WHERE id = ?", id)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeError(w, r, http.StatusNotFound, "alert not found")
		return
	}
	writeGenericStatus(w, http.StatusOK, "acknowledged")
}

func (s *Server) handleListAlertRules(w http.ResponseWriter, r *http.Request) {
	db := s.dbFn()

	// Query base rule info from the normalized table
	queryStart := time.Now()
	rows, err := db.Query(`SELECT id, name, type, severity, enabled FROM alert_rules ORDER BY id`)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var rules []AlertRuleMetric
	for rows.Next() {
		var rule AlertRuleMetric
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Type, &rule.Severity, &rule.Enabled); err != nil {
			continue
		}

		// Load type-specific fields from the appropriate table
		switch rule.Type {
		case "threshold":
			db.QueryRow(`SELECT metric, operator, value, duration,
				COALESCE(mount, ''), COALESCE(interface_name, ''), COALESCE(sensor, '')
				FROM alert_rule_threshold WHERE rule_id = ?`, rule.ID).Scan(
				&rule.Metric, &rule.Operator, &rule.Value, &rule.Duration,
				&rule.Mount, &rule.InterfaceName, &rule.Sensor)

		case "predictive":
			db.QueryRow(`SELECT metric, mount, predict_hours, threshold_pct
				FROM alert_rule_predictive WHERE rule_id = ?`, rule.ID).Scan(
				&rule.Metric, &rule.Mount, &rule.PredictHours, &rule.ThresholdPct)

		case "variance":
			db.QueryRow(`SELECT metric, delta_threshold, min_count, duration
				FROM alert_rule_variance WHERE rule_id = ?`, rule.ID).Scan(
				&rule.Metric, &rule.DeltaThreshold, &rule.MinCount, &rule.Duration)

		case "process_down":
			db.QueryRow(`SELECT process_name, COALESCE(process_pattern, ''),
				min_instances, check_duration
				FROM alert_rule_process_down WHERE rule_id = ?`, rule.ID).Scan(
				&rule.ProcessName, &rule.ProcessPattern, &rule.MinInstances, &rule.CheckDuration)

		case "process_thrashing":
			db.QueryRow(`SELECT process_name, COALESCE(process_pattern, ''),
				restart_threshold, restart_window
				FROM alert_rule_process_thrashing WHERE rule_id = ?`, rule.ID).Scan(
				&rule.ProcessName, &rule.ProcessPattern, &rule.RestartThreshold, &rule.RestartWindow)
		}

		rules = append(rules, rule)
	}
	if rules == nil {
		rules = []AlertRuleMetric{}
	}
	log.Debugf("alert-rules: %s rows=%d", time.Since(queryStart), len(rules))
	writeJSON(w, http.StatusOK, AlertRulesResponse{Rules: rules})
}

func (s *Server) handleCreateAlertRule(w http.ResponseWriter, r *http.Request) {
	var rule AlertRuleMetric
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if rule.Name == "" {
		writeError(w, r, http.StatusBadRequest, "name is required")
		return
	}

	db := s.dbFn()

	// Insert into base alert_rules table and get the ID
	result, err := db.Exec(`INSERT INTO alert_rules (name, type, severity) VALUES (?, ?, ?)`,
		rule.Name, rule.Type, rule.Severity)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	ruleID, err := result.LastInsertId()
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "failed to get rule id: "+err.Error())
		return
	}

	// Insert into the appropriate type-specific table
	switch rule.Type {
	case "threshold":
		_, err = db.Exec(`INSERT INTO alert_rule_threshold
			(rule_id, metric, operator, value, duration, mount, interface_name, sensor)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			ruleID, rule.Metric, rule.Operator, rule.Value,
			rule.Duration, rule.Mount, rule.InterfaceName, rule.Sensor)

	case "predictive":
		_, err = db.Exec(`INSERT INTO alert_rule_predictive
			(rule_id, metric, mount, predict_hours, threshold_pct)
			VALUES (?, ?, ?, ?, ?)`,
			ruleID, rule.Metric, rule.Mount, rule.PredictHours, rule.ThresholdPct)

	case "variance":
		_, err = db.Exec(`INSERT INTO alert_rule_variance
			(rule_id, metric, delta_threshold, min_count, duration)
			VALUES (?, ?, ?, ?, ?)`,
			ruleID, rule.Metric, rule.DeltaThreshold, rule.MinCount, rule.Duration)

	case "process_down":
		_, err = db.Exec(`INSERT INTO alert_rule_process_down
			(rule_id, process_name, process_pattern, min_instances, check_duration)
			VALUES (?, ?, ?, ?, ?)`,
			ruleID, rule.ProcessName, rule.ProcessPattern,
			rule.MinInstances, rule.CheckDuration)

	case "process_thrashing":
		_, err = db.Exec(`INSERT INTO alert_rule_process_thrashing
			(rule_id, process_name, process_pattern, restart_threshold, restart_window)
			VALUES (?, ?, ?, ?, ?)`,
			ruleID, rule.ProcessName, rule.ProcessPattern,
			rule.RestartThreshold, rule.RestartWindow)

	default:
		// Delete the base rule if type is unknown
		db.Exec("DELETE FROM alert_rules WHERE id = ?", ruleID)
		writeError(w, r, http.StatusBadRequest, "unknown rule type: "+rule.Type)
		return
	}

	if err != nil {
		// Rollback: delete the base rule
		db.Exec("DELETE FROM alert_rules WHERE id = ?", ruleID)
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	writeGenericStatus(w, http.StatusCreated, "created")
}

func (s *Server) handleDeleteAlertRule(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid id")
		return
	}
	db := s.dbFn()

	// Delete type-specific config (no FK constraints, so clean up explicitly)
	db.Exec("DELETE FROM alert_rule_threshold WHERE rule_id = ?", id)
	db.Exec("DELETE FROM alert_rule_predictive WHERE rule_id = ?", id)
	db.Exec("DELETE FROM alert_rule_variance WHERE rule_id = ?", id)
	db.Exec("DELETE FROM alert_rule_process_down WHERE rule_id = ?", id)
	db.Exec("DELETE FROM alert_rule_process_thrashing WHERE rule_id = ?", id)

	result, err := db.Exec("DELETE FROM alert_rules WHERE id = ?", id)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeError(w, r, http.StatusNotFound, "rule not found")
		return
	}
	writeGenericStatus(w, http.StatusOK, "deleted")
}

func (s *Server) handleToggleAlertRule(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid id")
		return
	}
	result, err := s.dbFn().Exec("UPDATE alert_rules SET enabled = NOT enabled WHERE id = ?", id)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeError(w, r, http.StatusNotFound, "rule not found")
		return
	}
	writeGenericStatus(w, http.StatusOK, "toggled")
}

func (s *Server) handleTestNotifications(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	var testAlert alert.Alert
	if len(body) > 0 {
		if err := json.Unmarshal(body, &testAlert); err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}
	if testAlert.RuleName == "" {
		testAlert.RuleName = "test"
		testAlert.Severity = "info"
		testAlert.Message = "Test notification from bewitch"
	}
	results, err := alert.SendTestNotifications(s.notifiers, &testAlert)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, NotifyTestResponse{Results: results})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg
	resp := ConfigResponse{
		Daemon: DaemonConfigResponse{
			Socket:          cfg.Daemon.Socket,
			DBPath:          cfg.Daemon.DBPath,
			DefaultInterval: cfg.Daemon.DefaultInterval,
		},
		Alerts: AlertsConfigResponse{
			EvaluationInterval: cfg.Alerts.EvaluationInterval,
		},
		TUI: TUIConfigResponse{
			RefreshInterval: cfg.TUI.RefreshInterval,
		},
	}
	for _, wh := range cfg.Alerts.Webhooks {
		resp.Alerts.Webhooks = append(resp.Alerts.Webhooks, WebhookDestResponse{
			URL:     wh.URL,
			Headers: wh.Headers,
		})
	}
	if resp.Alerts.Webhooks == nil {
		resp.Alerts.Webhooks = []WebhookDestResponse{}
	}
	for _, n := range cfg.Alerts.Ntfy {
		resp.Alerts.Ntfy = append(resp.Alerts.Ntfy, NtfyDestResponse{
			URL:   n.URL,
			Topic: n.Topic,
		})
	}
	for _, e := range cfg.Alerts.Email {
		resp.Alerts.Email = append(resp.Alerts.Email, EmailDestResponse{
			SMTPHost: e.SMTPHost,
			SMTPPort: e.GetSMTPPort(),
			From:     e.From,
			To:       e.To,
		})
	}
	for _, g := range cfg.Alerts.Gotify {
		resp.Alerts.Gotify = append(resp.Alerts.Gotify, GotifyDestResponse{
			URL: g.URL,
		})
	}
	for _, c := range cfg.Alerts.Commands {
		resp.Alerts.Commands = append(resp.Alerts.Commands, CommandDestResponse{
			Cmd: c.Cmd,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCompact(w http.ResponseWriter, r *http.Request) {
	if s.compactFn == nil {
		writeError(w, r, http.StatusServiceUnavailable, "compaction not available")
		return
	}
	if err := s.compactFn(); err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeGenericStatus(w, http.StatusOK, "ok")
}

func (s *Server) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	rows, err := s.dbFn().Query("SELECT key, value FROM preferences")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	prefs := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		prefs[k] = v
	}
	writeJSON(w, http.StatusOK, PreferencesResponse{Items: prefs})
}

func (s *Server) handleSetPreference(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Key == "" {
		writeError(w, r, http.StatusBadRequest, "key is required")
		return
	}
	_, err := s.dbFn().Exec("INSERT OR REPLACE INTO preferences (key, value) VALUES (?, ?)", req.Key, req.Value)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeGenericStatus(w, http.StatusOK, "ok")
}

func (s *Server) handleArchive(w http.ResponseWriter, r *http.Request) {
	if s.archiveFn == nil {
		writeError(w, r, http.StatusServiceUnavailable, "archiving not configured")
		return
	}
	if err := s.archiveFn(); err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeGenericStatus(w, http.StatusOK, "archive completed")
}

func (s *Server) handleUnarchive(w http.ResponseWriter, r *http.Request) {
	if s.unarchiveFn == nil {
		writeError(w, r, http.StatusServiceUnavailable, "archiving not configured")
		return
	}
	if err := s.unarchiveFn(); err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeGenericStatus(w, http.StatusOK, "unarchive completed")
}

func (s *Server) handleArchiveStatus(w http.ResponseWriter, r *http.Request) {
	resp := ArchiveStatusResponse{}

	// Get archive state from database
	if s.archiveStatusFn != nil {
		statuses, err := s.archiveStatusFn()
		if err == nil {
			resp.Tables = statuses
		}
	}
	if resp.Tables == nil {
		resp.Tables = []ArchiveStatusItem{}
	}

	// Get directory stats
	if s.archiveDirStatFn != nil {
		stats, err := s.archiveDirStatFn()
		if err == nil && stats != nil {
			resp.TotalFiles = stats.TotalFiles
			resp.TotalBytes = stats.TotalBytes
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SQL string `json:"sql"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, GenericResponse{Error: "invalid request body"})
		return
	}
	if req.SQL == "" {
		writeJSON(w, http.StatusBadRequest, GenericResponse{Error: "sql field is required"})
		return
	}
	if err := checkReadOnly(s.dbFn(), req.SQL); err != nil {
		writeJSON(w, http.StatusForbidden, QueryResponse{Error: err.Error()})
		return
	}

	queryStart := time.Now()
	rows, err := s.dbFn().Query(req.SQL)
	if err != nil {
		writeJSON(w, http.StatusOK, QueryResponse{Error: err.Error()})
		return
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		writeJSON(w, http.StatusOK, QueryResponse{Error: err.Error()})
		return
	}

	var data [][]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			writeJSON(w, http.StatusOK, QueryResponse{Error: err.Error()})
			return
		}
		// Convert to JSON-safe types (DuckDB driver may return
		// time.Time, big.Int, Decimal, etc. that don't marshal cleanly)
		for i, v := range values {
			values[i] = toJSONSafe(v)
		}
		data = append(data, values)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusOK, QueryResponse{Error: err.Error()})
		return
	}

	log.Debugf("query: %s rows=%d sql=%s", time.Since(queryStart), len(data), truncateSQL(req.SQL))
	writeJSON(w, http.StatusOK, QueryResponse{Columns: cols, Rows: data})
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	var req ExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ExportResponse{Error: "invalid request body"})
		return
	}
	if req.SQL == "" {
		writeJSON(w, http.StatusBadRequest, ExportResponse{Error: "sql field is required"})
		return
	}
	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, ExportResponse{Error: "path field is required"})
		return
	}
	if err := checkReadOnly(s.dbFn(), req.SQL); err != nil {
		writeJSON(w, http.StatusForbidden, ExportResponse{Error: err.Error()})
		return
	}
	if !filepath.IsAbs(req.Path) {
		writeJSON(w, http.StatusBadRequest, ExportResponse{Error: "path must be absolute"})
		return
	}
	if _, err := os.Stat(filepath.Dir(req.Path)); err != nil {
		writeJSON(w, http.StatusBadRequest, ExportResponse{Error: "parent directory does not exist"})
		return
	}

	// Infer format from extension if not specified
	format := strings.ToLower(req.Format)
	if format == "" {
		switch strings.ToLower(filepath.Ext(req.Path)) {
		case ".parquet":
			format = "parquet"
		case ".json":
			format = "json"
		default:
			format = "csv"
		}
	}

	// Build COPY statement
	options := fmt.Sprintf("FORMAT %s", format)
	if format == "parquet" {
		options += ", COMPRESSION zstd"
	} else if format == "csv" {
		options += ", HEADER"
	}
	copySQL := fmt.Sprintf("COPY (%s) TO '%s' (%s)", req.SQL, req.Path, options)

	queryStart := time.Now()
	result, err := s.dbFn().Exec(copySQL)
	if err != nil {
		writeJSON(w, http.StatusOK, ExportResponse{Error: err.Error()})
		return
	}

	rowCount, _ := result.RowsAffected()
	log.Debugf("export: %s rows=%d path=%s", time.Since(queryStart), rowCount, req.Path)
	writeJSON(w, http.StatusOK, ExportResponse{RowCount: rowCount, Path: req.Path})
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	var req SnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, SnapshotResponse{Error: "invalid request body"})
		return
	}
	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, SnapshotResponse{Error: "path field is required"})
		return
	}
	if !filepath.IsAbs(req.Path) {
		writeJSON(w, http.StatusBadRequest, SnapshotResponse{Error: "path must be absolute"})
		return
	}
	if _, err := os.Stat(filepath.Dir(req.Path)); err != nil {
		writeJSON(w, http.StatusBadRequest, SnapshotResponse{Error: "parent directory does not exist"})
		return
	}
	if _, err := os.Stat(req.Path); err == nil {
		writeJSON(w, http.StatusBadRequest, SnapshotResponse{Error: "output file already exists"})
		return
	}
	if s.snapshotFn == nil {
		writeError(w, r, http.StatusServiceUnavailable, "snapshot not available")
		return
	}
	if err := s.snapshotFn(req.Path, req.WithSystemTables); err != nil {
		writeJSON(w, http.StatusInternalServerError, SnapshotResponse{Error: err.Error()})
		return
	}
	var sizeBytes int64
	if info, err := os.Stat(req.Path); err == nil {
		sizeBytes = info.Size()
	}
	writeJSON(w, http.StatusOK, SnapshotResponse{Path: req.Path, SizeBytes: sizeBytes})
}

// toJSONSafe converts DuckDB driver types to JSON-serializable values.
// The driver may return time.Time, duckdb.Decimal (*big.Int), duckdb.Interval,
// and other types that encoding/json can't handle or produces unexpected output.
func toJSONSafe(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case time.Time:
		return val.Format("2006-01-02 15:04:05")
	case duckdb.Decimal:
		return val.Float64()
	case duckdb.Interval:
		return fmt.Sprintf("%dm%dd%dµs", val.Months, val.Days, val.Micros)
	case []byte:
		return string(val)
	case fmt.Stringer:
		return val.String()
	default:
		return v
	}
}
