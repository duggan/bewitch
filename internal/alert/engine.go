package alert

import (
	"database/sql"
	"sync"
	"time"

	"github.com/charmbracelet/log"

	"github.com/duggan/bewitch/internal/config"
)

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
		if alert == nil {
			continue
		}

		// Check if we recently fired this same alert (debounce: don't re-fire within the evaluation interval)
		var count int
		db.QueryRow(
			"SELECT COUNT(*) FROM alerts WHERE rule_name = ? AND ts > ? AND acknowledged = false",
			alert.RuleName, time.Now().Add(-e.interval*3),
		).Scan(&count)
		if count > 0 {
			continue
		}

		// Insert alert
		_, err = db.Exec(
			"INSERT INTO alerts (ts, rule_name, severity, message) VALUES (?, ?, ?, ?)",
			time.Now(), alert.RuleName, alert.Severity, alert.Message,
		)
		if err != nil {
			log.Errorf("inserting alert: %v", err)
			continue
		}

		log.Warnf("ALERT [%s] %s: %s", alert.Severity, alert.RuleName, alert.Message)

		// Fire notifications
		if len(e.notifiers) > 0 {
			sendNotifications(e.notifiers, alert)
		}
	}
}
