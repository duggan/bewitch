package api

import (
	"database/sql"
	"net/http/httptest"
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
