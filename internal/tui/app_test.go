package tui

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/api"
	"github.com/duggan/bewitch/internal/config"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/muesli/termenv"
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}

func newTestModel(t *testing.T) Model {
	t.Helper()
	return NewModel(newMockClient(), time.Second, config.DefaultHistoryRanges, DefaultCaptureSettings(), false)
}

func TestInitialRender(t *testing.T) {
	tm := teatest.NewTestModel(t, newTestModel(t),
		teatest.WithInitialTermSize(120, 40))

	// The first render should contain the "bewitch" branding and tab bar
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("bewitch"))
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestQuit(t *testing.T) {
	tm := teatest.NewTestModel(t, newTestModel(t),
		teatest.WithInitialTermSize(120, 40))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestViewSwitchingViaFinalModel(t *testing.T) {
	tests := []struct {
		name string
		key  rune
		want view
	}{
		{"CPU", '2', viewCPU},
		{"Memory", '3', viewMemory},
		{"Disk", '4', viewDisk},
		{"Network", '5', viewNetwork},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := teatest.NewTestModel(t, newTestModel(t),
				teatest.WithInitialTermSize(120, 40))

			// Allow time for initial render
			time.Sleep(50 * time.Millisecond)

			// Switch view
			tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tt.key}})

			// Small delay for the view switch to process
			time.Sleep(50 * time.Millisecond)

			// Quit and check final model state
			tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
			fm := tm.FinalModel(t, teatest.WithFinalTimeout(2*time.Second))

			model := fm.(Model)
			if model.current != tt.want {
				t.Errorf("current = %d (%s), want %d (%s)",
					model.current, viewName(model.current),
					tt.want, viewName(tt.want))
			}
		})
	}
}

func TestArrowKeyCyclingViaFinalModel(t *testing.T) {
	// Right arrow from Dashboard → should land on CPU (next tab)
	t.Run("right arrow forward", func(t *testing.T) {
		tm := teatest.NewTestModel(t, newTestModel(t),
			teatest.WithInitialTermSize(120, 40))

		time.Sleep(50 * time.Millisecond)
		tm.Send(tea.KeyMsg{Type: tea.KeyRight})
		time.Sleep(50 * time.Millisecond)

		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
		fm := tm.FinalModel(t, teatest.WithFinalTimeout(2*time.Second))

		model := fm.(Model)
		if model.current != viewCPU {
			t.Errorf("after Right: current = %d (%s), want viewCPU",
				model.current, viewName(model.current))
		}
	})

	// Right then Left → should be back on Dashboard
	t.Run("left arrow back", func(t *testing.T) {
		tm := teatest.NewTestModel(t, newTestModel(t),
			teatest.WithInitialTermSize(120, 40))

		time.Sleep(50 * time.Millisecond)
		tm.Send(tea.KeyMsg{Type: tea.KeyRight})
		time.Sleep(50 * time.Millisecond)
		tm.Send(tea.KeyMsg{Type: tea.KeyLeft})
		time.Sleep(50 * time.Millisecond)

		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
		fm := tm.FinalModel(t, teatest.WithFinalTimeout(2*time.Second))

		model := fm.(Model)
		if model.current != viewDashboard {
			t.Errorf("after Right+Left: current = %d (%s), want viewDashboard",
				model.current, viewName(model.current))
		}
	})
}

