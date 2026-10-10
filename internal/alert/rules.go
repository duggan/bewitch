package alert

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
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
	// Aggregate is the windowed reduction applied to the value metrics: "avg"
	// (default), "max" (catch a transient spike that averages out), or "min".
	// Counter metrics (IsCounterMetric) take "increase" (growth since the last
	// reading before the window) and otherwise compare the current count. Empty
	// is treated as "avg" for back-compat with pre-migration rows.
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

// IsCounterMetric reports whether a threshold metric is a cumulative counter
// (SMART sector/error counts, ECC error counts since boot). Comparing such a
// counter's level fires forever once a drive has ever had a reallocated sector,
// so these metrics also accept aggregate "increase".
func IsCounterMetric(metric string) bool {
	switch metric {
	case "smart.reallocated", "smart.pending", "smart.uncorrectable", "ecc.uncorrectable", "ecc.corrected":
		return true
	}
	return false
}

// counterColumns maps each counter metric to its table and column.
var counterColumns = map[string][2]string{
	"smart.reallocated":   {"smart_metrics", "reallocated_sectors"},
	"smart.pending":       {"smart_metrics", "pending_sectors"},
	"smart.uncorrectable": {"smart_metrics", "uncorrectable_errs"},
	"ecc.uncorrectable":   {"ecc_metrics", "uncorrected"},
	"ecc.corrected":       {"ecc_metrics", "corrected"},
}

// increaseBaselineLookback bounds how far before the window the "increase" query
// looks for a baseline reading, so it doesn't scan the counter's whole history
// every evaluation. A longer collection gap falls back to the window's minimum.
const increaseBaselineLookback = 24 * time.Hour

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

// ErrNoData is returned by Rule.Evaluate when the rule's window holds no samples
// to judge (a mount was unmounted, a sensor vanished, SMART rows are only written
// every smart_interval, collection has not started yet). It is distinct from
// "evaluated and not breaching": the engine holds the rule's current state rather
// than resolving an active alert, so missing data can never send an all-clear.
var ErrNoData = errors.New("no data in window")

// Rule evaluates whether an alert condition is met. Evaluate returns a non-nil
// Alert when breaching, (nil, nil) when evaluated and healthy, and ErrNoData when
// there was nothing to evaluate.
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

	// Every query yields (value, subject). subject names the worst SMART device
	// (or the failing ones, for smart.unhealthy) and is NULL elsewhere.
	var val sql.NullFloat64
	var subject sql.NullString
	if err := db.QueryRow(query, args...).Scan(&val, &subject); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoData // per-device queries return no row on an empty window
		}
		return nil, fmt.Errorf("querying %s: %w", r.cfg.Metric, err)
	}
	if !val.Valid {
		// Every aggregate here is NULL over an empty window (smart.unhealthy's
		// COUNT is wrapped to match), so NULL means "no samples", not "healthy".
		return nil, ErrNoData
	}

	if r.compare(val.Float64) {
		valueText, thresholdText := breachText(val.Float64, r.cfg.Value)
		metric := r.cfg.Metric
		if on := r.subject(subject.String); on != "" {
			metric += " on " + on
		}
		return &Alert{
			RuleName: r.base.Name,
			Severity: r.base.Severity,
			// "agg" (avg/max/count) names what was actually computed, and "over"
			// (not "for") makes clear the comparison is across the window, not a
			// value sustained for its whole length — a 30s spike on an idle box
			// averages below threshold and won't fire (unless the rule uses max),
			// and the text no longer implies otherwise.
			Message: fmt.Sprintf("%s %s %s %s %s over %s", metric, agg, valueText, r.cfg.Operator, thresholdText, r.cfg.Duration),
		}, nil
	}
	return nil, nil
}

// subject names what the alert is about: the device the query reported, else
// the rule's own mount, interface or sensor.
func (r *ThresholdRule) subject(fromQuery string) string {
	for _, s := range []string{fromQuery, r.cfg.Mount, r.cfg.InterfaceName, r.cfg.Sensor} {
		if s != "" {
			return s
		}
	}
	return ""
}

// breachText formats a breaching value and its threshold with one decimal, or
// more when one decimal would print them equal — "85.0 > 85.0" for 85.04 reads
// as a false alarm.
func breachText(value, threshold float64) (string, string) {
	for prec := 1; prec < 4; prec++ {
		v, t := strconv.FormatFloat(value, 'f', prec, 64), strconv.FormatFloat(threshold, 'f', prec, 64)
		if v != t {
			return v, t
		}
	}
	return strconv.FormatFloat(value, 'f', 4, 64), strconv.FormatFloat(threshold, 'f', 4, 64)
}

