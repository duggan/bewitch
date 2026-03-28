package alert

import (
	"database/sql"
	"fmt"
	"math"
	"time"
)

// AlertRuleBase contains common fields for all alert rule types.
type AlertRuleBase struct {
	ID       int
	Name     string
	Type     string // "threshold", "predictive", "variance", "process_down", "process_thrashing"
	Severity string
	Enabled  bool
}

// ThresholdConfig holds parameters for threshold-based alerts.
type ThresholdConfig struct {
	Metric        string
	Operator      string
	Value         float64
	Duration      string
	Mount         string
	InterfaceName string
	Sensor        string
}

// PredictiveConfig holds parameters for predictive alerts.
type PredictiveConfig struct {
	Metric       string
	Mount        string
	PredictHours int
	ThresholdPct float64
}

// VarianceConfig holds parameters for variance-based alerts.
type VarianceConfig struct {
	Metric         string
	DeltaThreshold float64
	MinCount       int
	Duration       string
}

// ProcessDownConfig holds parameters for process-down alerts.
type ProcessDownConfig struct {
	ProcessName    string
	ProcessPattern string
	MinInstances   int
	CheckDuration  string
}

// ProcessThrashingConfig holds parameters for process-thrashing alerts.
type ProcessThrashingConfig struct {
	ProcessName      string
	ProcessPattern   string
	RestartThreshold int
	RestartWindow    string
}

