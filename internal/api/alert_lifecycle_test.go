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

// lifecycleSetup opens a migrated DB, creates a disk.used_pct > 40% rule via the
// real handler, and seeds the mount dimension. Returns the db, engine, and a
// helper to set the current disk usage percent.
func lifecycleSetup(t *testing.T, ruleName string) (*sql.DB, *alert.Engine, func(pct int)) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "lifecycle.duckdb"), "", "")
	if err != nil {
		t.Fatalf("opening migrated db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	dbFn := func() *sql.DB { return database }
	s := &Server{dbFn: dbFn}

	rec := httptest.NewRecorder()
	s.handleCreateAlertRule(rec, httptest.NewRequest("POST", "/api/alert-rules",
		strings.NewReader(`{"name":"`+ruleName+`","type":"threshold","severity":"warning",
			"metric":"disk.used_pct","operator":">","value":40,"duration":"1m","mount":"/"}`)))
	if rec.Code != 201 {
		t.Fatalf("create rule: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := database.Exec("INSERT INTO dimension_values (id, category, value) VALUES (1, 'mount', '/')"); err != nil {
		t.Fatalf("seed dimension: %v", err)
	}

	setUsage := func(pct int) {
		if _, err := database.Exec("DELETE FROM disk_metrics"); err != nil {
			t.Fatalf("clear disk_metrics: %v", err)
		}
		now := time.Now()
		for i := 0; i < 3; i++ {
			if _, err := database.Exec(
				`INSERT INTO disk_metrics (ts, mount_id, total_bytes, used_bytes, free_bytes)
				 VALUES (?, 1, 100, ?, ?)`, now.Add(-time.Duration(i)*time.Second), pct, 100-pct); err != nil {
				t.Fatalf("seed disk_metrics: %v", err)
			}
		}
	}
	engine := alert.NewEngine(dbFn, &config.AlertsConfig{EvaluationInterval: "1s"})
	return database, engine, setUsage
}

func countAlerts(t *testing.T, database *sql.DB, where string) int {
	t.Helper()
	var n int
	if err := database.QueryRow("SELECT count(*) FROM alerts WHERE " + where).Scan(&n); err != nil {
		t.Fatalf("count(%s): %v", where, err)
	}
	return n
}

// TestAlertResolves fires an alert, clears the condition, and asserts the same
// alert row is marked resolved (not a second fired alert).
func TestAlertResolves(t *testing.T) {
	database, engine, setUsage := lifecycleSetup(t, "disk_res")

	setUsage(50) // breaching
	engine.EvaluateOnce()
	if got := countAlerts(t, database, "rule_name='disk_res' AND resolved_at IS NULL"); got != 1 {
		t.Fatalf("expected 1 active alert after breach, got %d", got)
	}

	setUsage(30) // condition clears
	engine.EvaluateOnce()
	if got := countAlerts(t, database, "rule_name='disk_res' AND resolved_at IS NOT NULL"); got != 1 {
		t.Errorf("expected the alert to be resolved, got %d resolved", got)
	}
	if got := countAlerts(t, database, "rule_name='disk_res'"); got != 1 {
		t.Errorf("expected exactly 1 alert total (fired then resolved in place), got %d", got)
	}
}

// TestAckDoesNotRefire is the regression for the old bug: acking a still-breaching
// alert used to spawn a duplicate next cycle (debounce keyed on acknowledged=false).
func TestAckDoesNotRefire(t *testing.T) {
	database, engine, setUsage := lifecycleSetup(t, "disk_ack")

	setUsage(50)
	engine.EvaluateOnce()
	if got := countAlerts(t, database, "rule_name='disk_ack'"); got != 1 {
		t.Fatalf("expected 1 alert after breach, got %d", got)
	}

	// Acknowledge it while the condition is still breaching.
	if _, err := database.Exec("UPDATE alerts SET acknowledged = true WHERE rule_name = 'disk_ack'"); err != nil {
		t.Fatalf("ack: %v", err)
	}
	engine.EvaluateOnce() // still breaching

	if got := countAlerts(t, database, "rule_name='disk_ack'"); got != 1 {
		t.Errorf("acking a still-breaching alert spawned a duplicate: %d alerts (want 1)", got)
	}
}

// TestDeadMansSwitch fires when cpu collection has stalled and resolves when it resumes.
func TestDeadMansSwitch(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "deadman.duckdb"), "", "")
	if err != nil {
		t.Fatalf("opening migrated db: %v", err)
	}
	defer database.Close()
	dbFn := func() *sql.DB { return database }
	engine := alert.NewEngine(dbFn, &config.AlertsConfig{EvaluationInterval: "1s"})

	insertCPU := func(ts time.Time) {
		if _, err := database.Exec(
			"INSERT INTO cpu_metrics (ts, core, user_pct, system_pct, idle_pct, iowait_pct) VALUES (?, 0, 1, 1, 98, 0)", ts); err != nil {
			t.Fatalf("insert cpu_metrics: %v", err)
		}
	}

	// Newest cpu sample is 10 minutes old → collection stalled (threshold is 2m).
	insertCPU(time.Now().Add(-10 * time.Minute))
	engine.EvaluateOnce()
	if got := countAlerts(t, database, "rule_name='collection-stalled' AND resolved_at IS NULL"); got != 1 {
		t.Fatalf("dead-man's-switch should fire on stale collection, got %d active", got)
	}

	// Collection resumes (fresh sample) → resolve.
	insertCPU(time.Now())
	engine.EvaluateOnce()
	if got := countAlerts(t, database, "rule_name='collection-stalled' AND resolved_at IS NULL"); got != 0 {
		t.Errorf("dead-man's-switch should resolve when collection resumes, got %d active", got)
	}
	if got := countAlerts(t, database, "rule_name='collection-stalled' AND resolved_at IS NOT NULL"); got != 1 {
		t.Errorf("expected 1 resolved collection-stalled alert, got %d", got)
	}
}
