package alert

import (
	"testing"

	"github.com/ross/bewitch/internal/config"
)

// stubNotifier is a Notifier that returns a canned result.
type stubNotifier struct {
	name   string
	method string
	result NotifyResult
}

func (s *stubNotifier) Name() string          { return s.name }
func (s *stubNotifier) Method() string        { return s.method }
func (s *stubNotifier) Send(_ *Alert) NotifyResult { return s.result }

func TestSendTestNotifications(t *testing.T) {
	t.Run("empty notifiers returns error", func(t *testing.T) {
		_, err := SendTestNotifications(nil, &Alert{RuleName: "test", Severity: "info", Message: "hi"})
		if err == nil {
			t.Fatal("expected error for empty notifiers")
		}
	})

	t.Run("collects results from all notifiers", func(t *testing.T) {
		notifiers := []Notifier{
			&stubNotifier{name: "a", method: "webhook", result: NotifyResult{Method: "webhook", Dest: "http://a"}},
			&stubNotifier{name: "b", method: "ntfy", result: NotifyResult{Method: "ntfy", Dest: "http://b"}},
		}
		results, err := SendTestNotifications(notifiers, &Alert{RuleName: "test", Severity: "info", Message: "hi"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
		if results[0].Method != "webhook" {
			t.Errorf("result[0].Method = %q, want webhook", results[0].Method)
		}
		if results[1].Method != "ntfy" {
			t.Errorf("result[1].Method = %q, want ntfy", results[1].Method)
		}
	})
}

func TestWebhookNotifierNameMethod(t *testing.T) {
	n := NewWebhookNotifier(config.WebhookDest{URL: "https://example.com/hook"})
	if n.Name() != "webhook:https://example.com/hook" {
		t.Errorf("Name() = %q", n.Name())
	}
	if n.Method() != "webhook" {
		t.Errorf("Method() = %q", n.Method())
	}
}
