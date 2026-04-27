package store

import (
	"path/filepath"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/duggan/bewitch/internal/db"
)

func newStatsTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "stats.duckdb")
	database, err := db.Open(dbPath, "")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return New(database)
}

func TestStatsEmpty(t *testing.T) {
	s := newStatsTestStore(t)

	stats, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if len(stats.Tables) == 0 {
		t.Fatal("expected per-table stats, got none")
	}

	expectedTables := map[string]bool{
		"cpu_metrics":         true,
		"memory_metrics":      true,
		"disk_metrics":        true,
		"network_metrics":     true,
		"ecc_metrics":         true,
		"temperature_metrics": true,
		"power_metrics":       true,
		"process_metrics":     true,
		"gpu_metrics":         true,
	}
	for _, ts := range stats.Tables {
		if !expectedTables[ts.Name] {
			t.Errorf("unexpected table %q", ts.Name)
		}
		if ts.Rows != 0 {
			t.Errorf("%s: rows = %d, want 0", ts.Name, ts.Rows)
		}
		if ts.OldestTs != 0 || ts.NewestTs != 0 {
			t.Errorf("%s: empty table reported non-zero ts: oldest=%d newest=%d", ts.Name, ts.OldestTs, ts.NewestTs)
		}
		delete(expectedTables, ts.Name)
	}
	if len(expectedTables) != 0 {
		t.Errorf("missing tables in stats: %v", expectedTables)
	}

	if stats.Processes != 0 {
		t.Errorf("Processes = %d, want 0", stats.Processes)
	}
	if stats.AlertRulesEnabled != 0 || stats.AlertRulesDisabled != 0 {
		t.Errorf("alert rule counts non-zero on empty DB")
	}
	if stats.AlertsFiredTotal != 0 || stats.AlertsFiredUnacked != 0 {
		t.Errorf("alert counts non-zero on empty DB")
	}
}

func TestStatsPopulated(t *testing.T) {
	s := newStatsTestStore(t)
	database := s.db

	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(30 * time.Minute)
	t2 := t0.Add(60 * time.Minute)

	// Insert into cpu_metrics across two timestamps
	for _, ts := range []time.Time{t0, t1, t2} {
		if _, err := database.Exec(
			`INSERT INTO cpu_metrics (ts, core, user_pct, system_pct) VALUES (?, ?, ?, ?)`,
			ts, 0, 10.0, 5.0); err != nil {
			t.Fatalf("insert cpu_metrics: %v", err)
		}
	}

	// Insert dimension values across categories
	for i, dim := range []struct{ cat, val string }{
		{"mount", "/"}, {"mount", "/home"},
		{"interface", "eth0"},
		{"sensor", "cpu0"}, {"sensor", "cpu1"}, {"sensor", "cpu2"},
	} {
		if _, err := database.Exec(
			`INSERT INTO dimension_values (id, category, value) VALUES (?, ?, ?)`,
			i+1, dim.cat, dim.val); err != nil {
			t.Fatalf("insert dimension: %v", err)
		}
	}

	// Insert process_info rows
	for i := 1; i <= 4; i++ {
		if _, err := database.Exec(
			`INSERT INTO process_info (pid, start_time, name, first_seen) VALUES (?, ?, ?, ?)`,
			i, int64(i*1000), "proc", t0); err != nil {
			t.Fatalf("insert process_info: %v", err)
		}
	}

	// Insert alert_rules: 2 enabled, 1 disabled
	for i, enabled := range []bool{true, true, false} {
		if _, err := database.Exec(
			`INSERT INTO alert_rules (id, name, type, severity, enabled) VALUES (?, ?, ?, ?, ?)`,
			i+1, "rule", "threshold", "warning", enabled); err != nil {
			t.Fatalf("insert alert_rules: %v", err)
		}
	}

	// Insert alerts: 3 total, 2 unacked
	for i, ack := range []bool{false, false, true} {
		if _, err := database.Exec(
			`INSERT INTO alerts (id, ts, rule_name, severity, message, acknowledged) VALUES (?, ?, ?, ?, ?, ?)`,
			i+1, t0, "rule", "warning", "msg", ack); err != nil {
			t.Fatalf("insert alerts: %v", err)
		}
	}

	stats, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	var cpu TableMetricStats
	for _, ts := range stats.Tables {
		if ts.Name == "cpu_metrics" {
			cpu = ts
		}
	}
	if cpu.Rows != 3 {
		t.Errorf("cpu_metrics rows = %d, want 3", cpu.Rows)
	}
	if cpu.OldestTs != t0.UnixNano() {
		t.Errorf("cpu_metrics OldestTs = %d, want %d", cpu.OldestTs, t0.UnixNano())
	}
	if cpu.NewestTs != t2.UnixNano() {
		t.Errorf("cpu_metrics NewestTs = %d, want %d", cpu.NewestTs, t2.UnixNano())
	}

	if got := stats.Dimensions["mount"]; got != 2 {
		t.Errorf("Dimensions[mount] = %d, want 2", got)
	}
	if got := stats.Dimensions["sensor"]; got != 3 {
		t.Errorf("Dimensions[sensor] = %d, want 3", got)
	}
	if got := stats.Dimensions["interface"]; got != 1 {
		t.Errorf("Dimensions[interface] = %d, want 1", got)
	}

	if stats.Processes != 4 {
		t.Errorf("Processes = %d, want 4", stats.Processes)
	}
	if stats.AlertRulesEnabled != 2 {
		t.Errorf("AlertRulesEnabled = %d, want 2", stats.AlertRulesEnabled)
	}
	if stats.AlertRulesDisabled != 1 {
		t.Errorf("AlertRulesDisabled = %d, want 1", stats.AlertRulesDisabled)
	}
	if stats.AlertsFiredTotal != 3 {
		t.Errorf("AlertsFiredTotal = %d, want 3", stats.AlertsFiredTotal)
	}
	if stats.AlertsFiredUnacked != 2 {
		t.Errorf("AlertsFiredUnacked = %d, want 2", stats.AlertsFiredUnacked)
	}
}