func TestDynamicTabHiding(t *testing.T) {
	// Mock with no temperature or power data — those tabs should be hidden
	mock := &mockClient{
		cpu: []api.CPUCoreMetric{
			{Core: -1, UserPct: 25.0, SystemPct: 10.0, IdlePct: 60.0},
		},
		mem: &api.MemoryMetric{
			TotalBytes: 16_000_000_000, UsedBytes: 8_000_000_000,
			AvailableBytes: 7_000_000_000,
		},
		procs: &api.ProcessResponse{},
		prefs: map[string]string{},
	}
	m := NewModel(mock, time.Second, config.DefaultHistoryRanges, DefaultCaptureSettings(), false)

	tm := teatest.NewTestModel(t, m,
		teatest.WithInitialTermSize(120, 40))

	// Check initial output doesn't have Temp or Power
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		hasBranding := bytes.Contains(bts, []byte("bewitch"))
		noTemp := !bytes.Contains(bts, []byte("Temp"))
		noPower := !bytes.Contains(bts, []byte("Pwr"))
		return hasBranding && noTemp && noPower
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestHardwareTabAlwaysVisible(t *testing.T) {
	// Hardware tab is always present regardless of data availability
	mockFull := newMockClient()
	mFull := NewModel(mockFull, time.Second, config.DefaultHistoryRanges, DefaultCaptureSettings(), false)

	hasHW := false
	for _, v := range mFull.visibleTabs {
		if v == viewHardware {
			hasHW = true
		}
	}
	if !hasHW {
		t.Error("hardware tab should always be visible")
	}

	// Even without temp/power data, hardware tab is present
	mockEmpty := &mockClient{
		cpu:   []api.CPUCoreMetric{{Core: -1, UserPct: 10}},
		mem:   &api.MemoryMetric{TotalBytes: 16_000_000_000, UsedBytes: 8_000_000_000},
		procs: &api.ProcessResponse{},
		prefs: map[string]string{},
	}
	mEmpty := NewModel(mockEmpty, time.Second, config.DefaultHistoryRanges, DefaultCaptureSettings(), false)

	hasHW = false
	for _, v := range mEmpty.visibleTabs {
		if v == viewHardware {
			hasHW = true
		}
	}
	if !hasHW {
		t.Error("hardware tab should always be visible even without temp/power data")
	}
}

func TestProcessViewInteraction(t *testing.T) {
	tm := teatest.NewTestModel(t, newTestModel(t),
		teatest.WithInitialTermSize(120, 40))

	time.Sleep(50 * time.Millisecond)

	// Find the process view key position
	m := newTestModel(t)
	var procKey rune
	for i, v := range m.visibleTabs {
		if v == viewProcess {
			procKey = rune('1' + i)
			break
		}
	}

	// Switch to process view
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{procKey}})
	time.Sleep(50 * time.Millisecond)

	// Quit and verify we're on the process view
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	fm := tm.FinalModel(t, teatest.WithFinalTimeout(2*time.Second))

	model := fm.(Model)
	if model.current != viewProcess {
		t.Errorf("current = %d (%s), want viewProcess",
			model.current, viewName(model.current))
	}
	if model.procData == nil {
		t.Error("procData should be populated")
	}
}

func TestBucketDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want time.Duration
	}{
		{30 * time.Minute, time.Minute},
		{time.Hour, time.Minute},
		{6 * time.Hour, 10 * time.Minute},
		{24 * time.Hour, 10 * time.Minute},
		{3 * 24 * time.Hour, time.Hour},
		{7 * 24 * time.Hour, time.Hour},
		{30 * 24 * time.Hour, 6 * time.Hour},
	}
	for _, tt := range tests {
		got := bucketDuration(tt.d)
		if got != tt.want {
			t.Errorf("bucketDuration(%v) = %v, want %v", tt.d, got, tt.want)
		}
	}
}

