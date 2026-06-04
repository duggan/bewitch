package api

import (
	"database/sql"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
)

// alertRulesDB creates an in-memory DuckDB with the alert rule schema needed by
// handleCreateAlertRule (mirrors migration 000001).
func alertRulesDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("opening in-memory DuckDB: %v", err)
	}
	stmts := []string{
		"CREATE SEQUENCE alert_rule_id_seq START 1",
		`CREATE TABLE alert_rules (
			id INTEGER DEFAULT nextval('alert_rule_id_seq'),
			name VARCHAR NOT NULL,
			type VARCHAR NOT NULL,
			severity VARCHAR NOT NULL,
			enabled BOOLEAN DEFAULT true,
			created_at TIMESTAMP DEFAULT current_timestamp
		)`,
		`CREATE TABLE alert_rule_threshold (
			rule_id INTEGER NOT NULL,
			metric VARCHAR NOT NULL,
			operator VARCHAR NOT NULL,
			value DOUBLE NOT NULL,
			duration VARCHAR NOT NULL,
			mount VARCHAR,
			interface_name VARCHAR,
			sensor VARCHAR
		)`,
		`CREATE TABLE alert_rule_predictive (
			rule_id INTEGER NOT NULL, metric VARCHAR NOT NULL, mount VARCHAR,
			predict_hours INTEGER NOT NULL, threshold_pct DOUBLE NOT NULL
		)`,
		`CREATE TABLE alert_rule_variance (
			rule_id INTEGER NOT NULL, metric VARCHAR NOT NULL,
			delta_threshold DOUBLE NOT NULL, min_count INTEGER NOT NULL, duration VARCHAR NOT NULL
		)`,
		`CREATE TABLE alert_rule_process_down (
			rule_id INTEGER NOT NULL, process_name VARCHAR NOT NULL, process_pattern VARCHAR,
			min_instances INTEGER NOT NULL, check_duration VARCHAR NOT NULL
		)`,
		`CREATE TABLE alert_rule_process_thrashing (
			rule_id INTEGER NOT NULL, process_name VARCHAR NOT NULL, process_pattern VARCHAR,
			restart_threshold INTEGER NOT NULL, restart_window VARCHAR NOT NULL
		)`,
		"CREATE SEQUENCE alert_id_seq START 1",
		`CREATE TABLE alerts (
			id INTEGER DEFAULT nextval('alert_id_seq'),
			ts TIMESTAMP NOT NULL,
			rule_name VARCHAR NOT NULL,
			severity VARCHAR NOT NULL,
			message VARCHAR NOT NULL,
			acknowledged BOOLEAN DEFAULT false
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("schema setup: %v", err)
		}
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestCreateThresholdRuleLinksConfig is a regression test for the DuckDB
// LastInsertId() bug: the driver returns 0 for LastInsertId(), so the type-specific
// config row was written with rule_id=0 and orphaned from the sequence-assigned base
// rule id, causing the engine's JOIN to silently skip the rule (it never fired).
func TestCreateThresholdRuleLinksConfig(t *testing.T) {
	db := alertRulesDB(t)
	s := &Server{dbFn: func() *sql.DB { return db }}

	body := `{"name":"disk_40","type":"threshold","severity":"warning",
		"metric":"disk.used_pct","operator":">","value":40,"duration":"1m","mount":"/"}`
	req := httptest.NewRequest("POST", "/api/alert-rules", strings.NewReader(body))
	rec := httptest.NewRecorder()

	s.handleCreateAlertRule(rec, req)

	if rec.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// The base rule and its threshold config must share the same (nonzero) id.
	var baseID int
	if err := db.QueryRow("SELECT id FROM alert_rules WHERE name = 'disk_40'").Scan(&baseID); err != nil {
		t.Fatalf("reading base rule: %v", err)
	}
	if baseID == 0 {
		t.Fatal("base rule id is 0")
	}

	var thresholdRuleID int
	if err := db.QueryRow("SELECT rule_id FROM alert_rule_threshold WHERE metric = 'disk.used_pct'").Scan(&thresholdRuleID); err != nil {
		t.Fatalf("reading threshold config: %v", err)
	}
	if thresholdRuleID != baseID {
		t.Errorf("threshold rule_id=%d does not match base rule id=%d (orphaned config)", thresholdRuleID, baseID)
	}

	// And the engine's JOIN must actually find the rule.
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM alert_rules r
		JOIN alert_rule_threshold t ON t.rule_id = r.id
		WHERE r.type = 'threshold'`).Scan(&n); err != nil {
		t.Fatalf("join query: %v", err)
	}
	if n != 1 {
		t.Errorf("engine JOIN matched %d rows, want 1", n)
	}
}

