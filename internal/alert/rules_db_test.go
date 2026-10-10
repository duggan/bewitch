package alert

import (
	"database/sql"
	"errors"
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

	t.Run("empty window (no process data yet) → no data, no false fire at startup", func(t *testing.T) {
		database := newRuleTestDB(t)
		alert, err := rule.Evaluate(database)
		if !errors.Is(err, ErrNoData) {
			t.Fatalf("evaluate: err = %v, want ErrNoData", err)
		}
		if alert != nil {
			t.Errorf("expected no alert when there is no process data at all, got %+v", alert)
		}
	})
}

// TestProcessDownSingleSnapshotNoData: the CheckDuration-less path used to count 0
// instances against a DB with no process snapshots and fire "down" at startup.
func TestProcessDownSingleSnapshotNoData(t *testing.T) {
	rule := NewProcessDownRule(AlertRuleBase{Name: "nginx-down"}, ProcessDownConfig{ProcessName: "nginx", MinInstances: 1})
	database := newRuleTestDB(t)
	if alert, err := rule.Evaluate(database); !errors.Is(err, ErrNoData) || alert != nil {
		t.Fatalf("Evaluate on empty DB = (%+v, %v), want (nil, ErrNoData)", alert, err)
	}
	seedProcess(t, database, 1, 100, "systemd", []time.Time{time.Now()})
	alert, err := rule.Evaluate(database)
	if err != nil || alert == nil {
		t.Fatalf("Evaluate with a snapshot lacking nginx = (%+v, %v), want a down alert", alert, err)
	}
}

// TestThresholdNoData: an empty window is ErrNoData for every threshold metric,
// including smart.unhealthy, whose bare COUNT used to read 0 ("healthy") and
// resolve an active alert between SMART refreshes.
func TestThresholdNoData(t *testing.T) {
	database := newRuleTestDB(t)
	for _, metric := range []string{"cpu.aggregate", "memory.used_pct", "disk.used_pct", "smart.reallocated", "smart.unhealthy", "ecc.uncorrectable"} {
		rule := NewThresholdRule(AlertRuleBase{Name: metric}, ThresholdConfig{Metric: metric, Operator: ">", Value: 0, Duration: "5m", Mount: "/"})
		if alert, err := rule.Evaluate(database); !errors.Is(err, ErrNoData) || alert != nil {
			t.Errorf("%s: Evaluate on empty window = (%+v, %v), want (nil, ErrNoData)", metric, alert, err)
		}
	}

	// A healthy SMART row makes smart.unhealthy evaluate (count 0, not breaching).
	if _, err := database.Exec(`INSERT INTO smart_metrics (ts, device, healthy) VALUES (?, 'nvme0n1', true)`, time.Now()); err != nil {
		t.Fatalf("seed smart_metrics: %v", err)
	}
	rule := NewThresholdRule(AlertRuleBase{Name: "u"}, ThresholdConfig{Metric: "smart.unhealthy", Operator: ">", Value: 0, Duration: "5m"})
	if alert, err := rule.Evaluate(database); err != nil || alert != nil {
		t.Errorf("smart.unhealthy with a healthy row = (%+v, %v), want (nil, nil)", alert, err)
	}
}

