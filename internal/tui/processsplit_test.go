package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/duggan/bewitch/internal/api"
	"github.com/duggan/bewitch/internal/config"
)

// processSplitModel builds a process view with n processes (the first half enriched),
// a window size, and a placeholder history chart, ready to render.
func processSplitModel(t *testing.T, n, w, h int) Model {
	t.Helper()
	procs := make([]api.ProcessMetric, n)
	now := time.Now().UnixNano()
	for i := range procs {
		procs[i] = api.ProcessMetric{
			PID: int32(i + 100), Name: "proc" + string(rune('a'+i%26)), State: "S",
			CPUUserPct: float64(n - i), RSSBytes: uint64((i + 1) * 1_000_000),
			NumThreads: int32(i%8 + 1), NumFDs: int32(i % 50), StartTimeNs: now - int64(time.Hour),
			Enriched: i < n/2,
		}
	}
	mc := newMockClient()
	mc.procs = &api.ProcessResponse{Processes: procs, TotalProcs: int32(n), ActiveProcs: 6, TotalCPUPct: 18.4, TotalRSSBytes: 1_100_000_000}
	m := NewModel(mc, time.Second, config.DefaultHistoryRanges, DefaultCaptureSettings(), false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = updated.(Model)
	m.current = viewProcess
	m.refreshProcessData()
	// A realistically-tall chart panel (chartHeightForTerminal lines wrapped in a panel),
	// so the height budget is actually exercised, not a one-line stub.
	ch := chartHeightForTerminal(h)
	m.cachedHistoryCharts[viewProcess] = renderPanel(procChartTitle, strings.Repeat("│ chart\n", ch)+"< >:range", w)
	m.viewport.SetContent(m.renderCurrentContent())
	return m
}

const procChartTitle = "Process CPU History [15m]"

// TestProcessChartAlwaysVisible is the headline regression guard for the Split Deck
// redesign: on a normal terminal the bounded table must leave the history chart on screen
// (rather than pushing it below the fold as the old inline list did), and the content must
// never overflow the viewport regardless of how many processes there are.
func TestProcessChartAlwaysVisible(t *testing.T) {
	for _, n := range []int{5, 250} {
		m := processSplitModel(t, n, 140, 44)
		out := m.View()
		if !strings.Contains(out, procChartTitle) {
			t.Errorf("n=%d: history chart not visible — table not bounded?", n)
		}
		if strings.Contains(out, "PgUp/Dn to scroll") {
			t.Errorf("n=%d: content overflows the viewport (chart pushed off / outer scroll)", n)
		}
		if !strings.Contains(out, "▸ ") {
			t.Errorf("n=%d: selected-row detail strip missing", n)
		}
		if !strings.Contains(out, "NET R/W") {
			t.Errorf("n=%d: expected NET R/W column at width 140", n)
		}
	}
}

// TestProcessTinyTerminalDropsChart verifies the documented fallback: when the terminal
// is too short for both a usable table and the chart, the chart is dropped (so the table
// stays usable) and nothing overflows.
func TestProcessTinyTerminalDropsChart(t *testing.T) {
	m := processSplitModel(t, 250, 120, 24)
	out := m.View()
	if strings.Contains(out, "PgUp/Dn to scroll") {
		t.Error("tiny terminal still overflows")
	}
	if strings.Contains(out, procChartTitle) {
		t.Error("chart should be dropped on a terminal too short for both")
	}
}

// TestProcessTableNavigation verifies arrow keys drive the table cursor (the selection
// source of truth) and that the selected process follows it.
func TestProcessTableNavigation(t *testing.T) {
	m := processSplitModel(t, 30, 140, 40)
	if m.procCursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.procCursor)
	}
	for i := 0; i < 3; i++ {
		updated, _ := m.Update(key("down"))
		m = updated.(Model)
	}
	if m.procCursor != 3 {
		t.Errorf("after 3×down, cursor = %d, want 3", m.procCursor)
	}
	// The detail strip should follow the cursor to the 4th process.
	if sel, ok := m.selectedProcess(); !ok || sel.PID != 103 {
		t.Errorf("selectedProcess after navigation = %+v (ok=%v), want PID 103", sel, ok)
	}
}

// TestProcessRowColoring verifies the hand-rendered table colours process-state cells
// (the reason it isn't a bubbles/table, whose cell truncation corrupts embedded ANSI) and
// keeps every row exactly the table width — i.e. the colour codes don't break alignment.
func TestProcessRowColoring(t *testing.T) {
	cols := processColumns(120, procSortCPU)
	tableW := 0
	for _, c := range cols {
		tableW += c.width
	}
	cases := []struct {
		state string
		style lipgloss.Style
	}{
		{"R", lipgloss.NewStyle().Foreground(colorPurple)},
		{"D", alertWarnStyle},
		{"Z", alertCritStyle},
	}
	for _, tc := range cases {
		p := api.ProcessMetric{PID: 1, Name: "x", State: tc.state, Enriched: true, StartTimeNs: time.Now().Add(-time.Hour).UnixNano()}
		row := renderProcessRow(p, cols, false, nil, tableW)
		sample := tc.style.Render("·") // "\x1b[<sgr>m·\x1b[0m" — the opening SGR is what we expect in the row
		sgr := sample[:strings.Index(sample, "m")+1]
		if !strings.Contains(row, sgr) {
			t.Errorf("state %s: row missing the expected colour escape %q", tc.state, sgr)
		}
		if w := lipgloss.Width(row); w != tableW {
			t.Errorf("state %s: row width = %d, want %d (colour broke alignment)", tc.state, w, tableW)
		}
	}
}

// TestProcessSearchFiltersAndClears exercises the textinput-backed search: '/' focuses it,
// typed runes become the filter, and esc clears it.
func TestProcessSearchFiltersAndClears(t *testing.T) {
	m := processSplitModel(t, 30, 140, 40)

	updated, _ := m.Update(key("/"))
	m = updated.(Model)
	if !m.procSearchActive {
		t.Fatal("'/' should activate search")
	}
	for _, r := range "proca" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	if m.procSearchQuery != "proca" {
		t.Errorf("search query = %q, want %q", m.procSearchQuery, "proca")
	}
	// enter keeps the filter, leaves edit mode.
	updated, _ = m.Update(key("enter"))
	m = updated.(Model)
	if m.procSearchActive || m.procSearchQuery != "proca" {
		t.Errorf("after enter: active=%v query=%q, want false/proca", m.procSearchActive, m.procSearchQuery)
	}
	// esc clears the filter.
	updated, _ = m.Update(key("esc"))
	m = updated.(Model)
	if m.procSearchQuery != "" {
		t.Errorf("after esc, query = %q, want empty", m.procSearchQuery)
	}
}
