package alert

import (
	"testing"
	"time"
)

// TestSendNotificationsCountsFailures verifies the live notification path bumps the
// notify-failure counter (surfaced via self-metrics) only for deliveries that error.
func TestSendNotificationsCountsFailures(t *testing.T) {
	e := &Engine{notifiers: []Notifier{
		&stubNotifier{name: "fail", method: "x", result: NotifyResult{Error: "boom"}},
		&stubNotifier{name: "ok", method: "x", result: NotifyResult{}},
	}}
	e.sendNotifications(&Alert{RuleName: "r"})

	// Sends are async; wait for the failing one to register.
	deadline := time.Now().Add(2 * time.Second)
	for e.NotifyFailures() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := e.NotifyFailures(); got != 1 {
		t.Errorf("NotifyFailures = %d, want 1 (one failing notifier, one ok)", got)
	}
}
