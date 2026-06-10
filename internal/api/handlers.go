package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/duckdb/duckdb-go/v2"
	"github.com/duggan/bewitch/internal/alert"
)

const (
	// queryTimeout bounds an ad-hoc /api/query or /api/export statement so a
	// runaway SELECT (cross join, generate_series, recursive CTE) can't peg CPU,
	// spill the disk, or hold a connection-pool slot indefinitely.
	queryTimeout = 30 * time.Second
	// maxQueryRows caps the rows /api/query materializes into memory before
	// encoding. Results buffer into a [][]any *outside* DuckDB's memory_limit, so
	// without this a wide SELECT could OOM the daemon on a small host.
	maxQueryRows = 1_000_000
	// minMaintenanceInterval rate-limits the heavyweight maintenance endpoints
	// (compact/archive/unarchive) so a caller can't loop them and keep the DB
	// perpetually rebuilding (which stalls writes and silently drops samples).
	minMaintenanceInterval = 30 * time.Second
)

// validRuleField reports whether s is free of control characters. Alert rule
// names and severities are caller-supplied and flow into the email notifier's
// subject line, so a newline would allow SMTP/header injection — reject it at
// the source in addition to sanitizing at send time.
func validRuleField(s string) bool {
	for _, r := range s {
		if r == '\n' || r == '\r' || r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// exportBaseDir returns the directory that /api/export and /api/snapshot output
// is confined to. It defaults to the database file's directory (bewitch-owned,
// not world-writable) and is overridable via [daemon] export_dir. An empty
// result means file output is unavailable.
func (s *Server) exportBaseDir() string {
	if s.cfg == nil {
		return ""
	}
	if s.cfg.Daemon.ExportDir != "" {
		return s.cfg.Daemon.ExportDir
	}
	if s.cfg.Daemon.DBPath != "" {
		return filepath.Dir(s.cfg.Daemon.DBPath)
	}
	return ""
}

// confineOutputPath validates that an export/snapshot destination is a new
// regular-file path inside baseDir. It resolves symlinks on baseDir and on the
// requested file's parent before the containment check, so neither a "../"
// traversal nor a pre-planted symlinked parent can redirect the write outside
// baseDir, and refuses to overwrite or write through an existing symlink. The
// returned path is absolute and cleaned.
func confineOutputPath(baseDir, requested string) (string, error) {
	if requested == "" {
		return "", fmt.Errorf("path field is required")
	}
	if !filepath.IsAbs(requested) {
		return "", fmt.Errorf("path must be absolute")
	}
	if baseDir == "" {
		return "", fmt.Errorf("file output is not configured (set [daemon] export_dir)")
	}
	base, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return "", fmt.Errorf("output directory %q unavailable", baseDir)
	}
	clean := filepath.Clean(requested)
	realParent, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil {
		return "", fmt.Errorf("parent directory does not exist")
	}
	rel, err := filepath.Rel(base, realParent)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path must be within %s", baseDir)
	}
	dest := filepath.Join(realParent, filepath.Base(clean))
	if fi, err := os.Lstat(dest); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("refusing to write through an existing symlink")
		}
		return "", fmt.Errorf("output file already exists")
	}
	return dest, nil
}

// beginMaintenance gates the heavyweight maintenance endpoints. It returns a
// non-zero HTTP status + message to refuse the request when another maintenance
// op is already running (409) or one finished too recently (429); otherwise it
// marks an op in flight and the caller must pair it with endMaintenance.
func (s *Server) beginMaintenance() (int, string) {
	s.maintMu.Lock()
	defer s.maintMu.Unlock()
	if s.maintRunning {
		return http.StatusConflict, "a maintenance operation is already in progress"
	}
	if !s.lastMaint.IsZero() && time.Since(s.lastMaint) < minMaintenanceInterval {
		return http.StatusTooManyRequests, "maintenance operations are rate-limited; try again shortly"
	}
	s.maintRunning = true
	return 0, ""
}