// createThresholdRule is a helper that creates a rule via the handler and returns its id.
func createThresholdRule(t *testing.T, s *Server, db *sql.DB, body string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleCreateAlertRule(rec, httptest.NewRequest("POST", "/api/alert-rules", strings.NewReader(body)))
	if rec.Code != 201 {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var id int
	if err := db.QueryRow("SELECT id FROM alert_rules ORDER BY id DESC LIMIT 1").Scan(&id); err != nil {
		t.Fatalf("reading created id: %v", err)
	}
	return id
}

// putRule invokes the update handler with {id} set on the request.
func putRule(s *Server, id int, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("PUT", "/api/alert-rules/x", strings.NewReader(body))
	req.SetPathValue("id", strconv.Itoa(id))
	rec := httptest.NewRecorder()
	s.handleUpdateAlertRule(rec, req)
	return rec
}

// TestUpdateAlertRuleInPlace verifies the update handler rewrites name/severity/config in
// place, preserving the rule id and created_at, and that the change is visible via JOIN.
func TestUpdateAlertRuleInPlace(t *testing.T) {
	db := alertRulesDB(t)
	s := &Server{dbFn: func() *sql.DB { return db }}

	id := createThresholdRule(t, s, db, `{"name":"disk_40","type":"threshold","severity":"warning",
		"metric":"disk.used_pct","operator":">","value":40,"duration":"1m","mount":"/"}`)

	var createdAt string
	db.QueryRow("SELECT created_at FROM alert_rules WHERE id = ?", id).Scan(&createdAt)

	rec := putRule(s, id, `{"name":"disk_90","type":"threshold","severity":"critical",
		"metric":"disk.used_pct","operator":">","value":90.5,"duration":"10m","mount":"/data"}`)
	if rec.Code != 200 {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var name, severity, gotCreated string
	db.QueryRow("SELECT name, severity, created_at FROM alert_rules WHERE id = ?", id).Scan(&name, &severity, &gotCreated)
	if name != "disk_90" || severity != "critical" {
		t.Errorf("base not updated: name=%q severity=%q", name, severity)
	}
	if gotCreated != createdAt {
		t.Errorf("created_at changed: %q -> %q", createdAt, gotCreated)
	}

	var value float64
	var duration, mount string
	db.QueryRow("SELECT value, duration, mount FROM alert_rule_threshold WHERE rule_id = ?", id).Scan(&value, &duration, &mount)
	if value != 90.5 || duration != "10m" || mount != "/data" {
		t.Errorf("config not updated: value=%v duration=%q mount=%q", value, duration, mount)
	}

	// Exactly one threshold config row should still exist for this rule (UPDATE, not INSERT).
	var cnt int
	db.QueryRow("SELECT count(*) FROM alert_rule_threshold WHERE rule_id = ?", id).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("expected 1 config row, got %d", cnt)
	}
}

func TestUpdateAlertRuleRejectsTypeChange(t *testing.T) {
	db := alertRulesDB(t)
	s := &Server{dbFn: func() *sql.DB { return db }}
	id := createThresholdRule(t, s, db, `{"name":"r","type":"threshold","severity":"warning",
		"metric":"disk.used_pct","operator":">","value":40,"duration":"1m","mount":"/"}`)

	rec := putRule(s, id, `{"name":"r","type":"variance","severity":"warning","metric":"memory.variance"}`)
	if rec.Code != 400 {
		t.Errorf("expected 400 on type change, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateAlertRuleNotFound(t *testing.T) {
	db := alertRulesDB(t)
	s := &Server{dbFn: func() *sql.DB { return db }}
	rec := putRule(s, 999, `{"name":"r","type":"threshold","severity":"warning",
		"metric":"disk.used_pct","operator":">","value":40,"duration":"1m","mount":"/"}`)
	if rec.Code != 404 {
		t.Errorf("expected 404 for missing rule, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateRejectsDuplicateName(t *testing.T) {
	db := alertRulesDB(t)
	s := &Server{dbFn: func() *sql.DB { return db }}
	body := `{"name":"dup","type":"threshold","severity":"warning",
		"metric":"disk.used_pct","operator":">","value":40,"duration":"1m","mount":"/"}`
	createThresholdRule(t, s, db, body)

	rec := httptest.NewRecorder()
	s.handleCreateAlertRule(rec, httptest.NewRequest("POST", "/api/alert-rules", strings.NewReader(body)))
	if rec.Code != 409 {
		t.Errorf("expected 409 on duplicate name, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateRejectsDuplicateName(t *testing.T) {
	db := alertRulesDB(t)
	s := &Server{dbFn: func() *sql.DB { return db }}
	createThresholdRule(t, s, db, `{"name":"a","type":"threshold","severity":"warning",
		"metric":"disk.used_pct","operator":">","value":40,"duration":"1m","mount":"/"}`)
	bID := createThresholdRule(t, s, db, `{"name":"b","type":"threshold","severity":"warning",
		"metric":"disk.used_pct","operator":">","value":40,"duration":"1m","mount":"/"}`)

	// Renaming b -> a collides.
	rec := putRule(s, bID, `{"name":"a","type":"threshold","severity":"warning",
		"metric":"disk.used_pct","operator":">","value":40,"duration":"1m","mount":"/"}`)
	if rec.Code != 409 {
		t.Errorf("expected 409 renaming onto existing name, got %d: %s", rec.Code, rec.Body.String())
	}

	// Renaming b -> b (no-op self) must be allowed.
	rec = putRule(s, bID, `{"name":"b","type":"threshold","severity":"critical",
		"metric":"disk.used_pct","operator":">","value":50,"duration":"1m","mount":"/"}`)
	if rec.Code != 200 {
		t.Errorf("expected 200 keeping own name, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestUpdateRenameRewritesFiredAlerts verifies a rename carries the rule's fired alerts
// to the new name, so they stay attributable and the delete-time cleanup can find them.
func TestUpdateRenameRewritesFiredAlerts(t *testing.T) {
	db := alertRulesDB(t)
	s := &Server{dbFn: func() *sql.DB { return db }}
	id := createThresholdRule(t, s, db, `{"name":"old","type":"threshold","severity":"warning",
		"metric":"disk.used_pct","operator":">","value":40,"duration":"1m","mount":"/"}`)
	db.Exec("INSERT INTO alerts (ts, rule_name, severity, message) VALUES (now(), 'old', 'warning', 'x')")

	rec := putRule(s, id, `{"name":"new","type":"threshold","severity":"warning",
		"metric":"disk.used_pct","operator":">","value":40,"duration":"1m","mount":"/"}`)
	if rec.Code != 200 {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var oldCount, newCount int
	db.QueryRow("SELECT count(*) FROM alerts WHERE rule_name = 'old'").Scan(&oldCount)
	db.QueryRow("SELECT count(*) FROM alerts WHERE rule_name = 'new'").Scan(&newCount)
	if oldCount != 0 || newCount != 1 {
		t.Errorf("expected fired alert carried old->new, got old=%d new=%d", oldCount, newCount)
	}
}

// TestDeleteAlertRuleClearsFiredAlerts verifies deleting a rule also removes its fired
// alerts (so they don't linger as orphaned, undismissable active alerts), while leaving
// other rules' alerts intact.
func TestDeleteAlertRuleClearsFiredAlerts(t *testing.T) {
	db := alertRulesDB(t)
	s := &Server{dbFn: func() *sql.DB { return db }}

	id := createThresholdRule(t, s, db, `{"name":"disk_40","type":"threshold","severity":"warning",
		"metric":"disk.used_pct","operator":">","value":40,"duration":"1m","mount":"/"}`)

	// Seed fired alerts: two for disk_40, one for an unrelated rule.
	db.Exec("INSERT INTO alerts (ts, rule_name, severity, message) VALUES (now(), 'disk_40', 'warning', 'a')")
	db.Exec("INSERT INTO alerts (ts, rule_name, severity, message) VALUES (now(), 'disk_40', 'warning', 'b')")
	db.Exec("INSERT INTO alerts (ts, rule_name, severity, message) VALUES (now(), 'other', 'warning', 'c')")

	req := httptest.NewRequest("DELETE", "/api/alert-rules/x", nil)
	req.SetPathValue("id", strconv.Itoa(id))
	rec := httptest.NewRecorder()
	s.handleDeleteAlertRule(rec, req)
	if rec.Code != 200 {
		t.Fatalf("delete: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var diskAlerts, otherAlerts, ruleRows, configRows int
	db.QueryRow("SELECT count(*) FROM alerts WHERE rule_name = 'disk_40'").Scan(&diskAlerts)
	db.QueryRow("SELECT count(*) FROM alerts WHERE rule_name = 'other'").Scan(&otherAlerts)
	db.QueryRow("SELECT count(*) FROM alert_rules WHERE id = ?", id).Scan(&ruleRows)
	db.QueryRow("SELECT count(*) FROM alert_rule_threshold WHERE rule_id = ?", id).Scan(&configRows)

	if diskAlerts != 0 {
		t.Errorf("expected disk_40 fired alerts cleared, got %d", diskAlerts)
	}
	if otherAlerts != 1 {
		t.Errorf("expected unrelated alert preserved, got %d", otherAlerts)
	}
	if ruleRows != 0 || configRows != 0 {
		t.Errorf("expected rule+config removed, got rule=%d config=%d", ruleRows, configRows)
	}
}
