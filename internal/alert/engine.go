package alert

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/charmbracelet/log"

	"github.com/duggan/bewitch/internal/config"
)

// collectionStalledRule is the reserved rule_name for the built-in dead-man's-
// switch alert that fires when metric collection has silently stopped.
const collectionStalledRule = "collection-stalled"

// Engine periodically evaluates alert rules.
type Engine struct {
	dbFn      func() *sql.DB
	rules     []Rule
	notifiers []Notifier
	interval  time.Duration
	stop      chan struct{}
	mu        sync.RWMutex
}

func NewEngine(dbFn func() *sql.DB, cfg *config.AlertsConfig) *Engine {
	interval := 10 * time.Second
	if cfg.EvaluationInterval != "" {
		if d, err := config.ParseDuration(cfg.EvaluationInterval); err == nil {
			interval = d
		}
	}

	var notifiers []Notifier
	for _, em := range cfg.Email {
		notifiers = append(notifiers, NewEmailNotifier(em))
	}
	for _, c := range cfg.Commands {
		notifiers = append(notifiers, NewCommandNotifier(c))
	}
	for _, u := range cfg.ShoutrrrURLs {
		n, err := NewShoutrrrNotifier(u)
		if err != nil {
			log.Warnf("skipping invalid shoutrrr url %q: %v", redactShoutrrrURL(u), err)
			continue
		}
		notifiers = append(notifiers, n)
	}

	e := &Engine{
		dbFn:      dbFn,
		notifiers: notifiers,
		interval:  interval,
		stop:      make(chan struct{}),
	}
	e.ReloadRules()
	return e
}