// Alert represents a fired alert.
type Alert struct {
	RuleName string `json:"rule_name"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Rule evaluates whether an alert condition is met.
type Rule interface {
	Name() string
	Evaluate(db *sql.DB) (*Alert, error)
}

// ThresholdRule fires when a metric exceeds a threshold for a duration.
type ThresholdRule struct {
	base AlertRuleBase
	cfg  ThresholdConfig
}

func NewThresholdRule(base AlertRuleBase, cfg ThresholdConfig) *ThresholdRule {
	return &ThresholdRule{base: base, cfg: cfg}
}

func (r *ThresholdRule) Name() string { return r.base.Name }

func (r *ThresholdRule) Evaluate(db *sql.DB) (*Alert, error) {
	dur, err := time.ParseDuration(r.cfg.Duration)
	if err != nil {
		return nil, fmt.Errorf("parsing duration %q: %w", r.cfg.Duration, err)
	}
	cutoff := time.Now().Add(-dur)

	query, args, err := r.buildQuery(cutoff)
	if err != nil {
		return nil, err
	}

	var avg sql.NullFloat64
	if err := db.QueryRow(query, args...).Scan(&avg); err != nil || !avg.Valid {
		return nil, nil
	}

	if r.compare(avg.Float64) {
		return &Alert{
			RuleName: r.base.Name,
			Severity: r.base.Severity,
			Message:  fmt.Sprintf("%s %.1f %s %.1f for %s", r.cfg.Metric, avg.Float64, r.cfg.Operator, r.cfg.Value, r.cfg.Duration),
		}, nil
	}
	return nil, nil
}

func (r *ThresholdRule) buildQuery(cutoff time.Time) (string, []any, error) {
	switch r.cfg.Metric {
	case "cpu.aggregate":
		return "SELECT AVG(user_pct + system_pct) FROM cpu_metrics WHERE core = 0 AND ts > ?", []any{cutoff}, nil
	case "memory.used_pct":
		return "SELECT AVG(CAST(used_bytes AS DOUBLE) / NULLIF(total_bytes, 0) * 100) FROM memory_metrics WHERE ts > ?", []any{cutoff}, nil
	case "disk.used_pct":
		return `SELECT AVG(CAST(m.used_bytes AS DOUBLE) / NULLIF(m.total_bytes, 0) * 100)
			FROM disk_metrics m
			JOIN dimension_values d ON d.category = 'mount' AND d.id = m.mount_id
			WHERE d.value = ? AND m.ts > ?`, []any{r.cfg.Mount, cutoff}, nil
	case "network.rx":
		return `SELECT AVG(m.rx_bytes_sec)
			FROM network_metrics m
			JOIN dimension_values d ON d.category = 'interface' AND d.id = m.interface_id
			WHERE d.value = ? AND m.ts > ?`, []any{r.cfg.InterfaceName, cutoff}, nil
	case "network.tx":
		return `SELECT AVG(m.tx_bytes_sec)
			FROM network_metrics m
			JOIN dimension_values d ON d.category = 'interface' AND d.id = m.interface_id
			WHERE d.value = ? AND m.ts > ?`, []any{r.cfg.InterfaceName, cutoff}, nil
	case "temperature.sensor":
		return `SELECT AVG(m.temp_celsius)
			FROM temperature_metrics m
			JOIN dimension_values d ON d.category = 'sensor' AND d.id = m.sensor_id
			WHERE d.value = ? AND m.ts > ?`, []any{r.cfg.Sensor, cutoff}, nil
	case "gpu.utilization":
		return `SELECT AVG(m.utilization_pct)
			FROM gpu_metrics m
			JOIN dimension_values d ON d.category = 'gpu' AND d.id = m.gpu_id
			WHERE d.value = ? AND m.ts > ?`, []any{r.cfg.Sensor, cutoff}, nil
	case "gpu.temperature":
		return `SELECT AVG(m.temp_celsius)
			FROM gpu_metrics m
			JOIN dimension_values d ON d.category = 'gpu' AND d.id = m.gpu_id
			WHERE d.value = ? AND m.ts > ?`, []any{r.cfg.Sensor, cutoff}, nil
	case "gpu.power":
		return `SELECT AVG(m.power_watts)
			FROM gpu_metrics m
			JOIN dimension_values d ON d.category = 'gpu' AND d.id = m.gpu_id
			WHERE d.value = ? AND m.ts > ?`, []any{r.cfg.Sensor, cutoff}, nil
	default:
		return "", nil, fmt.Errorf("unsupported threshold metric: %s", r.cfg.Metric)
	}
}

func (r *ThresholdRule) compare(val float64) bool {
	switch r.cfg.Operator {
	case ">":
		return val > r.cfg.Value
	case ">=":
		return val >= r.cfg.Value
	case "<":
		return val < r.cfg.Value
	case "<=":
		return val <= r.cfg.Value
	default:
		return false
	}
}

// PredictiveRule fires when linear extrapolation predicts a threshold breach.
type PredictiveRule struct {
	base AlertRuleBase
	cfg  PredictiveConfig
}

func NewPredictiveRule(base AlertRuleBase, cfg PredictiveConfig) *PredictiveRule {
	return &PredictiveRule{base: base, cfg: cfg}
}

func (r *PredictiveRule) Name() string { return r.base.Name }

func (r *PredictiveRule) Evaluate(db *sql.DB) (*Alert, error) {
	lookback := time.Duration(r.cfg.PredictHours) * time.Hour
	cutoff := time.Now().Add(-lookback)

	query, args, err := r.buildQuery(cutoff)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying for prediction: %w", err)
	}
	defer rows.Close()

	var xs, ys []float64
	for rows.Next() {
		var ts time.Time
		var val float64
		if err := rows.Scan(&ts, &val); err != nil {
			continue
		}
		xs = append(xs, float64(ts.Unix()))
		ys = append(ys, val)
	}

	if len(xs) < 2 {
		return nil, nil
	}

	slope, intercept := linearRegression(xs, ys)
	if slope <= 0 {
		// Not increasing, no concern
		return nil, nil
	}

	// Predict when threshold_pct will be reached
	targetTime := (r.cfg.ThresholdPct - intercept) / slope
	now := float64(time.Now().Unix())
	hoursUntil := (targetTime - now) / 3600

	if hoursUntil > 0 && hoursUntil <= float64(r.cfg.PredictHours) {
		return &Alert{
			RuleName: r.base.Name,
			Severity: r.base.Severity,
			Message:  fmt.Sprintf("%s on %s predicted to reach %.0f%% in %.1f hours", r.cfg.Metric, r.cfg.Mount, r.cfg.ThresholdPct, hoursUntil),
		}, nil
	}
	return nil, nil
}

func (r *PredictiveRule) buildQuery(cutoff time.Time) (string, []any, error) {
	switch r.cfg.Metric {
	case "disk.used_pct":
		return `SELECT m.ts, CAST(m.used_bytes AS DOUBLE) / NULLIF(m.total_bytes, 0) * 100
			FROM disk_metrics m
			JOIN dimension_values d ON d.category = 'mount' AND d.id = m.mount_id
			WHERE d.value = ? AND m.ts > ? ORDER BY m.ts`, []any{r.cfg.Mount, cutoff}, nil
	default:
		return "", nil, fmt.Errorf("unsupported predictive metric: %s", r.cfg.Metric)
	}
}

// VarianceRule fires when memory usage changes exceed a threshold magnitude
// a certain number of times within a window, indicating thrashing or instability.
type VarianceRule struct {
	base AlertRuleBase
	cfg  VarianceConfig
}

func NewVarianceRule(base AlertRuleBase, cfg VarianceConfig) *VarianceRule {
	return &VarianceRule{base: base, cfg: cfg}
}

func (r *VarianceRule) Name() string { return r.base.Name }

func (r *VarianceRule) Evaluate(db *sql.DB) (*Alert, error) {
	dur, err := time.ParseDuration(r.cfg.Duration)
	if err != nil {
		return nil, fmt.Errorf("parsing duration %q: %w", r.cfg.Duration, err)
	}
	cutoff := time.Now().Add(-dur)

	rows, err := db.Query(
		"SELECT CAST(used_bytes AS DOUBLE) / NULLIF(total_bytes, 0) * 100 FROM memory_metrics WHERE ts > ? ORDER BY ts",
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("querying memory for variance: %w", err)
	}
	defer rows.Close()

	var values []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			continue
		}
		values = append(values, v)
	}

	if len(values) < 2 {
		return nil, nil
	}

	// Count successive deltas exceeding the threshold
	var count int
	for i := 1; i < len(values); i++ {
		delta := math.Abs(values[i] - values[i-1])
		if delta >= r.cfg.DeltaThreshold {
			count++
		}
	}

	if count >= r.cfg.MinCount {
		return &Alert{
			RuleName: r.base.Name,
			Severity: r.base.Severity,
			Message:  fmt.Sprintf("memory variance: %d changes exceeding %.1f%% in %s (threshold: %d)", count, r.cfg.DeltaThreshold, r.cfg.Duration, r.cfg.MinCount),
		}, nil
	}
	return nil, nil
}

// ProcessDownRule fires when a monitored process is not running.
type ProcessDownRule struct {
	base AlertRuleBase
	cfg  ProcessDownConfig
}

func NewProcessDownRule(base AlertRuleBase, cfg ProcessDownConfig) *ProcessDownRule {
	return &ProcessDownRule{base: base, cfg: cfg}
}

func (r *ProcessDownRule) Name() string { return r.base.Name }

func (r *ProcessDownRule) Evaluate(db *sql.DB) (*Alert, error) {
	// Count active instances of the process in the latest metrics snapshot
	var query string
	var args []any

	if r.cfg.ProcessPattern != "" {
		// Match by cmdline pattern (convert glob to SQL LIKE)
		pattern := globToSQL(r.cfg.ProcessPattern)
		query = `WITH latest AS (
				SELECT MAX(ts) as ts FROM process_metrics
			)
			SELECT COUNT(DISTINCT (pm.pid, pm.start_time))
			FROM process_info pi
			JOIN process_metrics pm ON pm.pid = pi.pid AND pm.start_time = pi.start_time
			CROSS JOIN latest l
			WHERE pi.cmdline LIKE ?
			  AND pm.ts = l.ts`
		args = []any{pattern}
	} else {
		// Match by exact process name
		query = `WITH latest AS (
				SELECT MAX(ts) as ts FROM process_metrics
			)
			SELECT COUNT(DISTINCT (pm.pid, pm.start_time))
			FROM process_info pi
			JOIN process_metrics pm ON pm.pid = pi.pid AND pm.start_time = pi.start_time
			CROSS JOIN latest l
			WHERE pi.name = ?
			  AND pm.ts = l.ts`
		args = []any{r.cfg.ProcessName}
	}

	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		return nil, fmt.Errorf("querying process count: %w", err)
	}

	if count < r.cfg.MinInstances {
		name := r.cfg.ProcessName
		if r.cfg.ProcessPattern != "" {
			name = r.cfg.ProcessPattern
		}
		return &Alert{
			RuleName: r.base.Name,
			Severity: r.base.Severity,
			Message:  fmt.Sprintf("process '%s' is down: %d of %d expected instances running", name, count, r.cfg.MinInstances),
		}, nil
	}
	return nil, nil
}

// ProcessThrashingRule fires when a process restarts too frequently.
type ProcessThrashingRule struct {
	base AlertRuleBase
	cfg  ProcessThrashingConfig
}

func NewProcessThrashingRule(base AlertRuleBase, cfg ProcessThrashingConfig) *ProcessThrashingRule {
	return &ProcessThrashingRule{base: base, cfg: cfg}
}

func (r *ProcessThrashingRule) Name() string { return r.base.Name }

func (r *ProcessThrashingRule) Evaluate(db *sql.DB) (*Alert, error) {
	dur, err := time.ParseDuration(r.cfg.RestartWindow)
	if err != nil {
		return nil, fmt.Errorf("parsing restart window %q: %w", r.cfg.RestartWindow, err)
	}
	cutoff := time.Now().Add(-dur)

	// Count distinct (pid, start_time) pairs where first_seen is within the window
	// Each new start_time for the same process name = a restart
	var query string
	var args []any

	if r.cfg.ProcessPattern != "" {
		pattern := globToSQL(r.cfg.ProcessPattern)
		query = `SELECT COUNT(DISTINCT (pid, start_time))
			FROM process_info
			WHERE cmdline LIKE ?
			  AND first_seen > ?`
		args = []any{pattern, cutoff}
	} else {
		query = `SELECT COUNT(DISTINCT (pid, start_time))
			FROM process_info
			WHERE name = ?
			  AND first_seen > ?`
		args = []any{r.cfg.ProcessName, cutoff}
	}

	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		return nil, fmt.Errorf("querying restart count: %w", err)
	}

	if count >= r.cfg.RestartThreshold {
		name := r.cfg.ProcessName
		if r.cfg.ProcessPattern != "" {
			name = r.cfg.ProcessPattern
		}
		return &Alert{
			RuleName: r.base.Name,
			Severity: r.base.Severity,
			Message:  fmt.Sprintf("process '%s' is thrashing: %d restarts in last %s (threshold: %d)", name, count, r.cfg.RestartWindow, r.cfg.RestartThreshold),
		}, nil
	}
	return nil, nil
}

// globToSQL converts a glob pattern to SQL LIKE pattern.
func globToSQL(pattern string) string {
	// Simple conversion: * -> %, ? -> _
	result := ""
	for _, c := range pattern {
		switch c {
		case '*':
			result += "%"
		case '?':
			result += "_"
		case '%', '_':
			// Escape SQL wildcards in the original pattern
			result += "\\" + string(c)
		default:
			result += string(c)
		}
	}
	return result
}

// linearRegression computes slope and intercept via least squares.
func linearRegression(xs, ys []float64) (slope, intercept float64) {
	n := float64(len(xs))
	var sumX, sumY, sumXY, sumX2 float64
	for i := range xs {
		sumX += xs[i]
		sumY += ys[i]
		sumXY += xs[i] * ys[i]
		sumX2 += xs[i] * xs[i]
	}
	denom := n*sumX2 - sumX*sumX
	if math.Abs(denom) < 1e-10 {
		return 0, 0
	}
	slope = (n*sumXY - sumX*sumY) / denom
	intercept = (sumY - slope*sumX) / n
	return
}
