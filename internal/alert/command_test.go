package alert

import (
	"strings"
	"testing"

	"github.com/duggan/bewitch/internal/config"
)

func TestCommandNotifierNameMethod(t *testing.T) {
	n := NewCommandNotifier(config.CommandDest{Cmd: "/usr/local/bin/alert.sh"})
	if n.Name() != "command:/usr/local/bin/alert.sh" {
		t.Errorf("Name() = %q", n.Name())
	}
	if n.Method() != "command" {
		t.Errorf("Method() = %q", n.Method())
	}
}

func TestCommandNotifierEmptyCommand(t *testing.T) {
	n := NewCommandNotifier(config.CommandDest{Cmd: ""})
	r := n.Send(&Alert{RuleName: "test", Severity: "info", Message: "hi"})
	if r.Error != "empty command" {
		t.Errorf("expected 'empty command' error, got %q", r.Error)
	}
}

func TestCommandNotifierSuccess(t *testing.T) {
	n := NewCommandNotifier(config.CommandDest{Cmd: "echo hello"})
	r := n.Send(&Alert{RuleName: "test", Severity: "warning", Message: "disk full"})
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	if !strings.Contains(r.Body, "hello") {
		t.Errorf("expected stdout to contain 'hello', got %q", r.Body)
	}
	if r.Method != "command" {
		t.Errorf("Method = %q, want command", r.Method)
	}
}

func TestCommandNotifierEnvVars(t *testing.T) {
	// Use printenv to verify env vars are set
	n := NewCommandNotifier(config.CommandDest{Cmd: "printenv BEWITCH_RULE"})
	r := n.Send(&Alert{RuleName: "my-rule", Severity: "critical", Message: "test"})
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	if !strings.Contains(r.Body, "my-rule") {
		t.Errorf("expected BEWITCH_RULE=my-rule in output, got %q", r.Body)
	}
}
