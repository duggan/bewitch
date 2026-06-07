package alert

import (
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/duggan/bewitch/internal/config"
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
	// Aggregate is the windowed reduction applied to the AVG-backed metrics:
	// "avg" (default), "max" (catch a transient spike that averages out), or
	// "min". The SMART metrics ignore it (their MAX/COUNT aggregate is intrinsic).
	// Empty is treated as "avg" for back-compat with pre-migration rows.
	Aggregate string
}

// aggFunc maps the configured aggregate to its SQL function and the lowercase
// label used in the fired-alert message. Anything unrecognised (including the
// empty string from an old rule) falls back to AVG so behaviour is unchanged.
func aggFunc(aggregate string) (sqlFn, label string) {
	switch aggregate {
	case "max":
		return "MAX", "max"
	case "min":
		return "MIN", "min"
	default:
		return "AVG", "avg"
	}
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
	// Resolved marks a recovery/all-clear notification (the condition cleared).
	// Not persisted; it only shapes how notifiers render the message.
	Resolved bool `json:"-"`
}

// Rule evaluates whether an alert condition is met.
type Rule interface {
	ID() int
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

func (r *ThresholdRule) ID() int      { return r.base.ID }
func (r *ThresholdRule) Name() string { return r.base.Name }

func (r *ThresholdRule) Evaluate(db *sql.DB) (*Alert, error) {
	dur, err := config.ParseDuration(r.cfg.Duration)
	if err != nil {
		return nil, fmt.Errorf("parsing duration %q: %w", r.cfg.Duration, err)
	}
	cutoff := time.Now().Add(-dur)

	query, args, agg, err := r.buildQuery(cutoff)
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
			// "agg" (avg/max/count) names what was actually computed, and "over"
			// (not "for") makes clear the comparison is across the window, not a
			// value sustained for its whole length — a 30s spike on an idle box
			// averages below threshold and won't fire, and the text no longer
			// implies otherwise. A per-rule avg/max/min choice is a future follow-up.
			Message: fmt.Sprintf("%s %s %.1f %s %.1f over %s", r.cfg.Metric, agg, avg.Float64, r.cfg.Operator, r.cfg.Value, r.cfg.Duration),
		}, nil
	}
	return nil, nil
}

