package alert

import (
	"testing"
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
			&stubNotifier{name: "a", method: "email", result: NotifyResult{Method: "email", Dest: "admin@example.com"}},
			&stubNotifier{name: "b", method: "command", result: NotifyResult{Method: "command", Dest: "/usr/local/bin/alert"}},
		}
		results, err := SendTestNotifications(notifiers, &Alert{RuleName: "test", Severity: "info", Message: "hi"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
		if results[0].Method != "email" {
			t.Errorf("result[0].Method = %q, want email", results[0].Method)
		}
		if results[1].Method != "command" {
			t.Errorf("result[1].Method = %q, want command", results[1].Method)
		}
	})
}