// buildQuery returns the SQL, its bind args, and a label naming the aggregate it
// computes so the fired-alert message is truthful about the compared number. The
// SQL yields one row of (value, subject): subject is the SMART device the value
// came from (NULL for the other metrics). Value metrics honour the rule's
// avg/max/min; counter metrics compare their current level ("max") or, with
// aggregate "increase", their growth; percent_used/unhealthy are fixed MAX/COUNT.
func (r *ThresholdRule) buildQuery(cutoff time.Time) (string, []any, string, error) {
	if IsCounterMetric(r.cfg.Metric) {
		return r.counterQuery(cutoff)
	}
	fn, agg := aggFunc(r.cfg.Aggregate)
	value := func(query string, args ...any) (string, []any, string, error) {
		return "SELECT (" + query + "), NULL::VARCHAR", args, agg, nil
	}
	switch r.cfg.Metric {
	case "cpu.aggregate":
		// core = -1 is the whole-CPU aggregate (core = 0 was just physical core 0).
		// 100 - idle counts everything non-idle — including nice/irq/softirq and,
		// crucially, steal — so a contended VPS (high steal, low user+system) can
		// actually trip the alert instead of looking idle.
		return value(fmt.Sprintf("SELECT %s(100 - idle_pct) FROM cpu_metrics WHERE core = -1 AND ts > ?", fn), cutoff)
	case "cpu.steal":
		// Hypervisor-stolen time on the whole-CPU aggregate row — alert on VPS
		// noisy-neighbour contention directly (e.g. cpu.steal > 20), distinct from
		// overall utilization. steal_pct is nullable (rows predating migration 000012
		// are NULL); the aggregate ignores NULLs, so a pre-migration-only window
		// returns NULL and doesn't fire.
		return value(fmt.Sprintf("SELECT %s(steal_pct) FROM cpu_metrics WHERE core = -1 AND ts > ?", fn), cutoff)
	case "memory.used_pct":
		return value(fmt.Sprintf("SELECT %s(CAST(used_bytes AS DOUBLE) / NULLIF(total_bytes, 0) * 100) FROM memory_metrics WHERE ts > ?", fn), cutoff)
	case "disk.used_pct":
		return value(fmt.Sprintf(`SELECT %s(CAST(m.used_bytes AS DOUBLE) / NULLIF(m.total_bytes, 0) * 100)
			FROM disk_metrics m
			JOIN dimension_values d ON d.category = 'mount' AND d.id = m.mount_id
			WHERE d.value = ? AND m.ts > ?`, fn), r.cfg.Mount, cutoff)
	case "network.rx":
		return value(fmt.Sprintf(`SELECT %s(m.rx_bytes_sec)
			FROM network_metrics m
			JOIN dimension_values d ON d.category = 'interface' AND d.id = m.interface_id
			WHERE d.value = ? AND m.ts > ?`, fn), r.cfg.InterfaceName, cutoff)
	case "network.tx":
		return value(fmt.Sprintf(`SELECT %s(m.tx_bytes_sec)
			FROM network_metrics m
			JOIN dimension_values d ON d.category = 'interface' AND d.id = m.interface_id
			WHERE d.value = ? AND m.ts > ?`, fn), r.cfg.InterfaceName, cutoff)
	case "temperature.sensor":
		return value(fmt.Sprintf(`SELECT %s(m.temp_celsius)
			FROM temperature_metrics m
			JOIN dimension_values d ON d.category = 'sensor' AND d.id = m.sensor_id
			WHERE d.value = ? AND m.ts > ?`, fn), r.cfg.Sensor, cutoff)
	case "gpu.utilization":
		return value(fmt.Sprintf(`SELECT %s(m.utilization_pct)
			FROM gpu_metrics m
			JOIN dimension_values d ON d.category = 'gpu' AND d.id = m.gpu_id
			WHERE d.value = ? AND m.ts > ?`, fn), r.cfg.Sensor, cutoff)
	case "gpu.temperature":
		return value(fmt.Sprintf(`SELECT %s(m.temp_celsius)
			FROM gpu_metrics m
			JOIN dimension_values d ON d.category = 'gpu' AND d.id = m.gpu_id
			WHERE d.value = ? AND m.ts > ?`, fn), r.cfg.Sensor, cutoff)
	case "gpu.power":
		return value(fmt.Sprintf(`SELECT %s(m.power_watts)
			FROM gpu_metrics m
			JOIN dimension_values d ON d.category = 'gpu' AND d.id = m.gpu_id
			WHERE d.value = ? AND m.ts > ?`, fn), r.cfg.Sensor, cutoff)
	case "smart.percent_used":
		// NVMe wear: the most-worn device in the window (no per-device scope, so the
		// worst drive trips the alert and the message names it). No row → no data.
		return `SELECT CAST(percent_used AS DOUBLE), device FROM smart_metrics
			WHERE ts > ? AND percent_used IS NOT NULL
			ORDER BY percent_used DESC, ts DESC LIMIT 1`, []any{cutoff}, "max", nil
	case "smart.unhealthy":
		// Count of unhealthy snapshots in the window; > 0 means a drive reported a
		// SMART health failure (the "your disk is dying" signal). NULL when the
		// window has no SMART rows at all (they're written every smart_interval),
		// so an empty window reads as no data rather than as a healthy 0. The
		// subject lists the failing devices.
		return `SELECT CASE WHEN COUNT(*) = 0 THEN NULL ELSE COUNT(*) FILTER (WHERE healthy = false) END,
				array_to_string(list_sort(list_distinct(list(device) FILTER (WHERE healthy = false))), ', ')
			FROM smart_metrics WHERE ts > ?`, []any{cutoff}, "count", nil
	default:
		return "", nil, "", fmt.Errorf("unsupported threshold metric: %s", r.cfg.Metric)
	}
}

