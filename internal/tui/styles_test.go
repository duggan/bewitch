package tui

import (
	"strings"
	"testing"
	"time"
)

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		input uint64
		want  string
	}{
		{0, "0B"},
		{512, "512B"},
		{1023, "1023B"},
		{1024, "1.0K"},
		{1536, "1.5K"},
		{1048576, "1.0M"},
		{1073741824, "1.0G"},
		{1099511627776, "1.0T"},
		{1536 * 1024, "1.5M"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := humanBytes(tt.input)
			if got != tt.want {
				t.Errorf("humanBytes(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestHumanBits(t *testing.T) {
	tests := []struct {
		name  string
		input uint64 // bytes
		want  string
	}{
		{"zero", 0, "0bps"},
		{"sub-kilo", 100, "800bps"},
		{"1 Kbps", 125, "1.0Kbps"},      // 125 * 8 = 1000
		{"1 Mbps", 125000, "1.0Mbps"},    // 125000 * 8 = 1000000
		{"1 Gbps", 125000000, "1.0Gbps"}, // 125000000 * 8 = 1e9
		{"10 Mbps", 1250000, "10.0Mbps"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := humanBits(tt.input)
			if got != tt.want {
				t.Errorf("humanBits(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRenderBar(t *testing.T) {
	t.Run("0 percent all empty", func(t *testing.T) {
		bar := renderBar(0, 20)
		// Should have 20 empty chars (▒)
		if !strings.Contains(bar, "▒") {
			t.Error("expected empty fill chars")
		}
		if strings.Contains(bar, "█") {
			t.Error("unexpected filled chars at 0%")
		}
	})

	t.Run("100 percent all filled", func(t *testing.T) {
		bar := renderBar(100, 20)
		if strings.Contains(bar, "▒") {
			t.Error("unexpected empty chars at 100%")
		}
		if !strings.Contains(bar, "█") {
			t.Error("expected filled chars")
		}
	})

	t.Run("50 percent mixed", func(t *testing.T) {
		bar := renderBar(50, 20)
		if !strings.Contains(bar, "█") {
			t.Error("expected filled chars")
		}
		if !strings.Contains(bar, "▒") {
			t.Error("expected empty chars")
		}
	})

	t.Run("small width defaults to 20", func(t *testing.T) {
		// width < 2 should use 20
		bar := renderBar(50, 1)
		if !strings.Contains(bar, "█") {
			t.Error("expected filled chars with default width")
		}
	})

	t.Run("over 100 percent clamped", func(t *testing.T) {
		bar := renderBar(150, 20)
		// Should not panic, all filled
		if !strings.Contains(bar, "█") {
			t.Error("expected filled chars")
		}
	})

	t.Run("negative percent clamped to 0", func(t *testing.T) {
		bar := renderBar(-10, 20)
		if strings.Contains(bar, "█") {
			t.Error("unexpected filled chars for negative pct")
		}
	})
}

func TestBuildStatusBar(t *testing.T) {
	intervals := map[string]string{
		"cpu": "1s", "memory": "5s", "disk": "5s", "network": "2s",
		"ecc": "5s", "temperature": "5s", "power": "5s", "process": "3s",
	}
	status := map[string]any{
		"collector_intervals": intervals,
	}
	recent := time.Now() // fresh data, no staleness

	t.Run("single collector view", func(t *testing.T) {
		got := buildStatusBar(status, viewCPU, recent)
		if got != "Collection interval: 1s" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("multi collector view", func(t *testing.T) {
		got := buildStatusBar(status, viewDashboard, recent)
		if !strings.HasPrefix(got, "Collection intervals:") {
			t.Errorf("expected 'Collection intervals:' prefix, got %q", got)
		}
		if !strings.Contains(got, "CPU 1s") {
			t.Errorf("expected 'CPU 1s' in %q", got)
		}
	})

	t.Run("missing status data", func(t *testing.T) {
		got := buildStatusBar(map[string]any{}, viewCPU, recent)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("unknown view", func(t *testing.T) {
		got := buildStatusBar(status, view(99), recent)
		if got != "" {
			t.Errorf("expected empty for unknown view, got %q", got)
		}
	})

	t.Run("stale data", func(t *testing.T) {
		stale := time.Now().Add(-10 * time.Second) // CPU interval is 1s, 3× = 3s, 10s > 3s
		got := buildStatusBar(status, viewCPU, stale)
		if !strings.Contains(got, "stale") {
			t.Errorf("expected staleness indicator, got %q", got)
		}
	})

	t.Run("fresh data no staleness", func(t *testing.T) {
		got := buildStatusBar(status, viewCPU, recent)
		if strings.Contains(got, "stale") {
			t.Errorf("unexpected staleness indicator in %q", got)
		}
	})

	t.Run("zero time no staleness", func(t *testing.T) {
		got := buildStatusBar(status, viewCPU, time.Time{})
		if strings.Contains(got, "stale") {
			t.Errorf("zero time should not show staleness, got %q", got)
		}
	})
}

func TestXLabelFormatter(t *testing.T) {
	ts := time.Date(2024, 3, 15, 14, 30, 0, 0, time.UTC)

	t.Run("short duration shows HH:MM", func(t *testing.T) {
		f := xLabelFormatter(12 * time.Hour)
		got := f(0, float64(ts.Unix()))
		if got != "14:30" {
			t.Errorf("got %q, want 14:30", got)
		}
	})

	t.Run("medium duration shows day and time", func(t *testing.T) {
		f := xLabelFormatter(3 * 24 * time.Hour)
		got := f(0, float64(ts.Unix()))
		if got != "Fri 14:30" {
			t.Errorf("got %q, want Fri 14:30", got)
		}
	})

	t.Run("long duration shows date", func(t *testing.T) {
		f := xLabelFormatter(30 * 24 * time.Hour)
		got := f(0, float64(ts.Unix()))
		if got != "03/15" {
			t.Errorf("got %q, want 03/15", got)
		}
	})
}