// TestNoDataWarnsOnce: the engine warns once a rule has gone noDataWarnAfter
// without data, and forgets the rule as soon as it has data again.
func TestNoDataWarnsOnce(t *testing.T) {
	e := &Engine{}
	t0 := time.Now()
	e.noteNoData("r", t0)
	if e.noData["r"].warned {
		t.Fatal("warned immediately; want a grace period")
	}
	e.noteNoData("r", t0.Add(noDataWarnAfter))
	if !e.noData["r"].warned || !e.noData["r"].since.Equal(t0) {
		t.Fatalf("state after %s = %+v, want warned with the original since", noDataWarnAfter, *e.noData["r"])
	}
	e.noteData("r")
	if _, ok := e.noData["r"]; ok {
		t.Error("noteData did not clear the rule")
	}
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

func seedSMART(t *testing.T, database *sql.DB, device string, ts time.Time, reallocated int64, healthy bool) {
	t.Helper()
	if _, err := database.Exec(
		`INSERT INTO smart_metrics (ts, device, healthy, reallocated_sectors) VALUES (?, ?, ?, ?)`,
		ts, device, healthy, reallocated); err != nil {
		t.Fatalf("seed smart_metrics: %v", err)
	}
}

// TestCounterIncrease: "increase" fires on growth since the last reading before
// the window — even when the window is shorter than the SMART cadence — names the
// device, and ignores a drive whose count is high but stable.
func TestCounterIncrease(t *testing.T) {
	now := time.Now()
	rule := func(agg, dur string) *ThresholdRule {
		return NewThresholdRule(AlertRuleBase{Name: "realloc"},
			ThresholdConfig{Metric: "smart.reallocated", Operator: ">", Value: 0, Duration: dur, Aggregate: agg})
	}

	database := newRuleTestDB(t)
	// sda: 8 reallocated sectors for years, stable. sdb: 0 → 3 in the latest reading.
	for _, ago := range []time.Duration{15 * time.Minute, 10 * time.Minute, 5 * time.Minute, 30 * time.Second} {
		seedSMART(t, database, "sda", now.Add(-ago), 8, true)
	}
	seedSMART(t, database, "sdb", now.Add(-10*time.Minute), 0, true)
	seedSMART(t, database, "sdb", now.Add(-5*time.Minute), 0, true)
	seedSMART(t, database, "sdb", now.Add(-30*time.Second), 3, true)

	// Level: fires on the stable drive (the old, permanently-firing behaviour).
	alert, err := rule("max", "1m").Evaluate(database)
	if err != nil || alert == nil || !strings.Contains(alert.Message, "on sda") {
		t.Fatalf("level = (%+v, %v), want an alert naming sda", alert, err)
	}

	// Increase over a 1m window (one SMART row in it): sdb's step from its
	// pre-window reading is seen; sda's stable 8 is not.
	alert, err = rule("increase", "1m").Evaluate(database)
	if err != nil || alert == nil {
		t.Fatalf("increase = (%+v, %v), want an alert", alert, err)
	}
	if want := "smart.reallocated on sdb increase 3.0 > 0.0 over 1m"; alert.Message != want {
		t.Errorf("message = %q, want %q", alert.Message, want)
	}

	// Without sdb's step, increase is healthy even though sda sits at 8.
	if _, err := database.Exec("DELETE FROM smart_metrics WHERE device = 'sdb'"); err != nil {
		t.Fatal(err)
	}
	if alert, err := rule("increase", "1m").Evaluate(database); err != nil || alert != nil {
		t.Errorf("stable count under increase = (%+v, %v), want (nil, nil)", alert, err)
	}
}

// TestECCIncreaseAcrossReboot: ECC counts reset at boot. A reset inside the window
// counts the post-reset reading as new errors rather than a negative increase.
func TestECCIncreaseAcrossReboot(t *testing.T) {
	now := time.Now()
	database := newRuleTestDB(t)
	seed := func(ago time.Duration, n int64) {
		if _, err := database.Exec(`INSERT INTO ecc_metrics (ts, corrected, uncorrected) VALUES (?, ?, 0)`, now.Add(-ago), n); err != nil {
			t.Fatal(err)
		}
	}
	rule := NewThresholdRule(AlertRuleBase{Name: "ecc"},
		ThresholdConfig{Metric: "ecc.corrected", Operator: ">", Value: 0, Duration: "5m", Aggregate: "increase"})

	seed(10*time.Minute, 5) // baseline before the window
	seed(4*time.Minute, 5)
	if alert, err := rule.Evaluate(database); err != nil || alert != nil {
		t.Fatalf("no new errors = (%+v, %v), want (nil, nil)", alert, err)
	}
	seed(2*time.Minute, 0) // reboot
	seed(1*time.Minute, 2) // two new errors since boot
	alert, err := rule.Evaluate(database)
	if err != nil || alert == nil {
		t.Fatalf("errors after reboot = (%+v, %v), want an alert", alert, err)
	}
	if want := "ecc.corrected increase 2.0 > 0.0 over 5m"; alert.Message != want {
		t.Errorf("message = %q, want %q", alert.Message, want)
	}
}

// TestThresholdMessageNamesSubject: SMART alerts name the failing device(s), and
// value metrics name the rule's mount/interface/sensor.
func TestThresholdMessageNamesSubject(t *testing.T) {
	now := time.Now()
	database := newRuleTestDB(t)
	seedSMART(t, database, "sdb", now, 0, false)
	seedSMART(t, database, "sda", now, 0, false)
	seedSMART(t, database, "nvme0n1", now, 0, true)
	r := NewThresholdRule(AlertRuleBase{Name: "u"}, ThresholdConfig{Metric: "smart.unhealthy", Operator: ">", Value: 0, Duration: "5m"})
	alert, err := r.Evaluate(database)
	if err != nil || alert == nil || !strings.HasPrefix(alert.Message, "smart.unhealthy on sda, sdb count 2") {
		t.Errorf("unhealthy = (%+v, %v), want a message naming sda, sdb", alert, err)
	}

	if _, err := database.Exec("INSERT INTO dimension_values (id, category, value) VALUES (1, 'mount', '/srv')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO disk_metrics (ts, mount_id, total_bytes, used_bytes, free_bytes) VALUES (?, 1, 100, 95, 5)`, now); err != nil {
		t.Fatal(err)
	}
	r = NewThresholdRule(AlertRuleBase{Name: "d"}, ThresholdConfig{Metric: "disk.used_pct", Operator: ">", Value: 90, Duration: "5m", Mount: "/srv"})
	alert, err = r.Evaluate(database)
	if err != nil || alert == nil || alert.Message != "disk.used_pct on /srv avg 95.0 > 90.0 over 5m" {
		t.Errorf("disk = (%+v, %v), want the mount named", alert, err)
	}
}
