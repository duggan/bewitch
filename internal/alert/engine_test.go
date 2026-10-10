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
