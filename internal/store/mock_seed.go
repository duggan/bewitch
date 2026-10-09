package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/charmbracelet/log"

	"github.com/duggan/bewitch/internal/collector"
)

// mockTier defines a time tier for history seeding with age-appropriate granularity.
type mockTier struct {
	age      time.Duration // how far back this tier starts (from now)
	interval time.Duration // sample interval within this tier
}

// mockTiers defines the time tiers for mock history seeding, ordered from
// oldest to newest. Coarser intervals for older data match the history API's
// bucket aggregation, keeping total row count manageable.
var mockTiers = []mockTier{
	{30 * 24 * time.Hour, 15 * time.Minute}, // 7d–30d ago: 6-hour buckets
	{7 * 24 * time.Hour, 5 * time.Minute},   // 1d–7d ago: 1-hour buckets
	{24 * time.Hour, 30 * time.Second},      // 1h–24h ago: 10-min buckets
	{1 * time.Hour, 5 * time.Second},        // last 1h: 1-min buckets
}

// mockSeedProcs is how many of the busiest processes each seeded sample
// records — enough for the process history chart's top 10 and the alert rules'
// targets, without the full live table's row count across 30 days.
const mockSeedProcs = 25

// SeedMockHistory populates the database with 30 days of history replayed from
// the same simulation the live mock collectors read, up to the moment the
// simulation started, so history runs seamlessly into live data. Uses tiered
// intervals (coarser for older data) to keep startup fast while giving the
// TUI's historical charts data at every zoom level. Samples go through
// WriteBatch, the live write path, so every table the daemon writes is covered
// (process and load history included).
func (s *Store) SeedMockHistory(sim *collector.MockSim, pins []string) error {
	end := sim.Anchor()
	// Idempotent: skip if history from before this run already exists.
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM cpu_metrics WHERE ts < ?", end.Add(-time.Minute)).Scan(&count); err == nil && count > 0 {
		log.Infof("mock history already seeded (%d cpu rows), skipping", count)
		return nil
	}

	totalSteps := 0
	for i, tier := range mockTiers {
		tierEnd := end
		if i+1 < len(mockTiers) {
			tierEnd = end.Add(-mockTiers[i+1].age)
		}
		totalSteps += int(tierEnd.Sub(end.Add(-tier.age)) / tier.interval)
	}
	log.Infof("seeding %d data points of mock history (30 days, tiered intervals)", totalSteps)

	sources := collector.MockCustomSources()
	const flushEvery = 200 // timestamps per WriteBatch
	var batch []collector.Sample
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := s.WriteBatch(batch)
		batch = batch[:0]
		return err
	}
	var lastSMART time.Time
	steps := 0
	for i, tier := range mockTiers {
		tierStart := end.Add(-tier.age)
		tierEnd := end
		if i+1 < len(mockTiers) {
			tierEnd = end.Add(-mockTiers[i+1].age)
		}
		for ts := tierStart; ts.Before(tierEnd); ts = ts.Add(tier.interval) {
			snap := sim.Snapshot(ts)
			disk := snap.Disk
			if ts.Sub(lastSMART) >= 5*time.Minute { // the real smart_interval cadence
				lastSMART = ts
			} else {
				disk.SMART = nil
			}
			all, full := snap.Processes()
			procs := collector.MockProcessData(all, full, mockSeedProcs, pins)
			batch = append(batch,
				collector.Sample{Timestamp: ts, Kind: "cpu", Data: snap.CPU},
				collector.Sample{Timestamp: ts, Kind: "memory", Data: snap.Memory},
				collector.Sample{Timestamp: ts, Kind: "load", Data: snap.Load},
				collector.Sample{Timestamp: ts, Kind: "disk", Data: disk},
				collector.Sample{Timestamp: ts, Kind: "network", Data: snap.Network},
				collector.Sample{Timestamp: ts, Kind: "temperature", Data: snap.Temperature},
				collector.Sample{Timestamp: ts, Kind: "power", Data: snap.Power},
				collector.Sample{Timestamp: ts, Kind: "gpu", Data: snap.GPU},
				collector.Sample{Timestamp: ts, Kind: "ecc", Data: snap.ECC},
				collector.Sample{Timestamp: ts, Kind: "process", Data: procs},
			)
			// Custom sources (the Services tab)
			t := float64(ts.UnixNano()) / 1e9
			for _, src := range sources {
				var data collector.CustomSourceData
				data.Source = src.Name
				for _, m := range src.Metrics {
					if v, ok := collector.MockCustomValue(src.Name, m.Name, t); ok {
						data.Metrics = append(data.Metrics, collector.CustomMetricSample{Name: m.Name, Unit: m.Unit, Value: v})
					}
				}
				if len(data.Metrics) > 0 {
					batch = append(batch, collector.Sample{Timestamp: ts, Kind: "custom", Data: data})
				}
			}
			if steps++; steps%flushEvery == 0 {
				if err := flush(); err != nil {
					return fmt.Errorf("writing mock history: %w", err)
				}
			}
		}
	}
	if err := flush(); err != nil {
		return fmt.Errorf("writing mock history: %w", err)
	}

	log.Infof("mock history seeded successfully")
	return nil
}

