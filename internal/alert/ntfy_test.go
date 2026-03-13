package alert

import (
	"testing"

	"github.com/ross/bewitch/internal/config"
)

func TestNtfyPriority(t *testing.T) {
	tests := []struct {
		severity string
		want     string
	}{
		{"critical", "5"},
		{"warning", "3"},
		{"info", "3"},
		{"", "3"},
	}
	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			if got := ntfyPriority(tt.severity); got != tt.want {
				t.Errorf("ntfyPriority(%q) = %q, want %q", tt.severity, got, tt.want)
			}
		})
	}
}

func TestNtfyTags(t *testing.T) {
	tests := []struct {
		severity string
		want     string
	}{
		{"critical", "rotating_light"},
		{"warning", "warning"},
		{"info", "information_source"},
	}
	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			if got := ntfyTags(tt.severity); got != tt.want {
				t.Errorf("ntfyTags(%q) = %q, want %q", tt.severity, got, tt.want)
			}
		})
	}
}

func TestNtfyNotifierNameMethod(t *testing.T) {
	n := NewNtfyNotifier(config.NtfyDest{URL: "https://ntfy.sh", Topic: "alerts"})
	if n.Name() != "ntfy:alerts" {
		t.Errorf("Name() = %q", n.Name())
	}
	if n.Method() != "ntfy" {
		t.Errorf("Method() = %q", n.Method())
	}
}
