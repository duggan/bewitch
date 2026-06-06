package alert

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/duggan/bewitch/internal/db"
)

func newRuleTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "rules.duckdb")
	database, err := db.Open(dbPath, "", "")
	if err != nil {
		t.Fatalf("opening migrated db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// seedProcess writes a process_info row plus one process_metrics row per snapshot
// timestamp it was present in.
func seedProcess(t *testing.T, database *sql.DB, pid int, startTime int64, name string, present []time.Time) {
	t.Helper()
	if _, err := database.Exec(
		`INSERT INTO process_info (pid, start_time, name, first_seen) VALUES (?, ?, ?, ?)`,
		pid, startTime, name, present[0]); err != nil {
		t.Fatalf("insert process_info: %v", err)
	}
	for _, ts := range present {
		if _, err := database.Exec(
			`INSERT INTO process_metrics (ts, pid, start_time, cpu_user_pct) VALUES (?, ?, ?, ?)`,
			ts, pid, startTime, 1.0); err != nil {
			t.Fatalf("insert process_metrics: %v", err)
		}
	}
}

// TestProcessDownWindowedAbsence verifies CheckDuration is now honoured: the rule
// fires only when the process is absent across the WHOLE window, and a single
// healthy snapshot (a brief blip / one missed tick) clears it — the behaviour the
// old single-snapshot check got wrong.
func TestProcessDownWindowedAbsence(t *testing.T) {
	now := time.Now()
	t0, t1, t2 := now.Add(-3*time.Second), now.Add(-2*time.Second), now.Add(-1*time.Second)
	snaps := []time.Time{t0, t1, t2}

	base := AlertRuleBase{Name: "nginx-down", Severity: "critical"}
	rule := NewProcessDownRule(base, ProcessDownConfig{ProcessName: "nginx", MinInstances: 1, CheckDuration: "5m"})

	t.Run("present in every snapshot → no alert", func(t *testing.T) {
		database := newRuleTestDB(t)
		seedProcess(t, database, 1, 100, "systemd", snaps) // sentinel defines the window
		seedProcess(t, database, 10, 1000, "nginx", snaps)
		alert, err := rule.Evaluate(database)
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if alert != nil {
			t.Errorf("expected no alert when present in every snapshot, got %+v", alert)
		}
	})

	t.Run("present in just one snapshot → no alert (rides out a blip)", func(t *testing.T) {
		database := newRuleTestDB(t)
		seedProcess(t, database, 1, 100, "systemd", snaps)
		seedProcess(t, database, 10, 1000, "nginx", []time.Time{t1}) // only the middle snapshot
		alert, err := rule.Evaluate(database)
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if alert != nil {
			t.Errorf("expected no alert: a single healthy snapshot must clear it, got %+v", alert)
		}
	})

	t.Run("absent across the whole window → alert", func(t *testing.T) {
		database := newRuleTestDB(t)
		seedProcess(t, database, 1, 100, "systemd", snaps) // window has snapshots, but no nginx
		alert, err := rule.Evaluate(database)
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if alert == nil {
			t.Fatal("expected a down alert when absent across the whole window")
		}
		if alert.RuleName != "nginx-down" {
			t.Errorf("alert.RuleName = %q, want nginx-down", alert.RuleName)
		}
	})

	t.Run("empty window (no process data yet) → no false fire at startup", func(t *testing.T) {
		database := newRuleTestDB(t)
		alert, err := rule.Evaluate(database)
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if alert != nil {
			t.Errorf("expected no alert when there is no process data at all, got %+v", alert)
		}
	})
}

// TestPredictiveAlreadyBreached verifies the rule no longer goes silent when the
// disk is already at/over the target (the old hoursUntil>0 / slope<=0 guards both
// returned nil exactly when the disk was most at risk).
func TestPredictiveAlreadyBreached(t *testing.T) {
	seedDisk := func(t *testing.T, database *sql.DB, pct float64, n int) {
		t.Helper()
		if _, err := database.Exec(
			"INSERT INTO dimension_values (id, category, value) VALUES (1, 'mount', '/')"); err != nil {
			t.Fatalf("seed dimension: %v", err)
		}
		now := time.Now()
		used := int64(pct)
		for i := 0; i < n; i++ {
			if _, err := database.Exec(
				`INSERT INTO disk_metrics (ts, mount_id, total_bytes, used_bytes, free_bytes)
				 VALUES (?, 1, 100, ?, ?)`,
				now.Add(-time.Duration(n-i)*time.Minute), used, 100-used); err != nil {
				t.Fatalf("seed disk_metrics: %v", err)
			}
		}
	}

	base := AlertRuleBase{Name: "disk-fills", Severity: "warning"}
	rule := NewPredictiveRule(base, PredictiveConfig{Metric: "disk.used_pct", Mount: "/", ThresholdPct: 90, PredictHours: 24})

	t.Run("flat at 99 pct over target → alert", func(t *testing.T) {
		database := newRuleTestDB(t)
		seedDisk(t, database, 99, 5)
		alert, err := rule.Evaluate(database)
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if alert == nil {
			t.Fatal("expected an alert for a disk flat-at-99% over the target")
		}
		if !strings.Contains(alert.Message, "already at") {
			t.Errorf("message = %q, want it to say 'already at'", alert.Message)
		}
	})

	t.Run("flat well below target → no alert", func(t *testing.T) {
		database := newRuleTestDB(t)
		seedDisk(t, database, 20, 5)
		alert, err := rule.Evaluate(database)
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if alert != nil {
			t.Errorf("expected no alert for a flat disk well under target, got %+v", alert)
		}
	})
}

// TestEngineProcessPins verifies the engine surfaces process_down / process_thrashing
// rule targets so the collector can force-enrich them.
func TestEngineProcessPins(t *testing.T) {
	e := &Engine{rules: []Rule{
		NewProcessDownRule(AlertRuleBase{Name: "a"}, ProcessDownConfig{ProcessName: "nginx"}),
		NewProcessThrashingRule(AlertRuleBase{Name: "b"}, ProcessThrashingConfig{ProcessPattern: "*/myserver"}),
		NewThresholdRule(AlertRuleBase{Name: "c"}, ThresholdConfig{Metric: "cpu.aggregate"}), // ignored
	}}
	pins := e.ProcessPins()
	want := map[string]bool{"nginx": true, "*/myserver": true}
	if len(pins) != len(want) {
		t.Fatalf("pins = %v, want keys %v", pins, want)
	}
	for _, p := range pins {
		if !want[p] {
			t.Errorf("unexpected pin %q", p)
		}
	}
}
