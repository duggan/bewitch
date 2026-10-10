package alert

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fastRetries shortens the retry waits for the duration of a test.
func fastRetries(t *testing.T) {
	t.Helper()
	old := notifyRetryDelays
	notifyRetryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { notifyRetryDelays = old })
}

// TestSendNotificationsCountsFailures verifies the live notification path bumps the
// notify-failure counter (surfaced via self-metrics) only for deliveries that error
// on every attempt.
func TestSendNotificationsCountsFailures(t *testing.T) {
	fastRetries(t)
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

// flakyNotifier fails its first failFirst sends, then succeeds.
type flakyNotifier struct {
	failFirst int32
	calls     atomic.Int32
}

func (f *flakyNotifier) Name() string   { return "flaky" }
func (f *flakyNotifier) Method() string { return "x" }
func (f *flakyNotifier) Send(*Alert) NotifyResult {
	if f.calls.Add(1) <= f.failFirst {
		return NotifyResult{Error: "blip"}
	}
	return NotifyResult{}
}

// TestSendNotificationsRetries: a delivery that fails transiently is retried and
// not counted as a failure; one that fails every attempt is tried 1+len(delays)
// times and counted once.
func TestSendNotificationsRetries(t *testing.T) {
	fastRetries(t)
	recovers := &flakyNotifier{failFirst: 2}
	never := &flakyNotifier{failFirst: 100}
	e := &Engine{notifiers: []Notifier{recovers, never}}
	e.sendNotifications(&Alert{RuleName: "r"})

	deadline := time.Now().Add(2 * time.Second)
	for (recovers.calls.Load() < 3 || e.NotifyFailures() < 1) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // let any stray extra attempt show up
	if got := recovers.calls.Load(); got != 3 {
		t.Errorf("recovering notifier sent %d times, want 3 (two failures, then success)", got)
	}
	if got := never.calls.Load(); got != 3 {
		t.Errorf("failing notifier sent %d times, want 3 (1 + 2 retries)", got)
	}
	if got := e.NotifyFailures(); got != 1 {
		t.Errorf("NotifyFailures = %d, want 1 (only the notifier that never recovered)", got)
	}
}

// recordingNotifier records every alert it is sent.
type recordingNotifier struct {
	mu   sync.Mutex
	sent []Alert
}

func (r *recordingNotifier) Name() string   { return "rec" }
func (r *recordingNotifier) Method() string { return "x" }
func (r *recordingNotifier) Send(a *Alert) NotifyResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, *a)
	return NotifyResult{}
}

// waitSent waits for n notifications (sends are async) and returns them.
func (r *recordingNotifier) waitSent(t *testing.T, n int) []Alert {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		got := append([]Alert(nil), r.sent...)
		r.mu.Unlock()
		if len(got) >= n || time.Now().After(deadline) {
			time.Sleep(20 * time.Millisecond) // catch an unexpected extra send
			r.mu.Lock()
			got = append([]Alert(nil), r.sent...)
			r.mu.Unlock()
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}