// ReloadRules reads alert_rules from the database and rebuilds the rule set.
// Each rule type is loaded via a JOIN so that orphaned base rows (missing
// their type-specific config) are silently skipped instead of logging errors.
func (e *Engine) ReloadRules() {
	db := e.dbFn()
	var rules []Rule

	// Threshold rules
	if rows, err := db.Query(`SELECT r.id, r.name, r.severity,
		t.metric, t.operator, t.value, t.duration,
		COALESCE(t.mount, ''), COALESCE(t.interface_name, ''), COALESCE(t.sensor, '')
		FROM alert_rules r
		JOIN alert_rule_threshold t ON t.rule_id = r.id
		WHERE r.enabled = true AND r.type = 'threshold'`); err != nil {
		log.Errorf("loading threshold rules: %v", err)
	} else {
		for rows.Next() {
			var base AlertRuleBase
			var cfg ThresholdConfig
			if err := rows.Scan(&base.ID, &base.Name, &base.Severity,
				&cfg.Metric, &cfg.Operator, &cfg.Value, &cfg.Duration,
				&cfg.Mount, &cfg.InterfaceName, &cfg.Sensor); err != nil {
				log.Errorf("scanning threshold rule: %v", err)
				continue
			}
			base.Type = "threshold"
			base.Enabled = true
			rules = append(rules, NewThresholdRule(base, cfg))
		}
		rows.Close()
	}

	// Predictive rules
	if rows, err := db.Query(`SELECT r.id, r.name, r.severity,
		t.metric, t.mount, t.predict_hours, t.threshold_pct
		FROM alert_rules r
		JOIN alert_rule_predictive t ON t.rule_id = r.id
		WHERE r.enabled = true AND r.type = 'predictive'`); err != nil {
		log.Errorf("loading predictive rules: %v", err)
	} else {
		for rows.Next() {
			var base AlertRuleBase
			var cfg PredictiveConfig
			if err := rows.Scan(&base.ID, &base.Name, &base.Severity,
				&cfg.Metric, &cfg.Mount, &cfg.PredictHours, &cfg.ThresholdPct); err != nil {
				log.Errorf("scanning predictive rule: %v", err)
				continue
			}
			base.Type = "predictive"
			base.Enabled = true
			rules = append(rules, NewPredictiveRule(base, cfg))
		}
		rows.Close()
	}

	// Variance rules
	if rows, err := db.Query(`SELECT r.id, r.name, r.severity,
		t.metric, t.delta_threshold, t.min_count, t.duration
		FROM alert_rules r
		JOIN alert_rule_variance t ON t.rule_id = r.id
		WHERE r.enabled = true AND r.type = 'variance'`); err != nil {
		log.Errorf("loading variance rules: %v", err)
	} else {
		for rows.Next() {
			var base AlertRuleBase
			var cfg VarianceConfig
			if err := rows.Scan(&base.ID, &base.Name, &base.Severity,
				&cfg.Metric, &cfg.DeltaThreshold, &cfg.MinCount, &cfg.Duration); err != nil {
				log.Errorf("scanning variance rule: %v", err)
				continue
			}
			base.Type = "variance"
			base.Enabled = true
			rules = append(rules, NewVarianceRule(base, cfg))
		}
		rows.Close()
	}

	// Process down rules
	if rows, err := db.Query(`SELECT r.id, r.name, r.severity,
		t.process_name, COALESCE(t.process_pattern, ''), t.min_instances, t.check_duration
		FROM alert_rules r
		JOIN alert_rule_process_down t ON t.rule_id = r.id
		WHERE r.enabled = true AND r.type = 'process_down'`); err != nil {
		log.Errorf("loading process_down rules: %v", err)
	} else {
		for rows.Next() {
			var base AlertRuleBase
			var cfg ProcessDownConfig
			if err := rows.Scan(&base.ID, &base.Name, &base.Severity,
				&cfg.ProcessName, &cfg.ProcessPattern, &cfg.MinInstances, &cfg.CheckDuration); err != nil {
				log.Errorf("scanning process_down rule: %v", err)
				continue
			}
			base.Type = "process_down"
			base.Enabled = true
			rules = append(rules, NewProcessDownRule(base, cfg))
		}
		rows.Close()
	}

	// Process thrashing rules
	if rows, err := db.Query(`SELECT r.id, r.name, r.severity,
		t.process_name, COALESCE(t.process_pattern, ''), t.restart_threshold, t.restart_window
		FROM alert_rules r
		JOIN alert_rule_process_thrashing t ON t.rule_id = r.id
		WHERE r.enabled = true AND r.type = 'process_thrashing'`); err != nil {
		log.Errorf("loading process_thrashing rules: %v", err)
	} else {
		for rows.Next() {
			var base AlertRuleBase
			var cfg ProcessThrashingConfig
			if err := rows.Scan(&base.ID, &base.Name, &base.Severity,
				&cfg.ProcessName, &cfg.ProcessPattern, &cfg.RestartThreshold, &cfg.RestartWindow); err != nil {
				log.Errorf("scanning process_thrashing rule: %v", err)
				continue
			}
			base.Type = "process_thrashing"
			base.Enabled = true
			rules = append(rules, NewProcessThrashingRule(base, cfg))
		}
		rows.Close()
	}

	// Warn about enabled base rules that loaded no config row. The per-type JOINs above
	// silently skip such orphans, which previously masked a bug where rule config was
	// written with rule_id=0 (see handleCreateAlertRule). Surfacing it makes the
	// misconfiguration visible instead of a rule that quietly never fires.
	loaded := make(map[int]bool, len(rules))
	for _, r := range rules {
		loaded[r.ID()] = true
	}
	if rows, err := db.Query(`SELECT id, name, type FROM alert_rules WHERE enabled = true`); err == nil {
		for rows.Next() {
			var id int
			var name, typ string
			if err := rows.Scan(&id, &name, &typ); err != nil {
				continue
			}
			if !loaded[id] {
				log.Warnf("alert rule %q (id=%d, type=%s) is enabled but has no config row; it will not be evaluated — recreate the rule", name, id, typ)
			}
		}
		rows.Close()
	}

	e.mu.Lock()
	e.rules = rules
	e.mu.Unlock()
}

// Notifiers returns the configured notification destinations.
func (e *Engine) Notifiers() []Notifier {
	return e.notifiers
}

// Start begins the alert evaluation loop in the background.
func (e *Engine) Start() {
	go e.run()
}

// Stop halts the evaluation loop.
func (e *Engine) Stop() {
	close(e.stop)
}

func (e *Engine) run() {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			e.ReloadRules()
			e.evaluate()
		case <-e.stop:
			return
		}
	}
}

// EvaluateOnce runs a single reload-and-evaluate cycle synchronously. It is the same work
// the background loop does per tick, exposed so the full create→load→evaluate→fire
// pipeline can be exercised in tests without spinning up the goroutine.
func (e *Engine) EvaluateOnce() {
	e.ReloadRules()
	e.evaluate()
}

