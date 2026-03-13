package tui

import (
	"testing"
	"time"

	"github.com/ross/bewitch/internal/api"
)

func TestSortProcesses(t *testing.T) {
	procs := []api.ProcessMetric{
		{PID: 1, Name: "alpha", CPUUserPct: 10, CPUSystemPct: 5, RSSBytes: 1000, NumThreads: 2, NumFDs: 10},
		{PID: 2, Name: "beta", CPUUserPct: 30, CPUSystemPct: 10, RSSBytes: 5000, NumThreads: 8, NumFDs: 50},
		{PID: 3, Name: "gamma", CPUUserPct: 5, CPUSystemPct: 0, RSSBytes: 3000, NumThreads: 1, NumFDs: 5},
	}

	copyProcs := func() []api.ProcessMetric {
		c := make([]api.ProcessMetric, len(procs))
		copy(c, procs)
		return c
	}

	t.Run("sort by CPU descending", func(t *testing.T) {
		p := copyProcs()
		sortProcesses(p, procSortCPU)
		if p[0].Name != "beta" { // 40% total
			t.Errorf("first = %s, want beta", p[0].Name)
		}
		if p[2].Name != "gamma" { // 5% total
			t.Errorf("last = %s, want gamma", p[2].Name)
		}
	})

	t.Run("sort by memory descending", func(t *testing.T) {
		p := copyProcs()
		sortProcesses(p, procSortMem)
		if p[0].Name != "beta" { // 5000
			t.Errorf("first = %s, want beta", p[0].Name)
		}
	})

	t.Run("sort by PID ascending", func(t *testing.T) {
		p := copyProcs()
		sortProcesses(p, procSortPID)
		if p[0].PID != 1 {
			t.Errorf("first PID = %d, want 1", p[0].PID)
		}
		if p[2].PID != 3 {
			t.Errorf("last PID = %d, want 3", p[2].PID)
		}
	})

	t.Run("sort by name ascending", func(t *testing.T) {
		p := copyProcs()
		sortProcesses(p, procSortName)
		if p[0].Name != "alpha" {
			t.Errorf("first = %s, want alpha", p[0].Name)
		}
		if p[2].Name != "gamma" {
			t.Errorf("last = %s, want gamma", p[2].Name)
		}
	})

	t.Run("sort by threads descending", func(t *testing.T) {
		p := copyProcs()
		sortProcesses(p, procSortThreads)
		if p[0].Name != "beta" { // 8 threads
			t.Errorf("first = %s, want beta", p[0].Name)
		}
	})

	t.Run("sort by FDs descending", func(t *testing.T) {
		p := copyProcs()
		sortProcesses(p, procSortFDs)
		if p[0].Name != "beta" { // 50 FDs
			t.Errorf("first = %s, want beta", p[0].Name)
		}
	})
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		name string
		dur  time.Duration
		want string
	}{
		{"seconds", 45 * time.Second, "45s"},
		{"minutes", 5 * time.Minute, "5m"},
		{"hours", 3 * time.Hour, "3h"},
		{"days", 2 * 24 * time.Hour, "2d"},
		{"weeks", 14 * 24 * time.Hour, "2w"},
		{"months", 60 * 24 * time.Hour, "2mo"},
		{"years", 400 * 24 * time.Hour, "1y"},
		{"zero", 0, "0s"},
		{"just under a minute", 59 * time.Second, "59s"},
		{"just over a minute", 61 * time.Second, "1m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAge(tt.dur)
			if got != tt.want {
				t.Errorf("formatAge(%v) = %q, want %q", tt.dur, got, tt.want)
			}
		})
	}
}
