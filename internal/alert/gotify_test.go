package alert

import (
	"testing"

	"github.com/ross/bewitch/internal/config"
)

func TestGotifyPriority(t *testing.T) {
	tests := []struct {
		name           string
		severity       string
		configPriority int
		want           int
	}{
		{"critical default", "critical", 0, 8},
		{"warning default", "warning", 0, 5},
		{"info default", "info", 0, 4},
		{"config override critical", "critical", 10, 10},
		{"config override warning", "warning", 3, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gotifyPriority(tt.severity, tt.configPriority); got != tt.want {
				t.Errorf("gotifyPriority(%q, %d) = %d, want %d", tt.severity, tt.configPriority, got, tt.want)
			}
		})
	}
}

func TestGotifyNotifierNameMethod(t *testing.T) {
	n := NewGotifyNotifier(config.GotifyDest{URL: "https://gotify.example.com", Token: "tok"})
	if n.Name() != "gotify:https://gotify.example.com" {
		t.Errorf("Name() = %q", n.Name())
	}
	if n.Method() != "gotify" {
		t.Errorf("Method() = %q", n.Method())
	}
}