func (e *Engine) evaluate() {
	db := e.dbFn()
	e.mu.RLock()
	rules := e.rules
	e.mu.RUnlock()

	for _, rule := range rules {
		alert, err := rule.Evaluate(db)
		if err != nil {
			log.Errorf("alert rule %s error: %v", rule.Name(), err)
			continue
		}
		e.applyAlertState(db, rule.Name(), alert)
	}

	// Built-in dead-man's-switch: fire when metric collection has silently stopped.
	e.applyAlertState(db, collectionStalledRule, e.collectionStalled(db))
}

// applyAlertState drives one rule's (or the dead-man's-switch's) firing
// lifecycle. An alert is "active" while its row has resolved_at IS NULL. It
// fires on the rising edge (breaching with no active alert) and resolves on the
// falling edge (no longer breaching with an active alert), emitting a recovery
// notification. Keying the debounce on the active row rather than acknowledgement
// has two effects vs the old logic: a persistent condition fires exactly once
// until it clears, and acking a still-breaching alert no longer spawns a
// duplicate next cycle. alert is nil when the rule is not currently breaching.
func (e *Engine) applyAlertState(db *sql.DB, ruleName string, alert *Alert) {
	var activeID int
	var activeSeverity string
	hasActive := db.QueryRow(
		"SELECT id, severity FROM alerts WHERE rule_name = ? AND resolved_at IS NULL ORDER BY ts DESC LIMIT 1",
		ruleName,
	).Scan(&activeID, &activeSeverity) == nil

	switch {
	case alert != nil && !hasActive:
		// Rising edge: fire.
		if _, err := db.Exec(
			"INSERT INTO alerts (ts, rule_name, severity, message) VALUES (?, ?, ?, ?)",
			time.Now(), alert.RuleName, alert.Severity, alert.Message,
		); err != nil {
			log.Errorf("inserting alert: %v", err)
			return
		}
		log.Warnf("ALERT [%s] %s: %s", alert.Severity, alert.RuleName, alert.Message)
		if len(e.notifiers) > 0 {
			sendNotifications(e.notifiers, alert)
		}
	case alert == nil && hasActive:
		// Falling edge: resolve and emit an all-clear.
		if _, err := db.Exec("UPDATE alerts SET resolved_at = ? WHERE id = ?", time.Now(), activeID); err != nil {
			log.Errorf("resolving alert %s: %v", ruleName, err)
			return
		}
		log.Infof("RESOLVED %s", ruleName)
		if len(e.notifiers) > 0 {
			sendNotifications(e.notifiers, &Alert{
				RuleName: ruleName,
				Severity: activeSeverity,
				Message:  "condition cleared",
				Resolved: true,
			})
		}
	}
	// breaching && active: still firing, suppress. !breaching && !active: nothing to do.
}

// collectionStalled returns a non-nil Alert when metric collection appears to
// have silently stopped — the newest cpu_metrics sample (CPU is always-on and
// high-frequency) is older than the dead-man threshold. It returns nil when
// collection is healthy or when there is no data yet, so it never false-fires at
// startup. (A fully dead daemon can't fire this since the engine dies with it;
// this catches in-process stalls — a collector wedged in backoff, a stuck writer
// goroutine, or dropped write batches under pressure.)
func (e *Engine) collectionStalled(db *sql.DB) *Alert {
	threshold := 12 * e.interval
	if threshold < 2*time.Minute {
		threshold = 2 * time.Minute
	}
	cutoff := time.Now().Add(-threshold)
	var count, stale int
	// stale=1 when there is data but the newest sample predates the cutoff. The
	// comparison runs in SQL (bound param vs stored ts) to avoid Go/DB timezone
	// skew, matching how the rest of the engine compares timestamps.
	if err := db.QueryRow(
		"SELECT count(*), COALESCE(max(ts) < ?, false)::INT FROM cpu_metrics", cutoff,
	).Scan(&count, &stale); err != nil {
		return nil
	}
	if count == 0 || stale == 0 {
		return nil
	}
	return &Alert{
		RuleName: collectionStalledRule,
		Severity: "critical",
		Message:  fmt.Sprintf("metric collection has stalled: no new cpu samples in over %s", threshold.Round(time.Second)),
	}
}