// buildQuery returns the SQL, its bind args, and a label naming the aggregate it
// computes ("avg"/"max"/"min" for the value metrics, "max"/"count" for SMART) so
// the fired-alert message can be truthful about what the compared number is. The
// value metrics honour the rule's configured aggregate; the SMART metrics keep
// their intrinsic MAX/COUNT and ignore it.
func (r *ThresholdRule) buildQuery(cutoff time.Time) (string, []any, string, error) {
	fn, agg := aggFunc(r.cfg.Aggregate)
	switch r.cfg.Metric {
	case "cpu.aggregate":
		// core = -1 is the whole-CPU aggregate (core = 0 was just physical core 0).
		// 100 - idle counts everything non-idle — including nice/irq/softirq and,
		// crucially, steal — so a contended VPS (high steal, low user+system) can
		// actually trip the alert instead of looking idle.
		return fmt.Sprintf("SELECT %s(100 - idle_pct) FROM cpu_metrics WHERE core = -1 AND ts > ?", fn), []any{cutoff}, agg, nil
	case "memory.used_pct":
		return fmt.Sprintf("SELECT %s(CAST(used_bytes AS DOUBLE) / NULLIF(total_bytes, 0) * 100) FROM memory_metrics WHERE ts > ?", fn), []any{cutoff}, agg, nil
	case "disk.used_pct":
		return fmt.Sprintf(`SELECT %s(CAST(m.used_bytes AS DOUBLE) / NULLIF(m.total_bytes, 0) * 100)
			FROM disk_metrics m
			JOIN dimension_values d ON d.category = 'mount' AND d.id = m.mount_id
			WHERE d.value = ? AND m.ts > ?`, fn), []any{r.cfg.Mount, cutoff}, agg, nil
	case "network.rx":
		return fmt.Sprintf(`SELECT %s(m.rx_bytes_sec)
			FROM network_metrics m
			JOIN dimension_values d ON d.category = 'interface' AND d.id = m.interface_id
			WHERE d.value = ? AND m.ts > ?`, fn), []any{r.cfg.InterfaceName, cutoff}, agg, nil
	case "network.tx":
		return fmt.Sprintf(`SELECT %s(m.tx_bytes_sec)
			FROM network_metrics m
			JOIN dimension_values d ON d.category = 'interface' AND d.id = m.interface_id
			WHERE d.value = ? AND m.ts > ?`, fn), []any{r.cfg.InterfaceName, cutoff}, agg, nil
	case "temperature.sensor":
		return fmt.Sprintf(`SELECT %s(m.temp_celsius)
			FROM temperature_metrics m
			JOIN dimension_values d ON d.category = 'sensor' AND d.id = m.sensor_id
			WHERE d.value = ? AND m.ts > ?`, fn), []any{r.cfg.Sensor, cutoff}, agg, nil
	case "gpu.utilization":
		return fmt.Sprintf(`SELECT %s(m.utilization_pct)
			FROM gpu_metrics m
			JOIN dimension_values d ON d.category = 'gpu' AND d.id = m.gpu_id
			WHERE d.value = ? AND m.ts > ?`, fn), []any{r.cfg.Sensor, cutoff}, agg, nil
	case "gpu.temperature":
		return fmt.Sprintf(`SELECT %s(m.temp_celsius)
			FROM gpu_metrics m
			JOIN dimension_values d ON d.category = 'gpu' AND d.id = m.gpu_id
			WHERE d.value = ? AND m.ts > ?`, fn), []any{r.cfg.Sensor, cutoff}, agg, nil
	case "gpu.power":
		return fmt.Sprintf(`SELECT %s(m.power_watts)
			FROM gpu_metrics m
			JOIN dimension_values d ON d.category = 'gpu' AND d.id = m.gpu_id
			WHERE d.value = ? AND m.ts > ?`, fn), []any{r.cfg.Sensor, cutoff}, agg, nil
	// SMART metrics aggregate across all physical devices (the worst value over the
	// window) so a failing drive trips the alert without a per-device scope — fire
	// e.g. smart.reallocated > 0 or smart.percent_used > 90.
	case "smart.reallocated":
		return "SELECT MAX(reallocated_sectors) FROM smart_metrics WHERE ts > ?", []any{cutoff}, "max", nil
	case "smart.pending":
		return "SELECT MAX(pending_sectors) FROM smart_metrics WHERE ts > ?", []any{cutoff}, "max", nil
	case "smart.uncorrectable":
		return "SELECT MAX(uncorrectable_errs) FROM smart_metrics WHERE ts > ?", []any{cutoff}, "max", nil
	case "smart.percent_used":
		return "SELECT MAX(percent_used) FROM smart_metrics WHERE ts > ?", []any{cutoff}, "max", nil
	case "smart.unhealthy":
		// Count of unhealthy snapshots in the window; > 0 means a drive reported a
		// SMART health failure (the "your disk is dying" signal).
		return "SELECT COUNT(*) FROM smart_metrics WHERE healthy = false AND ts > ?", []any{cutoff}, "count", nil
	default:
		return "", nil, "", fmt.Errorf("unsupported threshold metric: %s", r.cfg.Metric)
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

func (r *PredictiveRule) ID() int      { return r.base.ID }
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

	// Already-breached path (covers both blind spots that made this rule go silent
	// exactly when the disk was most at risk): a disk already at/over the target
	// crossed it in the past, so the future-crossing math below gives hoursUntil<=0
	// and never fires; and a disk pinned flat-at-99% has slope<=0 and hits the
	// "not increasing" early-return. The query already ORDER BY m.ts, so the last
	// sample is the latest value. The message is intentionally distinct from the
	// "predicted to reach" wording, but the engine debounce keys on rule_name (not
	// message text) so a rule hovering at the boundary won't double-fire.
	current := ys[len(ys)-1]
	if current >= r.cfg.ThresholdPct {
		return &Alert{
			RuleName: r.base.Name,
			Severity: r.base.Severity,
			Message:  fmt.Sprintf("%s on %s already at %.0f%% (target %.0f%%)", r.cfg.Metric, r.cfg.Mount, current, r.cfg.ThresholdPct),
		}, nil
	}

	slope, intercept := linearRegression(xs, ys)
	if slope <= 0 {
		// Not increasing and not yet at the target — no concern.
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

func (r *VarianceRule) ID() int      { return r.base.ID }
func (r *VarianceRule) Name() string { return r.base.Name }

// varianceMetricSupported reports whether the variance rule can evaluate the
// given metric. Variance is memory-only (see Evaluate); the empty string is
// tolerated for robustness against any odd stored row.
func varianceMetricSupported(metric string) bool {
	switch metric {
	case "memory.variance", "memory.used_pct", "":
		return true
	}
	return false
}

func (r *VarianceRule) Evaluate(db *sql.DB) (*Alert, error) {
	dur, err := config.ParseDuration(r.cfg.Duration)
	if err != nil {
		return nil, fmt.Errorf("parsing duration %q: %w", r.cfg.Duration, err)
	}
	cutoff := time.Now().Add(-dur)

	// Variance is a memory-churn detector and the query below is memory-specific
	// (it counts abs-delta swings in memory used %). The configured Metric is
	// stored and round-tripped but was never read here, so a non-memory rule
	// (only creatable by hand via the API/REPL/raw DB — the TUI always emits
	// "memory.variance") silently ran memory variance under another metric's
	// label. Guard it so that mistake errors loudly instead.
	if !varianceMetricSupported(r.cfg.Metric) {
		return nil, fmt.Errorf("unsupported variance metric %q (variance only supports memory)", r.cfg.Metric)
	}

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

func (r *ProcessDownRule) ID() int      { return r.base.ID }
func (r *ProcessDownRule) Name() string { return r.base.Name }

func (r *ProcessDownRule) Evaluate(db *sql.DB) (*Alert, error) {
	// Match predicate: exact comm name, or a cmdline glob.
	matchClause, matchArg := "pi.name = ?", any(r.cfg.ProcessName)
	if r.cfg.ProcessPattern != "" {
		matchClause, matchArg = "pi.cmdline LIKE ?", any(globToSQL(r.cfg.ProcessPattern))
	}

	dur, derr := config.ParseDuration(r.cfg.CheckDuration)
	if derr != nil || dur <= 0 {
		// Back-compat: no/invalid check_duration → single newest-snapshot check.
		query := fmt.Sprintf(`WITH latest AS (
				SELECT MAX(ts) AS ts FROM process_metrics
			)
			SELECT COUNT(DISTINCT (pm.pid, pm.start_time))
			FROM process_info pi
			JOIN process_metrics pm ON pm.pid = pi.pid AND pm.start_time = pi.start_time
			CROSS JOIN latest l
			WHERE %s AND pm.ts = l.ts`, matchClause)
		var count int
		if err := db.QueryRow(query, matchArg).Scan(&count); err != nil {
			return nil, fmt.Errorf("querying process count: %w", err)
		}
		return r.alertIfDown(count), nil
	}

	// CheckDuration set: require sustained absence across the whole window rather
	// than firing on a single snapshot. Count instances per distinct snapshot ts,
	// then take the MAX — a single healthy snapshot anywhere in the window clears
	// the alert, so one missed tick or a brief restart (which gets a fresh
	// start_time and reappears within a tick or two) no longer false-fires "down".
	// The LEFT JOINs are load-bearing: a snapshot where the process was entirely
	// absent must still contribute cnt=0, and the match predicate sits in the ON
	// clause so non-matching rows count as 0 rather than dropping the whole
	// snapshot (an INNER JOIN would silently inflate the peak). snaps==0 guards
	// startup — never fire before any process data has been written.
	cutoff := time.Now().Add(-dur)
	// cnt counts matching instances per snapshot, not the snapshot's whole process
	// set: pm is joined only on ts (so it carries every process present at that ts),
	// and the name/cmdline predicate lives in the pi LEFT JOIN's ON clause — so pi.*
	// is non-null only for matching instances. The CASE makes the count NULL-safe:
	// counting the bare struct (pi.pid, pi.start_time) would count the all-NULL
	// no-match row as a distinct value (a DuckDB struct of NULLs is itself non-null),
	// so a genuinely-absent snapshot would wrongly read as cnt=1. Returning NULL when
	// pi didn't match makes COUNT(DISTINCT ...) ignore it, yielding cnt=0. The LEFT
	// JOINs keep the snapshot row alive so an all-absent window still contributes 0.
	query := fmt.Sprintf(`WITH window_snaps AS (
			SELECT DISTINCT ts FROM process_metrics WHERE ts >= ?
		),
		per_snap AS (
			SELECT ws.ts,
				COUNT(DISTINCT CASE WHEN pi.pid IS NOT NULL THEN (pi.pid, pi.start_time) END) AS cnt
			FROM window_snaps ws
			LEFT JOIN process_metrics pm ON pm.ts = ws.ts
			LEFT JOIN process_info pi ON pi.pid = pm.pid AND pi.start_time = pm.start_time AND %s
			GROUP BY ws.ts
		)
		SELECT COUNT(*) AS snaps, COALESCE(MAX(cnt), 0) AS peak FROM per_snap`, matchClause)

	var snaps, peak int
	if err := db.QueryRow(query, cutoff, matchArg).Scan(&snaps, &peak); err != nil {
		return nil, fmt.Errorf("querying process count: %w", err)
	}
	if snaps == 0 {
		return nil, nil
	}
	return r.alertIfDown(peak), nil
}

// alertIfDown returns a down alert when the instance count is below the rule's
// minimum, or nil otherwise. Shared by the single-snapshot and windowed paths.
func (r *ProcessDownRule) alertIfDown(count int) *Alert {
	if count >= r.cfg.MinInstances {
		return nil
	}
	name := r.cfg.ProcessName
	if r.cfg.ProcessPattern != "" {
		name = r.cfg.ProcessPattern
	}
	return &Alert{
		RuleName: r.base.Name,
		Severity: r.base.Severity,
		Message:  fmt.Sprintf("process '%s' is down: %d of %d expected instances running", name, count, r.cfg.MinInstances),
	}
}

// ProcessThrashingRule fires when a process restarts too frequently.
type ProcessThrashingRule struct {
	base AlertRuleBase
	cfg  ProcessThrashingConfig
}

func NewProcessThrashingRule(base AlertRuleBase, cfg ProcessThrashingConfig) *ProcessThrashingRule {
	return &ProcessThrashingRule{base: base, cfg: cfg}
}

func (r *ProcessThrashingRule) ID() int      { return r.base.ID }
func (r *ProcessThrashingRule) Name() string { return r.base.Name }

func (r *ProcessThrashingRule) Evaluate(db *sql.DB) (*Alert, error) {
	dur, err := config.ParseDuration(r.cfg.RestartWindow)
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
