package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/duggan/bewitch/internal/alert"
)

// TestMaintenanceGate covers the rate-limit/serialization guard on the heavy
// maintenance endpoints: a second op while one is running is refused (409), and
// one started too soon after the last finished is refused (429).
func TestMaintenanceGate(t *testing.T) {
	s := &Server{}

	if code, _ := s.beginMaintenance(); code != 0 {
		t.Fatalf("first beginMaintenance = %d, want 0 (allowed)", code)
	}
	// Still in flight: must be refused with 409.
	if code, _ := s.beginMaintenance(); code != http.StatusConflict {
		t.Fatalf("concurrent beginMaintenance = %d, want 409", code)
	}
	s.endMaintenance()
	// Just finished: within minMaintenanceInterval, refused with 429.
	if code, _ := s.beginMaintenance(); code != http.StatusTooManyRequests {
		t.Fatalf("rapid re-run beginMaintenance = %d, want 429", code)
	}
}

// recordingNotifier captures the alert it was asked to deliver.
type recordingNotifier struct{ got *alert.Alert }

func (n *recordingNotifier) Name() string   { return "recording" }
func (n *recordingNotifier) Method() string { return "recording" }
func (n *recordingNotifier) Send(a *alert.Alert) alert.NotifyResult {
	cp := *a
	n.got = &cp
	return alert.NotifyResult{Method: "recording"}
}

// TestTestNotificationsIgnoresPayload proves the test endpoint never forwards
// caller-supplied alert fields to the notifiers (which would let an
// unauthenticated socket client feed the command notifier / spray channels).
func TestTestNotificationsIgnoresPayload(t *testing.T) {
	rec := &recordingNotifier{}
	s := &Server{notifiers: []alert.Notifier{rec}}

	body := `{"rule_name":"x\nBcc: evil@example.com","severity":"critical","message":"PWNED ${BEWITCH_RULE}"}`
	req := httptest.NewRequest("POST", "/api/test-notifications", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	s.handleTestNotifications(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if rec.got == nil {
		t.Fatal("notifier was not invoked")
	}
	if rec.got.RuleName != "test" || rec.got.Severity != "info" || rec.got.Message != "Test notification from bewitch" {
		t.Fatalf("notifier received caller-controlled alert %+v; want the fixed canned alert", *rec.got)
	}
}
