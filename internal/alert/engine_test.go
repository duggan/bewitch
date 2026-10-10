package alert

import (
	"database/sql"
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/config"
)

// stubRule returns whatever its fields say, so engine tests can drive the
// lifecycle without seeding metrics.
type stubRule struct {
	name     string
	breached bool
	err      error
}

func (r *stubRule) ID() int      { return 1 }
func (r *stubRule) Name() string { return r.name }
func (r *stubRule) Evaluate(*sql.DB) (*Alert, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.breached {
		return &Alert{RuleName: r.name, Severity: "warning", Message: "breached"}, nil
	}
	return nil, nil
}

// TestResolveAfterDampsFlapping: an alert resolves only after its rule has been
// healthy continuously for resolve_after; a re-breach inside the clear period
// keeps the original alert active (no second fire) and restarts the period.
func TestResolveAfterDampsFlapping(t *testing.T) {
	database := newRuleTestDB(t)
	e := NewEngine(func() *sql.DB { return database }, &config.AlertsConfig{ResolveAfter: "2m"})
	rule := &stubRule{name: "flappy"}
	e.rules = []Rule{rule}
	now := time.Now()
	e.now = func() time.Time { return now }
	step := func(breached bool, advance time.Duration) {
		now = now.Add(advance)
		rule.breached = breached
		e.evaluate()
	}
	count := func(where string) int {
		var n int
		if err := database.QueryRow("SELECT count(*) FROM alerts WHERE rule_name = 'flappy' AND " + where).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	step(true, 0)
	if count("resolved_at IS NULL") != 1 {
		t.Fatal("expected the rule to fire immediately")
	}
	step(false, 10*time.Second)
	step(false, time.Minute)
	if count("resolved_at IS NULL") != 1 {
		t.Fatal("resolved before resolve_after elapsed")
	}
	step(true, 10*time.Second) // flaps back inside the clear period
	step(false, 10*time.Second)
	step(false, 90*time.Second) // 90s into a fresh clear period
	if got := count("true"); got != 1 {
		t.Fatalf("flapping produced %d alert rows, want 1", got)
	}
	if count("resolved_at IS NULL") != 1 {
		t.Fatal("re-breach did not restart the clear period")
	}
	step(false, 30*time.Second) // 2m healthy
	if count("resolved_at IS NOT NULL") != 1 {
		t.Fatal("expected the alert to resolve after 2m healthy")
	}
}

func TestResolveAfterConfig(t *testing.T) {
	for in, want := range map[string]time.Duration{"": defaultResolveAfter, "0s": 0, "5m": 5 * time.Minute, "bogus": defaultResolveAfter} {
		e := NewEngine(func() *sql.DB { return newRuleTestDB(t) }, &config.AlertsConfig{ResolveAfter: in})
		if e.resolveAfter != want {
			t.Errorf("resolve_after %q → %s, want %s", in, e.resolveAfter, want)
		}
	}
}

// TestRepeatInterval: with repeat_interval set, a still-firing alert is re-sent as
// a reminder each interval until it is acknowledged; the default sends once.
func TestRepeatInterval(t *testing.T) {
	database := newRuleTestDB(t)
	rec := &recordingNotifier{}
	e := NewEngine(func() *sql.DB { return database }, &config.AlertsConfig{RepeatInterval: "1h", ResolveAfter: "0s"})
	e.notifiers = []Notifier{rec}
	rule := &stubRule{name: "hot", breached: true}
	e.rules = []Rule{rule}
	now := time.Now()
	e.now = func() time.Time { return now }
	step := func(advance time.Duration) { now = now.Add(advance); e.evaluate() }

	step(0)                // fires
	step(30 * time.Minute) // still firing, too soon
	step(31 * time.Minute) // 61m since firing → reminder
	step(10 * time.Minute) // too soon after the reminder
	sent := rec.waitSent(t, 2)
	if len(sent) != 2 || sent[0].Reminder || !sent[1].Reminder {
		t.Fatalf("sent = %+v, want a fire then one reminder", sent)
	}

	if _, err := database.Exec("UPDATE alerts SET acknowledged = true WHERE rule_name = 'hot'"); err != nil {
		t.Fatal(err)
	}
	step(2 * time.Hour) // acknowledged: no more reminders
	if got := rec.waitSent(t, 3); len(got) != 2 {
		t.Errorf("acknowledged alert was reminded: %d sends, want 2", len(got))
	}

	// Default config: never reminds.
	if d := NewEngine(func() *sql.DB { return database }, &config.AlertsConfig{}); d.repeatInterval != 0 {
		t.Errorf("default repeatInterval = %s, want 0 (off)", d.repeatInterval)
	}
}