// SeedMock seeds mock history and demo alerts while holding the maintenance
// lock. A startup archive pass is followed by a compaction, which swaps the
// database file; without the lock, a concurrent seeding run could write into
// the file being replaced and its 30 days of history vanished. pins are the
// process patterns the live collector force-enriches (alert rule targets).
func (s *Store) SeedMock(sim *collector.MockSim, pins []string) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := SeedMockAlerts(s.DB(), sim.Anchor()); err != nil {
		return fmt.Errorf("alerts: %w", err)
	}
	if err := s.SeedMockHistory(sim, append([]string{"nginx"}, pins...)); err != nil {
		return fmt.Errorf("history: %w", err)
	}
	if sim.ScheduleScenario(time.Now().Add(collector.IncidentLeadIn)) {
		start, _ := sim.ScenarioStart()
		log.Infof("mock scenario starts at %s", start.Format("15:04:05"))
	}
	return nil
}

// SeedMockAlerts inserts the demo alert rules and a short, already-resolved
// alert history. The rules are what a real host would run, tuned so the
// "incident" mock scenario trips two of them (package temperature and root
// disk space, both "max over 1m") and recovers. No alert is active at start:
// the engine fires them live, edge-triggered, like any other.
func SeedMockAlerts(db *sql.DB, now time.Time) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM alert_rules").Scan(&count); err != nil {
		return fmt.Errorf("checking alert_rules: %w", err)
	}
	if count > 0 {
		return nil // already seeded
	}

	log.Infof("seeding mock alert rules and alert history")

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	rules := []struct {
		name, ruleType, severity string
		config                   string
		args                     []any
	}{
		{"CPU Critical", "threshold", "critical",
			"INSERT INTO alert_rule_threshold (rule_id, metric, operator, value, duration, aggregate) VALUES (?, 'cpu.aggregate', '>', 90, '5m', 'avg')", nil},
		{"Root Disk Space", "threshold", "warning",
			"INSERT INTO alert_rule_threshold (rule_id, metric, operator, value, duration, mount, aggregate) VALUES (?, 'disk.used_pct', '>', 90, '1m', '/', 'max')", nil},
		{"Package Temperature", "threshold", "warning",
			"INSERT INTO alert_rule_threshold (rule_id, metric, operator, value, duration, sensor, aggregate) VALUES (?, 'temperature.sensor', '>', 85, '1m', 'coretemp/Package id 0', 'max')", nil},
		{"Root Disk Fill Prediction", "predictive", "warning",
			"INSERT INTO alert_rule_predictive (rule_id, metric, mount, predict_hours, threshold_pct) VALUES (?, 'disk.used_pct', '/', 48, 95)", nil},
		{"Nginx Workers", "process_down", "critical",
			"INSERT INTO alert_rule_process_down (rule_id, process_name, process_pattern, min_instances, check_duration) VALUES (?, 'nginx', '', 4, '1m')", nil},
		{"Memory Pressure", "variance", "warning",
			"INSERT INTO alert_rule_variance (rule_id, metric, delta_threshold, min_count, duration) VALUES (?, 'memory.variance', 5, 10, '30m')", nil},
	}
	for _, r := range rules {
		var id int64
		if err := tx.QueryRow(
			"INSERT INTO alert_rules (name, type, severity, enabled) VALUES (?, ?, ?, true) RETURNING id",
			r.name, r.ruleType, r.severity,
		).Scan(&id); err != nil {
			return fmt.Errorf("inserting rule %s: %w", r.name, err)
		}
		if _, err := tx.Exec(r.config, id); err != nil {
			return fmt.Errorf("inserting %s config: %w", r.name, err)
		}
	}

	// Default the charts to 24h: enough history to show the daily rhythm.
	if _, err := tx.Exec("INSERT INTO preferences (key, value) VALUES ('history_range', '24h') ON CONFLICT (key) DO UPDATE SET value = '24h'"); err != nil {
		return fmt.Errorf("setting history range: %w", err)
	}

	// Past alerts, all resolved, worded exactly as the engine words them.
	day := func(daysAgo int, hour, min int) time.Time {
		d := now.AddDate(0, 0, -daysAgo)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, min, 0, 0, d.Location())
	}
	history := []struct {
		ts       time.Time
		lasted   time.Duration
		rule     string
		severity string
		message  string
		acked    bool
	}{
		{day(12, 15, 42), 9 * time.Minute, "CPU Critical", "critical", "cpu.aggregate avg 91.4 > 90.0 over 5m", true},
		{day(6, 10, 3), 70 * time.Second, "Nginx Workers", "critical", "process 'nginx' is down: 1 of 4 expected instances running", true},
		{day(2, 2, 51), 4 * time.Minute, "Package Temperature", "warning", "temperature.sensor max 86.3 > 85.0 over 1m", true},
		{day(1, 14, 18), 3 * time.Minute, "Package Temperature", "warning", "temperature.sensor max 85.6 > 85.0 over 1m", false},
	}
	for _, a := range history {
		if _, err := tx.Exec(
			"INSERT INTO alerts (ts, rule_name, severity, message, acknowledged, resolved_at) VALUES (?, ?, ?, ?, ?, ?)",
			a.ts, a.rule, a.severity, a.message, a.acked, a.ts.Add(a.lasted),
		); err != nil {
			return fmt.Errorf("inserting alert history: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	log.Infof("mock alerts seeded: %d rules, %d past alerts", len(rules), len(history))
	return nil
}