// endMaintenance clears the in-flight flag and records completion time (used by
// the minMaintenanceInterval rate limit).
func (s *Server) endMaintenance() {
	s.maintMu.Lock()
	s.maintRunning = false
	s.lastMaint = time.Now()
	s.maintMu.Unlock()
}

// queryErr maps a cancelled/timed-out ad-hoc query to a clear message, otherwise
// returns the driver error string.
func queryErr(ctx context.Context, err error) string {
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("query exceeded the %s time limit", queryTimeout)
	}
	return err.Error()
}

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
		StatusResponse{Status: "ok", Version: s.version, UptimeSec: uptime, DefaultInterval: interval, CollectorIntervals: s.collectorIntervals})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	resp := StatsResponse{
		Version:    s.version,
		UptimeSec:  time.Since(s.startTime).Seconds(),
		Database:   DatabaseStats{Path: s.cfg.Daemon.DBPath},
		Dimensions: map[string]int64{},
		Tables:     []TableStats{},
		Collectors: s.collectorIntervals,
	}

	if info, err := os.Stat(s.cfg.Daemon.DBPath); err == nil {
		resp.Database.SizeBytes = info.Size()
	}
	if info, err := os.Stat(s.cfg.Daemon.DBPath + ".wal"); err == nil {
		resp.Database.WALBytes = info.Size()
	}

	if s.statsFn != nil {
		core, err := s.statsFn()
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if core != nil {
			resp.Tables = core.Tables
			if core.Dimensions != nil {
				resp.Dimensions = core.Dimensions
			}
			resp.Processes = core.Processes
			resp.Alerts = core.Alerts
		}
	}

	// Compute live-DB coverage from per-table min/max
	var oldest, newest int64
	for _, t := range resp.Tables {
		if t.OldestTs > 0 && (oldest == 0 || t.OldestTs < oldest) {
			oldest = t.OldestTs
		}
		if t.NewestTs > newest {
			newest = t.NewestTs
		}
	}

	// Archive section + extend coverage backwards using oldest archive file date
	if s.cfg.Daemon.ArchivePath != "" && s.archiveDirStatFn != nil {
		dirStats, err := s.archiveDirStatFn()
		if err == nil && dirStats != nil {
			resp.Archive = &ArchiveStats{
				Path:       s.cfg.Daemon.ArchivePath,
				FileCount:  dirStats.TotalFiles,
				TotalBytes: dirStats.TotalBytes,
			}
			for _, t := range dirStats.Tables {
				if t.OldestFile == "" {
					continue
				}
				d, err := time.Parse("2006-01-02", t.OldestFile)
				if err != nil {
					continue
				}
				ts := d.UnixNano()
				if oldest == 0 || ts < oldest {
					oldest = ts
				}
			}
		}
	}

	resp.Coverage.OldestTs = oldest
	resp.Coverage.NewestTs = newest
	if oldest > 0 && newest > oldest {
		resp.Coverage.SpanSeconds = float64(newest-oldest) / float64(time.Second)
	}

	if s.selfStatsFn != nil {
		self := s.selfStatsFn()
		resp.Self = &self
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	ackFilter := r.URL.Query().Get("ack")
	query := "SELECT id, ts, rule_name, severity, message, acknowledged, resolved_at IS NOT NULL FROM alerts ORDER BY ts DESC LIMIT 100"
	if ackFilter == "false" {
		// "Active" alerts are unacknowledged AND still firing — a resolved alert
		// is no longer active even if it was never acked.
		query = "SELECT id, ts, rule_name, severity, message, acknowledged, resolved_at IS NOT NULL FROM alerts WHERE acknowledged = false AND resolved_at IS NULL ORDER BY ts DESC LIMIT 100"
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
		if err := rows.Scan(&a.ID, &a.Timestamp, &a.RuleName, &a.Severity, &a.Message, &a.Acknowledged, &a.Resolved); err != nil {
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

// handleDeleteAlert deletes a single fired alert row by id. This is a bounded
// maintenance operation — it can only remove a row from the `alerts` table
// (notification history), never arbitrary data — so it is safe to expose on the
// unauthenticated unix socket (unlike general write SQL). It lets the TUI clear
// stuck/obsolete fired alerts (e.g. the reserved collection-stalled alert, or old
// alerts on a rule the user wants to keep) without deleting the rule or taking the
// daemon offline. Lifecycle-safe: if the rule is still breaching, the engine
// re-fires on the next cycle (correct); if the condition cleared, it stays gone.
func (s *Server) handleDeleteAlert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid id")
		return
	}
	result, err := s.dbFn().Exec("DELETE FROM alerts WHERE id = ?", id)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeError(w, r, http.StatusNotFound, "alert not found")
		return
	}
	writeGenericStatus(w, http.StatusOK, "deleted")
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

		// Load type-specific fields from the appropriate table. A missing or
		// unreadable config row leaves the rule half-populated; log it (the rule
		// still exists) instead of silently returning blank fields — this mirrors
		// the engine's behavior of surfacing orphaned rules rather than skipping
		// them silently.
		var cfgErr error
		switch rule.Type {
		case "threshold":
			cfgErr = db.QueryRow(`SELECT metric, operator, value, duration,
				COALESCE(mount, ''), COALESCE(interface_name, ''), COALESCE(sensor, ''),
				COALESCE(aggregate, 'avg')
				FROM alert_rule_threshold WHERE rule_id = ?`, rule.ID).Scan(
				&rule.Metric, &rule.Operator, &rule.Value, &rule.Duration,
				&rule.Mount, &rule.InterfaceName, &rule.Sensor, &rule.Aggregate)

		case "predictive":
			cfgErr = db.QueryRow(`SELECT metric, mount, predict_hours, threshold_pct
				FROM alert_rule_predictive WHERE rule_id = ?`, rule.ID).Scan(
				&rule.Metric, &rule.Mount, &rule.PredictHours, &rule.ThresholdPct)

		case "variance":
			cfgErr = db.QueryRow(`SELECT metric, delta_threshold, min_count, duration
				FROM alert_rule_variance WHERE rule_id = ?`, rule.ID).Scan(
				&rule.Metric, &rule.DeltaThreshold, &rule.MinCount, &rule.Duration)

		case "process_down":
			cfgErr = db.QueryRow(`SELECT process_name, COALESCE(process_pattern, ''),
				min_instances, check_duration
				FROM alert_rule_process_down WHERE rule_id = ?`, rule.ID).Scan(
				&rule.ProcessName, &rule.ProcessPattern, &rule.MinInstances, &rule.CheckDuration)

		case "process_thrashing":
			cfgErr = db.QueryRow(`SELECT process_name, COALESCE(process_pattern, ''),
				restart_threshold, restart_window
				FROM alert_rule_process_thrashing WHERE rule_id = ?`, rule.ID).Scan(
				&rule.ProcessName, &rule.ProcessPattern, &rule.RestartThreshold, &rule.RestartWindow)
		}
		if cfgErr != nil {
			log.Warnf("alert-rule %d (%q, type %s): loading config: %v", rule.ID, rule.Name, rule.Type, cfgErr)
		}

		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		log.Warnf("alert-rules: row iteration error (results may be truncated): %v", err)
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
	if !validRuleField(rule.Name) || !validRuleField(rule.Severity) {
		writeError(w, r, http.StatusBadRequest, "name and severity must not contain control characters")
		return
	}

	db := s.dbFn()

	// Rule names must be unique: fired alerts, the engine's debounce, and the delete-time
	// cleanup all key on rule_name, so two rules sharing a name corrupt each other.
	var dup int
	if err := db.QueryRow("SELECT count(*) FROM alert_rules WHERE name = ?", rule.Name).Scan(&dup); err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if dup > 0 {
		writeError(w, r, http.StatusConflict, "a rule named "+rule.Name+" already exists")
		return
	}

	// Insert into base alert_rules table and get the ID. Use RETURNING rather than
	// LastInsertId(): the DuckDB driver does not support LastInsertId() and returns 0,
	// which would write the type-specific config rows with rule_id=0 and orphan them
	// from the sequence-assigned base rule id (so the engine's JOIN never matches).
	var ruleID int64
	err := db.QueryRow(`INSERT INTO alert_rules (name, type, severity) VALUES (?, ?, ?) RETURNING id`,
		rule.Name, rule.Type, rule.Severity).Scan(&ruleID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	// Insert into the appropriate type-specific table
	switch rule.Type {
	case "threshold":
		agg := rule.Aggregate
		if agg == "" {
			agg = "avg"
		}
		_, err = db.Exec(`INSERT INTO alert_rule_threshold
			(rule_id, metric, operator, value, duration, mount, interface_name, sensor, aggregate)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ruleID, rule.Metric, rule.Operator, rule.Value,
			rule.Duration, rule.Mount, rule.InterfaceName, rule.Sensor, agg)

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

// handleUpdateAlertRule updates an existing rule's name, severity, and type-specific
// config in place. The rule's type is locked (changing it would mean swapping config
// tables); enabled stays under /toggle and created_at is preserved. All writes run in a
// single transaction so a partial failure leaves the rule untouched.
func (s *Server) handleUpdateAlertRule(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid id")
		return
	}

	var rule AlertRuleMetric
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if rule.Name == "" {
		writeError(w, r, http.StatusBadRequest, "name is required")
		return
	}
	if !validRuleField(rule.Name) || !validRuleField(rule.Severity) {
		writeError(w, r, http.StatusBadRequest, "name and severity must not contain control characters")
		return
	}

	tx, err := s.dbFn().Begin()
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()

	// The type is immutable: load the stored type (and current name) and reject a type
	// mismatch. This guarantees the per-type config row already exists, so a plain UPDATE
	// is always correct.
	var storedType, oldName string
	if err := tx.QueryRow("SELECT type, name FROM alert_rules WHERE id = ?", id).Scan(&storedType, &oldName); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, r, http.StatusNotFound, "rule not found")
			return
		}
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if rule.Type != "" && rule.Type != storedType {
		writeError(w, r, http.StatusBadRequest, "rule type is immutable (delete and recreate to change it)")
		return
	}

	// Names stay unique (see create); reject a rename onto another rule's name.
	if rule.Name != oldName {
		var dup int
		if err := tx.QueryRow("SELECT count(*) FROM alert_rules WHERE name = ? AND id != ?", rule.Name, id).Scan(&dup); err != nil {
			writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if dup > 0 {
			writeError(w, r, http.StatusConflict, "a rule named "+rule.Name+" already exists")
			return
		}
	}

	if _, err := tx.Exec(`UPDATE alert_rules SET name = ?, severity = ? WHERE id = ?`,
		rule.Name, rule.Severity, id); err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	// Fired alerts are linked to the rule by name; carry them across a rename so they
	// stay attributable (and the delete-time cleanup can still find them).
	if rule.Name != oldName {
		if _, err := tx.Exec("UPDATE alerts SET rule_name = ? WHERE rule_name = ?", rule.Name, oldName); err != nil {
			writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
	}

	switch storedType {
	case "threshold":
		agg := rule.Aggregate
		if agg == "" {
			agg = "avg"
		}
		_, err = tx.Exec(`UPDATE alert_rule_threshold SET
			metric = ?, operator = ?, value = ?, duration = ?, mount = ?, interface_name = ?, sensor = ?, aggregate = ?
			WHERE rule_id = ?`,
			rule.Metric, rule.Operator, rule.Value, rule.Duration,
			rule.Mount, rule.InterfaceName, rule.Sensor, agg, id)

	case "predictive":
		_, err = tx.Exec(`UPDATE alert_rule_predictive SET
			metric = ?, mount = ?, predict_hours = ?, threshold_pct = ?
			WHERE rule_id = ?`,
			rule.Metric, rule.Mount, rule.PredictHours, rule.ThresholdPct, id)

	case "variance":
		_, err = tx.Exec(`UPDATE alert_rule_variance SET
			metric = ?, delta_threshold = ?, min_count = ?, duration = ?
			WHERE rule_id = ?`,
			rule.Metric, rule.DeltaThreshold, rule.MinCount, rule.Duration, id)

	case "process_down":
		_, err = tx.Exec(`UPDATE alert_rule_process_down SET
			process_name = ?, process_pattern = ?, min_instances = ?, check_duration = ?
			WHERE rule_id = ?`,
			rule.ProcessName, rule.ProcessPattern, rule.MinInstances, rule.CheckDuration, id)

	case "process_thrashing":
		_, err = tx.Exec(`UPDATE alert_rule_process_thrashing SET
			process_name = ?, process_pattern = ?, restart_threshold = ?, restart_window = ?
			WHERE rule_id = ?`,
			rule.ProcessName, rule.ProcessPattern, rule.RestartThreshold, rule.RestartWindow, id)

	default:
		writeError(w, r, http.StatusInternalServerError, "unknown rule type: "+storedType)
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeGenericStatus(w, http.StatusOK, "updated")
}

func (s *Server) handleDeleteAlertRule(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid id")
		return
	}
	tx, err := s.dbFn().Begin()
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()

	// Resolve the rule name first so we can clean up its fired alerts. Fired alerts are
	// linked to a rule by rule_name (not id), and would otherwise linger forever as
	// unacknowledged "active" alerts for a rule the user can no longer see or manage.
	var name string
	if err := tx.QueryRow("SELECT name FROM alert_rules WHERE id = ?", id).Scan(&name); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, r, http.StatusNotFound, "rule not found")
			return
		}
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	// Delete type-specific config (no FK constraints, so clean up explicitly).
	for _, t := range []string{
		"alert_rule_threshold", "alert_rule_predictive", "alert_rule_variance",
		"alert_rule_process_down", "alert_rule_process_thrashing",
	} {
		if _, err := tx.Exec("DELETE FROM "+t+" WHERE rule_id = ?", id); err != nil {
			writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Remove the rule's fired alerts so they don't remain as orphaned, undismissable
	// active alerts after the rule is gone.
	if _, err := tx.Exec("DELETE FROM alerts WHERE rule_name = ?", name); err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if _, err := tx.Exec("DELETE FROM alert_rules WHERE id = ?", id); err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
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
	// Deliberately ignore any caller-supplied payload. This endpoint is reachable
	// unauthenticated on the local socket and triggers real delivery to every
	// configured notifier (email/command/shoutrrr). Letting the caller set the
	// alert fields would (a) feed attacker-controlled values into the command
	// notifier's BEWITCH_* env vars and (b) let it spray forged alerts to the
	// operator's external channels. A fixed canned alert removes both vectors.
	testAlert := alert.Alert{
		RuleName: "test",
		Severity: "info",
		Message:  "Test notification from bewitch",
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
	// Redact details that aid abuse or are PII. /api/config is readable by any
	// local socket client; the configured command line directly aids abuse of the
	// command notifier, and From/To are addresses. Expose only enough to confirm a
	// notifier of each kind is configured.
	for _, e := range cfg.Alerts.Email {
		resp.Alerts.Email = append(resp.Alerts.Email, EmailDestResponse{
			UseMailCmd: e.UseMailCmd,
			SMTPHost:   e.SMTPHost,
			SMTPPort:   e.GetSMTPPort(),
		})
	}
	for range cfg.Alerts.Commands {
		resp.Alerts.Commands = append(resp.Alerts.Commands, CommandDestResponse{
			Cmd: "[redacted]",
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCompact(w http.ResponseWriter, r *http.Request) {
	if s.compactFn == nil {
		writeError(w, r, http.StatusServiceUnavailable, "compaction not available")
		return
	}
	if code, msg := s.beginMaintenance(); code != 0 {
		writeError(w, r, code, msg)
		return
	}
	defer s.endMaintenance()
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
	if code, msg := s.beginMaintenance(); code != 0 {
		writeError(w, r, code, msg)
		return
	}
	defer s.endMaintenance()
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
	if code, msg := s.beginMaintenance(); code != 0 {
		writeError(w, r, code, msg)
		return
	}
	defer s.endMaintenance()
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

	// Bound the statement: cancel a runaway query at queryTimeout (also cancels if
	// the client disconnects, since the request context is the parent).
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	queryStart := time.Now()
	rows, err := s.dbFn().QueryContext(ctx, req.SQL)
	if err != nil {
		writeJSON(w, http.StatusOK, QueryResponse{Error: queryErr(ctx, err)})
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
		// Cap the materialized result so a wide SELECT can't OOM the daemon: the
		// rows buffer into Go memory, outside DuckDB's memory_limit.
		if len(data) >= maxQueryRows {
			writeJSON(w, http.StatusOK, QueryResponse{Error: fmt.Sprintf("result exceeds %d rows; add a LIMIT or narrow the query", maxQueryRows)})
			return
		}
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
	// Confine the destination to the export base dir (refuses traversal, symlinked
	// parents, and overwrite). The daemon runs with elevated privileges, so an
	// unconfined path here is an arbitrary-file-write-as-bewitch primitive.
	cleanPath, err := confineOutputPath(s.exportBaseDir(), req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ExportResponse{Error: err.Error()})
		return
	}

	// Infer format from extension if not specified
	format := strings.ToLower(req.Format)
	if format == "" {
		switch strings.ToLower(filepath.Ext(cleanPath)) {
		case ".parquet":
			format = "parquet"
		case ".json":
			format = "json"
		default:
			format = "csv"
		}
	}
	// Validate format against an allowlist: it is interpolated into the COPY
	// options below, so an unchecked value would be an injection seam.
	switch format {
	case "csv", "parquet", "json":
	default:
		writeJSON(w, http.StatusBadRequest, ExportResponse{Error: "format must be one of: csv, parquet, json"})
		return
	}

	// Build COPY statement. req.SQL is validated read-only above; the destination
	// path is a single-quoted literal with embedded quotes doubled (DuckDB has no
	// parameter binding for COPY target paths, so we escape it ourselves).
	options := fmt.Sprintf("FORMAT %s", format)
	if format == "parquet" {
		options += ", COMPRESSION zstd"
	} else if format == "csv" {
		options += ", HEADER"
	}
	copySQL := fmt.Sprintf("COPY (%s) TO '%s' (%s)", req.SQL, strings.ReplaceAll(cleanPath, "'", "''"), options)

	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	queryStart := time.Now()
	result, err := s.dbFn().ExecContext(ctx, copySQL)
	if err != nil {
		writeJSON(w, http.StatusOK, ExportResponse{Error: queryErr(ctx, err)})
		return
	}

	rowCount, _ := result.RowsAffected()
	log.Debugf("export: %s rows=%d path=%s", time.Since(queryStart), rowCount, cleanPath)
	writeJSON(w, http.StatusOK, ExportResponse{RowCount: rowCount, Path: cleanPath})
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	var req SnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, SnapshotResponse{Error: "invalid request body"})
		return
	}
	// Confine the destination to the export base dir (refuses traversal, symlinked
	// parents, and overwrite) — same arbitrary-file-creation concern as export.
	destPath, err := confineOutputPath(s.exportBaseDir(), req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, SnapshotResponse{Error: err.Error()})
		return
	}
	if s.snapshotFn == nil {
		writeError(w, r, http.StatusServiceUnavailable, "snapshot not available")
		return
	}
	if err := s.snapshotFn(destPath, req.WithSystemTables); err != nil {
		writeJSON(w, http.StatusInternalServerError, SnapshotResponse{Error: err.Error()})
		return
	}
	var sizeBytes int64
	if info, err := os.Stat(destPath); err == nil {
		sizeBytes = info.Size()
	}
	writeJSON(w, http.StatusOK, SnapshotResponse{Path: destPath, SizeBytes: sizeBytes})
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
