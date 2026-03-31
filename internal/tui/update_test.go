package tui

import (
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/api"
	"github.com/duggan/bewitch/internal/config"

	tea "github.com/charmbracelet/bubbletea"
)

// helper creates a Model with mock data and triggers WindowSizeMsg so m.ready=true.
func readyModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(newMockClient(), time.Second, config.DefaultHistoryRanges, DefaultCaptureSettings(), false)
	// Send WindowSizeMsg to initialize viewport
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return updated.(Model)
}

func TestUpdateWindowSize(t *testing.T) {
	m := NewModel(newMockClient(), time.Second, config.DefaultHistoryRanges, DefaultCaptureSettings(), false)
	if m.ready {
		t.Fatal("model should not be ready before WindowSizeMsg")
	}

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := updated.(Model)

	if !model.ready {
		t.Error("model should be ready after WindowSizeMsg")
	}
	if model.width != 120 {
		t.Errorf("width = %d, want 120", model.width)
	}
	if model.height != 40 {
		t.Errorf("height = %d, want 40", model.height)
	}
}

func TestUpdateViewKeys(t *testing.T) {
	m := readyModel(t)

	tests := []struct {
		key  rune
		want view
	}{
		{'1', viewDashboard},
		{'2', viewCPU},
		{'3', viewMemory},
		{'4', viewDisk},
		{'5', viewNetwork},
	}
	for _, tt := range tests {
		t.Run(string(tt.key), func(t *testing.T) {
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tt.key}})
			model := updated.(Model)
			if model.current != tt.want {
				t.Errorf("key %c: current = %d, want %d", tt.key, model.current, tt.want)
			}
		})
	}
}

func TestUpdateHistoryRange(t *testing.T) {
	m := readyModel(t)

	// Switch to CPU view first — Dashboard doesn't have history
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = updated.(Model)

	// Start from range 0 so we can test '>' advancing
	m.historyRange = 0

	// Press '>' to advance range
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'>'}})
	model := updated.(Model)
	if model.historyRange != 1 {
		t.Errorf("after '>': historyRange = %d, want 1", model.historyRange)
	}

	// Press '<' to go back
	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'<'}})
	model2 := updated2.(Model)
	if model2.historyRange != 0 {
		t.Errorf("after '<': historyRange = %d, want 0", model2.historyRange)
	}
}

func TestUpdateHistoryRangeClamps(t *testing.T) {
	m := readyModel(t)

	// Switch to CPU view
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = updated.(Model)

	// Set to last range — '>' should be a no-op (clamp, not wrap)
	lastIdx := len(m.historyRanges) - 1
	m.historyRange = lastIdx
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'>'}})
	model := updated.(Model)
	if model.historyRange != lastIdx {
		t.Errorf("expected clamp at %d, got %d", lastIdx, model.historyRange)
	}

	// Set to first range — '<' should be a no-op (clamp, not wrap)
	model.historyRange = 0
	updated2, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'<'}})
	model2 := updated2.(Model)
	if model2.historyRange != 0 {
		t.Errorf("expected clamp at 0, got %d", model2.historyRange)
	}
}

func TestUpdateAlertNavigation(t *testing.T) {
	m := readyModel(t)

	// Switch to alerts view — find its key position
	var alertKey rune
	for i, v := range m.visibleTabs {
		if v == viewAlerts {
			alertKey = rune('1' + i)
			break
		}
	}
	if alertKey == 0 {
		t.Fatal("alerts view not found in visible tabs")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{alertKey}})
	model := updated.(Model)
	if model.current != viewAlerts {
		t.Fatalf("expected alerts view, got %d", model.current)
	}

	// Cursor should start at 0
	if model.alertRuleCursor != 0 {
		t.Errorf("alertRuleCursor = %d, want 0", model.alertRuleCursor)
	}
}

func TestUpdateTick(t *testing.T) {
	m := readyModel(t)

	// Clear cached data to verify tick refreshes it
	m.cpuData = nil
	m.memData = nil

	updated, _ := m.Update(tickMsg(time.Now()))
	model := updated.(Model)

	// After tick on dashboard view, data should be refreshed
	if model.dashData == nil {
		t.Error("dashData should be non-nil after tick")
	}
}

func TestUpdateHardwareTabAlwaysVisible(t *testing.T) {
	// Hardware tab is always visible regardless of data
	mock := newMockClient()
	m := NewModel(mock, time.Second, config.DefaultHistoryRanges, DefaultCaptureSettings(), false)

	hasHW := false
	for _, v := range m.visibleTabs {
		if v == viewHardware {
			hasHW = true
		}
	}
	if !hasHW {
		t.Error("hardware tab should always be visible")
	}

	// Even without temp/power data
	mock2 := &mockClient{
		cpu:   []api.CPUCoreMetric{{Core: -1, UserPct: 25.0}},
		mem:   &api.MemoryMetric{TotalBytes: 16_000_000_000, UsedBytes: 8_000_000_000},
		procs: &api.ProcessResponse{},
		prefs: map[string]string{},
	}
	m2 := NewModel(mock2, time.Second, config.DefaultHistoryRanges, DefaultCaptureSettings(), false)

	hasHW = false
	for _, v := range m2.visibleTabs {
		if v == viewHardware {
			hasHW = true
		}
	}
	if !hasHW {
		t.Error("hardware tab should always be visible even without temp/power data")
	}
}
