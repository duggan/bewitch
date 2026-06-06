package alert

import (
	"strings"
	"testing"
)

func TestRedactShoutrrrURL(t *testing.T) {
	// The redacted form must keep the scheme but leak nothing after "://" —
	// tokens hide in userinfo, host, or path depending on the service.
	cases := []struct {
		raw, want string
	}{
		{"discord://token@channel", "discord://***"},
		{"telegram://12345:AAbbCC@telegram?chats=1", "telegram://***"},
		{"slack://xoxb-secret/a/b", "slack://***"},
		{"ntfy://ntfy.sh/topic", "ntfy://***"},
		{"not-a-url", "not-a-url://***"},
	}
	for _, c := range cases {
		got := redactShoutrrrURL(c.raw)
		if got != c.want {
			t.Errorf("redactShoutrrrURL(%q) = %q, want %q", c.raw, got, c.want)
		}
		// Belt-and-suspenders: the secret must not survive.
		for _, secret := range []string{"token", "AAbbCC", "xoxb-secret"} {
			if strings.Contains(c.raw, secret) && strings.Contains(got, secret) {
				t.Errorf("redactShoutrrrURL(%q) leaked %q: %q", c.raw, secret, got)
			}
		}
	}
}

func TestShoutrrrTitle(t *testing.T) {
	firing := shoutrrrTitle(&Alert{RuleName: "disk_full", Severity: "warning", Message: "x"})
	if firing != "[bewitch] FIRING warning: disk_full" {
		t.Errorf("firing title = %q", firing)
	}
	resolved := shoutrrrTitle(&Alert{RuleName: "disk_full", Severity: "warning", Resolved: true})
	if resolved != "[bewitch] RESOLVED warning: disk_full" {
		t.Errorf("resolved title = %q", resolved)
	}
}

func TestNewShoutrrrNotifier(t *testing.T) {
	// A valid Shoutrrr URL constructs; the notifier satisfies the interface and
	// redacts its identity. (logger:// is Shoutrrr's no-network sink.)
	n, err := NewShoutrrrNotifier("logger://")
	if err != nil {
		t.Fatalf("NewShoutrrrNotifier(logger://) error: %v", err)
	}
	var _ Notifier = n
	if n.Method() != "shoutrrr" {
		t.Errorf("Method() = %q, want shoutrrr", n.Method())
	}
	if n.Name() != "shoutrrr:logger://***" {
		t.Errorf("Name() = %q", n.Name())
	}

	if _, err := NewShoutrrrNotifier("://garbage"); err == nil {
		t.Error("expected error for an invalid shoutrrr url")
	}
}