func TestMergeSeriesIncremental(t *testing.T) {
	ts := func(sec int64) int64 { return sec * 1e9 } // seconds to nanoseconds
	pt := func(sec int64, val float64) api.TimeSeriesPoint {
		return api.TimeSeriesPoint{TimestampNS: ts(sec), Value: val}
	}

	t.Run("append new points", func(t *testing.T) {
		cached := []api.TimeSeries{{
			Label:  "cpu",
			Points: []api.TimeSeriesPoint{pt(100, 10), pt(200, 20), pt(300, 30)},
		}}
		inc := []api.TimeSeries{{
			Label:  "cpu",
			Points: []api.TimeSeriesPoint{pt(300, 30), pt(400, 40)},
		}}
		merged, changed := mergeSeriesIncremental(cached, inc, time.Unix(100, 0))
		if !changed {
			t.Fatal("expected changed=true")
		}
		if len(merged[0].Points) != 4 {
			t.Fatalf("got %d points, want 4", len(merged[0].Points))
		}
		if merged[0].Points[3].Value != 40 {
			t.Errorf("last point value = %v, want 40", merged[0].Points[3].Value)
		}
	})

	t.Run("update existing bucket value", func(t *testing.T) {
		cached := []api.TimeSeries{{
			Label:  "cpu",
			Points: []api.TimeSeriesPoint{pt(100, 10), pt(200, 20), pt(300, 30)},
		}}
		inc := []api.TimeSeries{{
			Label:  "cpu",
			Points: []api.TimeSeriesPoint{pt(300, 35)},
		}}
		merged, changed := mergeSeriesIncremental(cached, inc, time.Unix(100, 0))
		if !changed {
			t.Fatal("expected changed=true")
		}
		if merged[0].Points[2].Value != 35 {
			t.Errorf("updated point value = %v, want 35", merged[0].Points[2].Value)
		}
	})

	t.Run("no change when values identical", func(t *testing.T) {
		cached := []api.TimeSeries{{
			Label:  "cpu",
			Points: []api.TimeSeriesPoint{pt(100, 10), pt(200, 20), pt(300, 30)},
		}}
		inc := []api.TimeSeries{{
			Label:  "cpu",
			Points: []api.TimeSeriesPoint{pt(300, 30)},
		}}
		_, changed := mergeSeriesIncremental(cached, inc, time.Unix(100, 0))
		if changed {
			t.Fatal("expected changed=false when values are identical")
		}
	})

	t.Run("trim left edge", func(t *testing.T) {
		cached := []api.TimeSeries{{
			Label:  "cpu",
			Points: []api.TimeSeriesPoint{pt(100, 10), pt(200, 20), pt(300, 30)},
		}}
		inc := []api.TimeSeries{{
			Label:  "cpu",
			Points: []api.TimeSeriesPoint{pt(300, 30)},
		}}
		// Window start at 200 should trim the point at 100.
		merged, changed := mergeSeriesIncremental(cached, inc, time.Unix(200, 0))
		if !changed {
			t.Fatal("expected changed=true due to trim")
		}
		if len(merged[0].Points) != 2 {
			t.Fatalf("got %d points, want 2", len(merged[0].Points))
		}
		if merged[0].Points[0].TimestampNS != ts(200) {
			t.Errorf("first point ts = %d, want %d", merged[0].Points[0].TimestampNS, ts(200))
		}
	})

	t.Run("new series label", func(t *testing.T) {
		cached := []api.TimeSeries{{
			Label:  "cpu",
			Points: []api.TimeSeriesPoint{pt(100, 10)},
		}}
		inc := []api.TimeSeries{{
			Label:  "new_sensor",
			Points: []api.TimeSeriesPoint{pt(200, 50)},
		}}
		merged, changed := mergeSeriesIncremental(cached, inc, time.Unix(100, 0))
		if !changed {
			t.Fatal("expected changed=true for new series")
		}
		if len(merged) != 2 {
			t.Fatalf("got %d series, want 2", len(merged))
		}
		if merged[1].Label != "new_sensor" {
			t.Errorf("new series label = %q, want %q", merged[1].Label, "new_sensor")
		}
	})

	t.Run("multiple series", func(t *testing.T) {
		cached := []api.TimeSeries{
			{Label: "user", Points: []api.TimeSeriesPoint{pt(100, 10), pt(200, 20)}},
			{Label: "system", Points: []api.TimeSeriesPoint{pt(100, 5), pt(200, 8)}},
		}
		inc := []api.TimeSeries{
			{Label: "user", Points: []api.TimeSeriesPoint{pt(200, 22), pt(300, 30)}},
			{Label: "system", Points: []api.TimeSeriesPoint{pt(200, 8), pt(300, 12)}},
		}
		merged, changed := mergeSeriesIncremental(cached, inc, time.Unix(100, 0))
		if !changed {
			t.Fatal("expected changed=true")
		}
		// user: 100→10, 200→22 (updated), 300→30 (new)
		if len(merged[0].Points) != 3 {
			t.Fatalf("user got %d points, want 3", len(merged[0].Points))
		}
		if merged[0].Points[1].Value != 22 {
			t.Errorf("user[1] = %v, want 22", merged[0].Points[1].Value)
		}
		// system: 100→5, 200→8 (unchanged), 300→12 (new)
		if len(merged[1].Points) != 3 {
			t.Fatalf("system got %d points, want 3", len(merged[1].Points))
		}
	})
}

func TestHistoryRangeViaFinalModel(t *testing.T) {
	tm := teatest.NewTestModel(t, newTestModel(t),
		teatest.WithInitialTermSize(120, 40))

	time.Sleep(50 * time.Millisecond)

	// Switch to CPU view (which has history charts)
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	time.Sleep(50 * time.Millisecond)

	// Get initial range
	m := newTestModel(t)
	initialRange := m.historyRange

	// Cycle forward
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'>'}})
	time.Sleep(50 * time.Millisecond)

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	fm := tm.FinalModel(t, teatest.WithFinalTimeout(2*time.Second))

	model := fm.(Model)
	expected := initialRange + 1
	if expected >= len(config.DefaultHistoryRanges) {
		expected = 0
	}
	if model.historyRange != expected {
		t.Errorf("historyRange = %d, want %d", model.historyRange, expected)
	}
}