// counterQuery builds the query for a cumulative counter (IsCounterMetric). SMART
// rows are per device, so both modes pick the worst device and report it; ECC
// is one host-wide series (subject NULL).
//
// Level ("max", the default for anything but "increase"): the highest count in
// the window. Any drive that has ever reallocated a sector stays above 0.
//
// "increase": per device, the latest reading in the window minus the last
// reading before it (within increaseBaselineLookback). Measuring against the
// pre-window reading rather than MAX−MIN inside the window means a window
// shorter than smart_interval (one SMART row per 5m) still sees the step. With
// no baseline it falls back to the window's minimum. A count that went down
// (ECC counters reset on reboot; a replaced drive reusing the device name)
// counts from zero: the latest reading is all new.
func (r *ThresholdRule) counterQuery(cutoff time.Time) (string, []any, string, error) {
	tc := counterColumns[r.cfg.Metric]
	table, col := tc[0], tc[1]
	device := "device"
	if table != "smart_metrics" {
		device = "NULL::VARCHAR"
	}
	if r.cfg.Aggregate != "increase" {
		return fmt.Sprintf(`SELECT CAST(%[2]s AS DOUBLE), %[3]s FROM %[1]s
			WHERE ts > ? AND %[2]s IS NOT NULL
			ORDER BY %[2]s DESC, ts DESC LIMIT 1`, table, col, device), []any{cutoff}, "max", nil
	}
	return fmt.Sprintf(`WITH w AS (
			SELECT %[3]s AS dev, arg_max(%[2]s, ts) AS last, MIN(%[2]s) AS lo
			FROM %[1]s WHERE ts > ? AND %[2]s IS NOT NULL GROUP BY 1
		), b AS (
			SELECT %[3]s AS dev, arg_max(%[2]s, ts) AS base
			FROM %[1]s WHERE ts <= ? AND ts > ? AND %[2]s IS NOT NULL GROUP BY 1
		)
		SELECT CAST(CASE
				WHEN b.base IS NULL THEN w.last - w.lo
				WHEN w.lo < b.base THEN w.last
				ELSE w.last - b.base END AS DOUBLE) AS inc,
			w.dev
		FROM w LEFT JOIN b ON b.dev IS NOT DISTINCT FROM w.dev
		ORDER BY inc DESC, w.dev LIMIT 1`, table, col, device),
		[]any{cutoff, cutoff, cutoff.Add(-increaseBaselineLookback)}, "increase", nil
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
		return nil, ErrNoData
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
		return nil, ErrNoData
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
		// has_data guards startup: with no process snapshot at all the count is 0,
		// which would otherwise read as "down".
		query := fmt.Sprintf(`WITH latest AS (
				SELECT MAX(ts) AS ts FROM process_metrics
			)
			SELECT (SELECT ts IS NOT NULL FROM latest) AS has_data,
				COUNT(DISTINCT (pm.pid, pm.start_time))
			FROM process_info pi
			JOIN process_metrics pm ON pm.pid = pi.pid AND pm.start_time = pi.start_time
			CROSS JOIN latest l
			WHERE %s AND pm.ts = l.ts`, matchClause)
		var hasData bool
		var count int
		if err := db.QueryRow(query, matchArg).Scan(&hasData, &count); err != nil {
			return nil, fmt.Errorf("querying process count: %w", err)
		}
		if !hasData {
			return nil, ErrNoData
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
		return nil, ErrNoData
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

	// Sampling-rate limitation (inherent, not a bug): a restart is observed only if
	// the process exists during at least one collection tick, so a crash-loop whose
	// lifetime is shorter than the process-collector interval (e.g. respawns every
	// 200ms with a 5s interval) can be born-and-die between samples and never be
	// counted. Auto-pinning rule targets (Engine.ProcessPins) removes the top-N gate
	// but not this tick-rate gate; observing such loops would need event-based
	// detection (e.g. the netlink proc connector), out of scope here. A shorter
	// [collectors.process] interval narrows but cannot close the gap.

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
