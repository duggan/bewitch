package api

import (
	"database/sql"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/duggan/bewitch/internal/alert"
	"github.com/duggan/bewitch/internal/config"
	"github.com/duggan/bewitch/internal/db"
)

// TestAlertPipelineEndToEnd exercises the full lifecycle on a real migrated database:
// create a rule through the HTTP handler, feed it breaching metrics, run one engine
// evaluation, and assert an alert fires and is listed — then delete the rule and confirm
// its fired alerts are cleared.
//
// This is the test that would have caught the rule_id=0 bug: with LastInsertId() the
// rule's config row was orphaned, the engine's JOIN skipped it, and no alert ever fired —
// which this test asserts against directly, rather than only checking the handler returns
// 201.
func TestAlertPipelineEndToEnd(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pipeline.duckdb")
	database, err := db.Open(dbPath, "", "")
	if err != nil {
		t.Fatalf("opening migrated db: %v", err)
	}
	defer database.Close()
	dbFn := func() *sql.DB { return database }

	s := &Server{dbFn: dbFn}

	// 1. Create a threshold rule via the real handler: disk / usage > 40% for 1m.
	rec := httptest.NewRecorder()
	s.handleCreateAlertRule(rec, httptest.NewRequest("POST", "/api/alert-rules",
		strings.NewReader(`{"name":"disk_40","type":"threshold","severity":"warning",
			"metric":"disk.used_pct","operator":">","value":40,"duration":"1m","mount":"/"}`)))
	if rec.Code != 201 {
		t.Fatalf("create rule: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Seed breaching disk metrics for mount "/": 50% used, within the 1m window.
	if _, err := database.Exec(
		"INSERT INTO dimension_values (id, category, value) VALUES (1, 'mount', '/')"); err != nil {
		t.Fatalf("seed dimension: %v", err)
	}
	now := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := database.Exec(
			`INSERT INTO disk_metrics (ts, mount_id, total_bytes, used_bytes, free_bytes)
			 VALUES (?, 1, 100, 50, 50)`, now.Add(-time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("seed disk_metrics: %v", err)
		}
	}

	// 3. Run one engine cycle (reload rules from DB + evaluate).
	engine := alert.NewEngine(dbFn, &config.AlertsConfig{EvaluationInterval: "1s"})
	engine.EvaluateOnce()

	// 4. The alert must have fired and be visible through the list handler.
	listRec := httptest.NewRecorder()
	s.handleListAlerts(listRec, httptest.NewRequest("GET", "/api/alerts", nil))
	if listRec.Code != 200 {
		t.Fatalf("list alerts: expected 200, got %d", listRec.Code)
	}
	if !strings.Contains(listRec.Body.String(), `"rule_name":"disk_40"`) {
		t.Fatalf("expected a fired alert for disk_40, got: %s", listRec.Body.String())
	}

	var fired int
	database.QueryRow("SELECT count(*) FROM alerts WHERE rule_name = 'disk_40'").Scan(&fired)
	if fired == 0 {
		t.Fatal("no alert row written — rule did not fire")
	}

	// 5. Deleting the rule clears its fired alerts (concern B).
	var id int
	database.QueryRow("SELECT id FROM alert_rules WHERE name = 'disk_40'").Scan(&id)
	delReq := httptest.NewRequest("DELETE", "/api/alert-rules/x", nil)
	delReq.SetPathValue("id", strconv.Itoa(id))
	delRec := httptest.NewRecorder()
	s.handleDeleteAlertRule(delRec, delReq)
	if delRec.Code != 200 {
		t.Fatalf("delete rule: expected 200, got %d: %s", delRec.Code, delRec.Body.String())
	}

	var remaining int
	database.QueryRow("SELECT count(*) FROM alerts WHERE rule_name = 'disk_40'").Scan(&remaining)
	if remaining != 0 {
		t.Errorf("expected fired alerts cleared after rule delete, got %d", remaining)
	}
}

// TestUpdatedRuleReloadsAndFires verifies an edit is picked up by the engine: a rule that
// does not breach is updated to a threshold that does, and the next evaluation fires.
func TestUpdatedRuleReloadsAndFires(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "update_pipeline.duckdb")
	database, err := db.Open(dbPath, "", "")
	if err != nil {
		t.Fatalf("opening migrated db: %v", err)
	}
	defer database.Close()
	dbFn := func() *sql.DB { return database }
	s := &Server{dbFn: dbFn}

	// Rule starts at > 90% (won't fire at 50% usage).
	rec := httptest.NewRecorder()
	s.handleCreateAlertRule(rec, httptest.NewRequest("POST", "/api/alert-rules",
		strings.NewReader(`{"name":"disk_x","type":"threshold","severity":"warning",
			"metric":"disk.used_pct","operator":">","value":90,"duration":"1m","mount":"/"}`)))
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	database.Exec("INSERT INTO dimension_values (id, category, value) VALUES (1, 'mount', '/')")
	database.Exec(`INSERT INTO disk_metrics (ts, mount_id, total_bytes, used_bytes, free_bytes)
		VALUES (?, 1, 100, 50, 50)`, time.Now())

	engine := alert.NewEngine(dbFn, &config.AlertsConfig{EvaluationInterval: "1s"})
	engine.EvaluateOnce()
	var fired int
	database.QueryRow("SELECT count(*) FROM alerts WHERE rule_name = 'disk_x'").Scan(&fired)
	if fired != 0 {
		t.Fatalf("rule should not fire at 90%% threshold, got %d alerts", fired)
	}

	// Update the threshold down to > 40%, which 50% usage breaches.
	var id int
	database.QueryRow("SELECT id FROM alert_rules WHERE name = 'disk_x'").Scan(&id)
	putRule(s, id, `{"name":"disk_x","type":"threshold","severity":"warning",
		"metric":"disk.used_pct","operator":">","value":40,"duration":"1m","mount":"/"}`)

	engine.EvaluateOnce()
	database.QueryRow("SELECT count(*) FROM alerts WHERE rule_name = 'disk_x'").Scan(&fired)
	if fired == 0 {
		t.Error("expected the updated rule to fire after lowering the threshold")
	}
}
