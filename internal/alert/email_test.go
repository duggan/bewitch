package alert

import (
	"testing"

	"github.com/ross/bewitch/internal/config"
)

func TestEmailDestIsStartTLS(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name     string
		startTLS *bool
		want     bool
	}{
		{"nil defaults to true", nil, true},
		{"explicit true", &trueVal, true},
		{"explicit false", &falseVal, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &config.EmailDest{StartTLS: tt.startTLS}
			if got := e.IsStartTLS(); got != tt.want {
				t.Errorf("IsStartTLS() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEmailDestGetSMTPPort(t *testing.T) {
	tests := []struct {
		name string
		port int
		want int
	}{
		{"zero defaults to 587", 0, 587},
		{"explicit port", 465, 465},
		{"custom port", 2525, 2525},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &config.EmailDest{SMTPPort: tt.port}
			if got := e.GetSMTPPort(); got != tt.want {
				t.Errorf("GetSMTPPort() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEmailNotifierNameMethod(t *testing.T) {
	n := NewEmailNotifier(config.EmailDest{
		From: "alerts@example.com",
		To:   []string{"admin@example.com", "ops@example.com"},
	})
	if n.Name() != "email:admin@example.com,ops@example.com" {
		t.Errorf("Name() = %q", n.Name())
	}
	if n.Method() != "email" {
		t.Errorf("Method() = %q", n.Method())
	}
}
