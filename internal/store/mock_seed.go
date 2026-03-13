package store

import (
	"database/sql"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/charmbracelet/log"
)

// smoothWaveAt returns a value oscillating between min and max using a sine wave
// with the given period (seconds) and phase offset at a specific time, plus jitter.
func smoothWaveAt(t, min, max, periodSec, phase float64) float64 {
	base := (math.Sin(t*2*math.Pi/periodSec+phase) + 1) / 2
	noise := (rand.Float64() - 0.5) * 0.05
	val := min + (max-min)*(base+noise)
	return math.Max(min, math.Min(max, val))
}

// SeedMockHistory populates the database with realistic historical data for
// all collectors. This makes mock mode useful for demos and screenshots by
// giving the TUI's historical charts data to display immediately.
func (s *Store) SeedMockHistory(duration time.Duration, interval time.Duration) error {
	// Check if history already exists (idempotent)
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM cpu_metrics").Scan(&count); err == nil && count > 0 {
		log.Infof("mock history already seeded (%d cpu rows), skipping", count)
		return nil
	}

	now := time.Now()
	start := now.Add(-duration)
	steps := int(duration / interval)

	log.Infof("seeding %d data points of mock history (%v at %v intervals)", steps, duration, interval)

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Ensure dimension values exist
	dims := []struct {
		cat, val string
	}{
		{"mount", "/"}, {"mount", "/home"},
		{"device", "/dev/nvme0n1p2"}, {"device", "/dev/sda1"},
		{"interface", "eth0"}, {"interface", "wlan0"},
		{"sensor", "coretemp/Core 0"}, {"sensor", "coretemp/Core 1"},
		{"sensor", "coretemp/Package id 0"}, {"sensor", "acpitz/temp1"},
		{"zone", "package-0"}, {"zone", "package-0/core"}, {"zone", "package-0/uncore"},
	}
	dimIDs := make(map[string]int16)
	for _, d := range dims {
		id, err := s.getDimensionID(tx, d.cat, d.val)
		if err != nil {
			return fmt.Errorf("dimension %s/%s: %w", d.cat, d.val, err)
		}
		dimIDs[d.cat+"/"+d.val] = id
	}

	// Prepare batch insert statements
	cpuStmt, err := tx.Prepare("INSERT INTO cpu_metrics (ts, core, user_pct, system_pct, idle_pct, iowait_pct) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		return fmt.Errorf("prepare cpu: %w", err)
	}
	defer cpuStmt.Close()

	memStmt, err := tx.Prepare("INSERT INTO memory_metrics (ts, total_bytes, used_bytes, available_bytes, buffers_bytes, cached_bytes, swap_total_bytes, swap_used_bytes) VALUES (?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return fmt.Errorf("prepare memory: %w", err)
	}
	defer memStmt.Close()

	diskStmt, err := tx.Prepare("INSERT INTO disk_metrics (ts, mount_id, device_id, total_bytes, used_bytes, free_bytes, read_bytes_sec, write_bytes_sec, read_iops, write_iops) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return fmt.Errorf("prepare disk: %w", err)
	}
	defer diskStmt.Close()

	netStmt, err := tx.Prepare("INSERT INTO network_metrics (ts, interface_id, rx_bytes_sec, tx_bytes_sec, rx_packets_sec, tx_packets_sec, rx_errors, tx_errors) VALUES (?, ?, ?, ?, ?, ?, 0, 0)")
	if err != nil {
		return fmt.Errorf("prepare network: %w", err)
	}
	defer netStmt.Close()

	tempStmt, err := tx.Prepare("INSERT INTO temperature_metrics (ts, sensor_id, temp_celsius) VALUES (?, ?, ?)")
	if err != nil {
		return fmt.Errorf("prepare temperature: %w", err)
	}
	defer tempStmt.Close()

	powerStmt, err := tx.Prepare("INSERT INTO power_metrics (ts, zone_id, watts) VALUES (?, ?, ?)")
	if err != nil {
		return fmt.Errorf("prepare power: %w", err)
	}
	defer powerStmt.Close()

	const (
		memTotal     = 32 * 1024 * 1024 * 1024
		swapTotal    = 8 * 1024 * 1024 * 1024
		rootTotal    = 500e9
		homeTotal    = 2000e9
		numCores     = 8
	)

	for i := 0; i < steps; i++ {
		ts := start.Add(time.Duration(i) * interval)
		t := float64(ts.UnixNano()) / 1e9

		// CPU - aggregate + 8 cores
		{
			user := smoothWaveAt(t, 5, 25, 120, 0)
			sys := smoothWaveAt(t, 2, 10, 90, 1.0)
			iow := smoothWaveAt(t, 0, 3, 200, 2.0)
			idle := math.Max(0, 100-user-sys-iow)
			cpuStmt.Exec(ts, -1, user, sys, idle, iow)

			for c := 0; c < numCores; c++ {
				phase := float64(c) * 0.7
				u := smoothWaveAt(t, 2, 40, 60+float64(c)*30, phase)
				sv := smoothWaveAt(t, 1, 12, 80+float64(c)*20, phase+0.5)
				w := smoothWaveAt(t, 0, 4, 150, phase+1.0)
				id := math.Max(0, 100-u-sv-w)
				cpuStmt.Exec(ts, c, u, sv, id, w)
			}
		}

		// Memory
		{
			usedPct := smoothWaveAt(t, 0.40, 0.65, 600, 0)
			used := uint64(float64(memTotal) * usedPct)
			buffers := uint64(float64(memTotal) * smoothWaveAt(t, 0.01, 0.03, 300, 1.5))
			cached := uint64(float64(memTotal) * smoothWaveAt(t, 0.10, 0.20, 400, 2.0))
			swapUsed := uint64(float64(swapTotal) * smoothWaveAt(t, 0, 0.02, 900, 3.0))
			memStmt.Exec(ts, memTotal, used, memTotal-used, buffers, cached, swapTotal, swapUsed)
		}

		// Disk
		{
			rootUsed := uint64(rootTotal * smoothWaveAt(t, 0.58, 0.62, 1800, 0))
			diskStmt.Exec(ts,
				dimIDs["mount//"], dimIDs["device//dev/nvme0n1p2"],
				uint64(rootTotal), rootUsed, uint64(rootTotal)-rootUsed,
				smoothWaveAt(t, 0, 50e6, 60, 0.5), smoothWaveAt(t, 0, 30e6, 45, 1.0),
				smoothWaveAt(t, 0, 5000, 60, 0.5), smoothWaveAt(t, 0, 3000, 45, 1.0),
			)
			homeUsed := uint64(homeTotal * smoothWaveAt(t, 0.43, 0.47, 2400, 1.0))
			diskStmt.Exec(ts,
				dimIDs["mount//home"], dimIDs["device//dev/sda1"],
				uint64(homeTotal), homeUsed, uint64(homeTotal)-homeUsed,
				smoothWaveAt(t, 0, 20e6, 90, 2.0), smoothWaveAt(t, 0, 10e6, 70, 2.5),
				smoothWaveAt(t, 0, 2000, 90, 2.0), smoothWaveAt(t, 0, 1000, 70, 2.5),
			)
		}

		// Network
		{
			netStmt.Exec(ts,
				dimIDs["interface/eth0"],
				smoothWaveAt(t, 1e6, 50e6, 45, 0), smoothWaveAt(t, 0.5e6, 20e6, 60, 0.5),
				smoothWaveAt(t, 1000, 40000, 45, 0), smoothWaveAt(t, 500, 15000, 60, 0.5),
			)
			netStmt.Exec(ts,
				dimIDs["interface/wlan0"],
				smoothWaveAt(t, 0.1e6, 5e6, 80, 2.0), smoothWaveAt(t, 0.05e6, 2e6, 100, 2.5),
				smoothWaveAt(t, 100, 5000, 80, 2.0), smoothWaveAt(t, 50, 2000, 100, 2.5),
			)
		}

		// Temperature
		{
			sensors := []struct {
				key          string
				min, max     float64
				period, phase float64
			}{
				{"sensor/coretemp/Core 0", 38, 68, 90, 0},
				{"sensor/coretemp/Core 1", 36, 65, 100, 0.8},
				{"sensor/coretemp/Package id 0", 42, 75, 80, 0.3},
				{"sensor/acpitz/temp1", 30, 40, 300, 1.5},
			}
			for _, s := range sensors {
				tempStmt.Exec(ts, dimIDs[s.key], smoothWaveAt(t, s.min, s.max, s.period, s.phase))
			}
		}

		// Power
		{
			zones := []struct {
				key          string
				min, max     float64
				period, phase float64
			}{
				{"zone/package-0", 15, 65, 60, 0},
				{"zone/package-0/core", 8, 45, 50, 0.5},
				{"zone/package-0/uncore", 2, 10, 120, 1.0},
			}
			for _, z := range zones {
				powerStmt.Exec(ts, dimIDs[z.key], smoothWaveAt(t, z.min, z.max, z.period, z.phase))
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	log.Infof("mock history seeded successfully")
	return nil
}

// SeedMockAlerts inserts demo alert rules and fired alerts into the database.
func SeedMockAlerts(db *sql.DB) error {
	// Check if rules already exist (idempotent)
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM alert_rules").Scan(&count); err != nil {
		return fmt.Errorf("checking alert_rules: %w", err)
	}
	if count > 0 {
		return nil // already seeded
	}

	log.Infof("seeding mock alert rules and fired alerts")

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Insert alert rules (base table)
	rules := []struct {
		name, ruleType, severity string
	}{
		{"CPU Critical", "threshold", "critical"},
		{"Root Disk Space", "threshold", "warning"},
		{"Temperature Warning", "threshold", "warning"},
		{"Root Disk Fill Prediction", "predictive", "warning"},
		{"Nginx Worker Count", "process_down", "critical"},
		{"Memory Pressure", "variance", "warning"},
	}

	ruleIDs := make([]int64, len(rules))
	for i, r := range rules {
		var id int64
		err := tx.QueryRow(
			"INSERT INTO alert_rules (name, type, severity, enabled) VALUES (?, ?, ?, true) RETURNING id",
			r.name, r.ruleType, r.severity,
		).Scan(&id)
		if err != nil {
			return fmt.Errorf("inserting rule %s: %w", r.name, err)
		}
		ruleIDs[i] = id
	}

	// Insert type-specific rule parameters
	// CPU > 90% for 5m
	tx.Exec("INSERT INTO alert_rule_threshold (rule_id, metric, operator, value, duration) VALUES (?, 'cpu.aggregate', '>', 90, '5m')", ruleIDs[0])
	// Disk > 85%
	tx.Exec("INSERT INTO alert_rule_threshold (rule_id, metric, operator, value, duration, mount) VALUES (?, 'disk.used_pct', '>', 85, '10m', '/')", ruleIDs[1])
	// Temperature > 70°C
	tx.Exec("INSERT INTO alert_rule_threshold (rule_id, metric, operator, value, duration, sensor) VALUES (?, 'temperature.sensor', '>', 70, '3m', 'coretemp/Package id 0')", ruleIDs[2])
	// Disk fill prediction < 48h
	tx.Exec("INSERT INTO alert_rule_predictive (rule_id, metric, mount, predict_hours, threshold_pct) VALUES (?, 'disk.used_pct', '/', 48, 90)", ruleIDs[3])
	// Nginx min 2 workers
	tx.Exec("INSERT INTO alert_rule_process_down (rule_id, process_name, process_pattern, min_instances, check_duration) VALUES (?, 'nginx', 'nginx', 2, '1m')", ruleIDs[4])
	// Memory variance
	tx.Exec("INSERT INTO alert_rule_variance (rule_id, metric, delta_threshold, min_count, duration) VALUES (?, 'memory.variance', 5, 10, '30m')", ruleIDs[5])

	// Set history range preference to 1h so charts show the seeded data nicely
	tx.Exec("INSERT INTO preferences (key, value) VALUES ('history_range', '1h') ON CONFLICT (key) DO UPDATE SET value = '1h'")

	// Insert some fired alerts with realistic timestamps and messages
	now := time.Now()
	firedAlerts := []struct {
		ts       time.Time
		rule     string
		severity string
		message  string
		acked    bool
	}{
		{now.Add(-47 * time.Minute), "Temperature Warning", "warning", "coretemp/Package id 0 at 72.3°C (threshold: 70°C for 3m)", true},
		{now.Add(-32 * time.Minute), "CPU Critical", "critical", "CPU aggregate at 93.1% (threshold: >90% for 5m)", true},
		{now.Add(-18 * time.Minute), "Temperature Warning", "warning", "coretemp/Package id 0 at 71.8°C (threshold: 70°C for 3m)", false},
		{now.Add(-7 * time.Minute), "Root Disk Space", "warning", "/ at 86.2% used (threshold: >85% for 10m)", false},
		{now.Add(-3 * time.Minute), "Memory Pressure", "warning", "Memory variance: 12 swings of >5% in 30m (threshold: 10)", false},
	}

	for _, a := range firedAlerts {
		tx.Exec(
			"INSERT INTO alerts (ts, rule_name, severity, message, acknowledged) VALUES (?, ?, ?, ?, ?)",
			a.ts, a.rule, a.severity, a.message, a.acked,
		)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	log.Infof("mock alerts seeded: %d rules, %d fired alerts", len(rules), len(firedAlerts))
	return nil
}
