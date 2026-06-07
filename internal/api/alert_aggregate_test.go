package api

import (
	"database/sql"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/duggan/bewitch/internal/alert"
	"github.com/duggan/bewitch/internal/config"
	"github.com/duggan/bewitch/internal/db"
)

// TestThresholdAggregatePersistence verifies the aggregate column is written on
// create (explicit and defaulted), updated in place, and returned by the list
// handler.
func TestThresholdAggregatePersistence(t *testing.T) {
	database := alertRulesDB(t)
	s := &Server{dbFn: func() *sql.DB { return database }}

	readAgg := func(ruleID int) string {
		t.Helper()
		var agg string
		if err := database.QueryRow("SELECT aggregate FROM alert_rule_threshold WHERE rule_id = ?", ruleID).Scan(&agg); err != nil {
			t.Fatalf("read aggregate: %v", err)
		}
		return agg
	}

	// Explicit aggregate persists.
	id := createThresholdRule(t, s, database, `{"name":"r1","type":"threshold","severity":"warning",
		"metric":"disk.used_pct","operator":">","value":40,"duration":"1m","mount":"/","aggregate":"max"}`)
	if got := readAgg(id); got != "max" {
		t.Errorf("created aggregate = %q, want max", got)
	}

	// Omitted aggregate defaults to avg.
	id2 := createThresholdRule(t, s, database, `{"name":"r2","type":"threshold","severity":"warning",
		"metric":"cpu.aggregate","operator":">","value":90,"duration":"5m"}`)
	if got := readAgg(id2); got != "avg" {
		t.Errorf("defaulted aggregate = %q, want avg", got)
	}

	// Update changes it in place.
	rec := putRule(s, id, `{"name":"r1","type":"threshold","severity":"warning",
		"metric":"disk.used_pct","operator":">","value":40,"duration":"1m","mount":"/","aggregate":"min"}`)
	if rec.Code != 200 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if got := readAgg(id); got != "min" {
		t.Errorf("updated aggregate = %q, want min", got)
	}

	// The list handler round-trips it back to clients.
	listRec := httptest.NewRecorder()
	s.handleListAlertRules(listRec, httptest.NewRequest("GET", "/api/alert-rules", nil))
	if !strings.Contains(listRec.Body.String(), `"aggregate":"min"`) {
		t.Errorf("list response missing aggregate:\n%s", listRec.Body.String())
	}
}

// TestThresholdAggregateColumnDefault proves the column DEFAULT 'avg' applies to
// a row inserted without an aggregate — exactly what existing threshold rows get
// when migration 000011 runs ADD COLUMN on an upgraded database.
func TestThresholdAggregateColumnDefault(t *testing.T) {
	database := alertRulesDB(t)
	if _, err := database.Exec(`INSERT INTO alert_rule_threshold (rule_id, metric, operator, value, duration)
		VALUES (999, 'cpu.aggregate', '>', 90, '5m')`); err != nil {
		t.Fatalf("insert threshold without aggregate: %v", err)
	}
	var agg string
	if err := database.QueryRow("SELECT aggregate FROM alert_rule_threshold WHERE rule_id = 999").Scan(&agg); err != nil {
		t.Fatalf("read aggregate: %v", err)
	}
	if agg != "avg" {
		t.Errorf("column DEFAULT not applied (upgrade back-compat): aggregate = %q, want avg", agg)
	}
}

// TestThresholdAggregateMaxVsAvg proves the per-rule aggregate is honoured end to
// end: a windowed MAX rule fires on a brief spike that a windowed AVG rule — same
// threshold, same data — correctly ignores. This is the whole point of the feature.
func TestThresholdAggregateMaxVsAvg(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agg.duckdb")
	database, err := db.Open(dbPath, "", "")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	dbFn := func() *sql.DB { return database }
	s := &Server{dbFn: dbFn}

	create := func(name, agg string) {
		t.Helper()
		body := `{"name":"` + name + `","type":"threshold","severity":"warning",
			"metric":"disk.used_pct","operator":">","value":60,"duration":"1m","mount":"/","aggregate":"` + agg + `"}`
		rec := httptest.NewRecorder()
		s.handleCreateAlertRule(rec, httptest.NewRequest("POST", "/api/alert-rules", strings.NewReader(body)))
		if rec.Code != 201 {
			t.Fatalf("create %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	create("disk_max", "max")
	create("disk_avg", "avg")

	// Five low samples (30%) plus one spike (90%) within the 1m window:
	// AVG = (30*5 + 90)/6 = 40% (below 60 → avg rule must NOT fire),
	// MAX = 90% (above 60 → max rule fires).
	if _, err := database.Exec("INSERT INTO dimension_values (id, category, value) VALUES (1, 'mount', '/')"); err != nil {
		t.Fatalf("seed dim: %v", err)
	}
	now := time.Now()
	seed := func(usedPct int64, secAgo int) {
		if _, err := database.Exec(`INSERT INTO disk_metrics (ts, mount_id, total_bytes, used_bytes, free_bytes)
			VALUES (?, 1, 100, ?, ?)`, now.Add(-time.Duration(secAgo)*time.Second), usedPct, 100-usedPct); err != nil {
			t.Fatalf("seed disk: %v", err)
		}
	}
	for i := 0; i < 5; i++ {
		seed(30, i+1)
	}
	seed(90, 0) // the spike (newest sample)

	engine := alert.NewEngine(dbFn, &config.AlertsConfig{EvaluationInterval: "1s"})
	engine.EvaluateOnce()

	fired := func(name string) int {
		var n int
		database.QueryRow("SELECT count(*) FROM alerts WHERE rule_name = ?", name).Scan(&n)
		return n
	}
	if fired("disk_max") == 0 {
		t.Error("max-aggregate rule did NOT fire on the 90% spike (windowed MAX should breach 60)")
	}
	if fired("disk_avg") != 0 {
		t.Error("avg-aggregate rule fired, but the 40% window average is below 60 — it must not")
	}
}
