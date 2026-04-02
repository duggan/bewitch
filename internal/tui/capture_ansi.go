package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// DemoState identifies a unique renderable TUI state.
type DemoState struct {
	View            view
	HistoryRange    int           // index into historyRanges
	HardwareSection int           // hwSectionTemp..hwSectionGPU (hardware view only)
	ProcessSort     procSortField // process view only
}

// StateKey returns a string identifier for this state, used as JSON map key
// and transition table reference. Format: "view" or "view/qualifier/range".
func (s DemoState) StateKey(rangeLabels []string) string {
	base := captureFileName[s.View]
	switch s.View {
	case viewDashboard, viewAlerts:
		return base
	case viewHardware:
		section := [...]string{"temp", "power", "ecc", "gpu"}[s.HardwareSection]
		return fmt.Sprintf("%s/%s/%s", base, section, rangeLabels[s.HistoryRange])
	case viewProcess:
		sort := [...]string{"cpu", "mem", "pid", "name", "threads", "fds"}[s.ProcessSort]
		return fmt.Sprintf("%s/%s/%s", base, sort, rangeLabels[s.HistoryRange])
	default:
		return fmt.Sprintf("%s/%s", base, rangeLabels[s.HistoryRange])
	}
}

// DemoStateMap is the complete output for the browser demo player.
type DemoStateMap struct {
	Cols        int                        `json:"cols"`
	Rows        int                        `json:"rows"`
	States      map[string][]string        `json:"states"`      // stateKey → ANSI frames
	Transitions map[string]map[string]string `json:"transitions"` // stateKey → { key → stateKey }
	Initial     string                     `json:"initial"`
}

// EnumerateStates returns all valid DemoState combinations for the given config.
func EnumerateStates(numRanges int) []DemoState {
	var states []DemoState
	views := []view{viewDashboard, viewCPU, viewMemory, viewDisk, viewNetwork, viewHardware, viewProcess, viewAlerts}

	for _, v := range views {
		switch v {
		case viewDashboard, viewAlerts:
			states = append(states, DemoState{View: v})
		case viewHardware:
			for sec := hwSectionTemp; sec <= hwSectionGPU; sec++ {
				for r := 0; r < numRanges; r++ {
					states = append(states, DemoState{View: v, HistoryRange: r, HardwareSection: sec})
				}
			}
		case viewProcess:
			for sort := procSortCPU; sort <= procSortFDs; sort++ {
				for r := 0; r < numRanges; r++ {
					states = append(states, DemoState{View: v, HistoryRange: r, ProcessSort: sort})
				}
			}
		default:
			for r := 0; r < numRanges; r++ {
				states = append(states, DemoState{View: v, HistoryRange: r})
			}
		}
	}
	return states
}

// BuildTransitions computes the key→state transition table for all states.
func BuildTransitions(states []DemoState, rangeLabels []string) map[string]map[string]string {
	numRanges := len(rangeLabels)
	numViews := int(viewCount)
	keyMap := make(map[string]string, len(states))
	for _, s := range states {
		keyMap[s.StateKey(rangeLabels)] = "" // just for existence check later
	}

	result := make(map[string]map[string]string, len(states))
	for _, s := range states {
		key := s.StateKey(rangeLabels)
		t := make(map[string]string)

		// Number keys 1-8: switch view, preserve range
		for vi := 0; vi < numViews; vi++ {
			target := DemoState{View: view(vi), HistoryRange: s.HistoryRange}
			switch view(vi) {
			case viewDashboard, viewAlerts:
				target.HistoryRange = 0
			case viewHardware:
				target.HardwareSection = s.HardwareSection
			case viewProcess:
				target.ProcessSort = s.ProcessSort
			}
			targetKey := target.StateKey(rangeLabels)
			if _, ok := keyMap[targetKey]; ok && targetKey != key {
				t[fmt.Sprintf("%d", vi+1)] = targetKey
			}
		}

		// Arrow keys: prev/next view
		prevView := (int(s.View) - 1 + numViews) % numViews
		nextView := (int(s.View) + 1) % numViews
		prevState := DemoState{View: view(prevView), HistoryRange: s.HistoryRange}
		nextState := DemoState{View: view(nextView), HistoryRange: s.HistoryRange}
		switch view(prevView) {
		case viewDashboard, viewAlerts:
			prevState.HistoryRange = 0
		case viewHardware:
			prevState.HardwareSection = s.HardwareSection
		case viewProcess:
			prevState.ProcessSort = s.ProcessSort
		}
		switch view(nextView) {
		case viewDashboard, viewAlerts:
			nextState.HistoryRange = 0
		case viewHardware:
			nextState.HardwareSection = s.HardwareSection
		case viewProcess:
			nextState.ProcessSort = s.ProcessSort
		}
		if pk := prevState.StateKey(rangeLabels); pk != key {
			t["ArrowLeft"] = pk
		}
		if nk := nextState.StateKey(rangeLabels); nk != key {
			t["ArrowRight"] = nk
		}

		// < / > : history range navigation (views that have ranges)
		switch s.View {
		case viewDashboard, viewAlerts:
			// no range navigation
		default:
			if s.HistoryRange > 0 {
				prev := s
				prev.HistoryRange--
				t["<"] = prev.StateKey(rangeLabels)
				t[","] = prev.StateKey(rangeLabels)
			}
			if s.HistoryRange < numRanges-1 {
				next := s
				next.HistoryRange++
				t[">"] = next.StateKey(rangeLabels)
				t["."] = next.StateKey(rangeLabels)
			}
		}

		// Tab / Shift+Tab: hardware section cycling
		if s.View == viewHardware {
			nextSec := s
			nextSec.HardwareSection = (s.HardwareSection + 1) % 4
			prevSec := s
			prevSec.HardwareSection = (s.HardwareSection + 3) % 4
			t["Tab"] = nextSec.StateKey(rangeLabels)
			t["Shift+Tab"] = prevSec.StateKey(rangeLabels)
		}

		// Process sort keys
		if s.View == viewProcess {
			sortKeys := []struct {
				key  string
				sort procSortField
			}{
				{"c", procSortCPU}, {"m", procSortMem}, {"p", procSortPID},
				{"n", procSortName}, {"t", procSortThreads}, {"f", procSortFDs},
			}
			for _, sk := range sortKeys {
				if sk.sort != s.ProcessSort {
					target := s
					target.ProcessSort = sk.sort
					t[sk.key] = target.StateKey(rangeLabels)
				}
			}
		}

		result[key] = t
	}
	return result
}

// CaptureStateMappedANSI renders all enumerated TUI states to ANSI strings,
// capturing multiple frames per state with a delay between each to show data
// changing over time. Returns a complete DemoStateMap for the browser player.
func (m *Model) CaptureStateMappedANSI(frameCount int, frameDelay time.Duration) (*DemoStateMap, error) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)

	m.clearETagCache()
	m.initSelectionMaps()

	rangeLabels := make([]string, len(m.historyRanges))
	for i, r := range m.historyRanges {
		rangeLabels[i] = r.Label
	}

	allStates := EnumerateStates(len(m.historyRanges))
	transitions := BuildTransitions(allStates, rangeLabels)

	result := &DemoStateMap{
		Cols:        m.width,
		Rows:        m.height,
		States:      make(map[string][]string, len(allStates)),
		Transitions: transitions,
		Initial:     "dashboard",
	}

	// Group states by (view, historyRange) so we can share fetched history data.
	// Within a group, we vary only hardwareSection or processSort which don't
	// require re-fetching history.
	type groupKey struct {
		v     view
		rng   int
	}
	groups := make(map[groupKey][]DemoState)
	var groupOrder []groupKey
	for _, s := range allStates {
		gk := groupKey{s.View, s.HistoryRange}
		if _, seen := groups[gk]; !seen {
			groupOrder = append(groupOrder, gk)
		}
		groups[gk] = append(groups[gk], s)
	}

	for _, gk := range groupOrder {
		statesInGroup := groups[gk]
		m.current = gk.v
		m.historyRange = gk.rng

		// Fetch history once for this (view, range) group.
		m.fetchHistoryForCapture(gk.v)

		// For each frame tick, refresh data then render all states in this group.
		for f := 0; f < frameCount; f++ {
			if f > 0 {
				time.Sleep(frameDelay)
				m.clearETagCache()
			}
			m.refreshDataForView(gk.v)

			for _, s := range statesInGroup {
				// Set the state-specific model fields.
				m.hardwareSection = s.HardwareSection
				m.procSortBy = s.ProcessSort

				frame := m.renderCurrentContent()
				frame = m.normalizeFrameHeight(frame)

				key := s.StateKey(rangeLabels)
				result.States[key] = append(result.States[key], frame)
			}
		}
	}

	return result, nil
}

// initSelectionMaps ensures selection maps are populated for chart rendering.
func (m *Model) initSelectionMaps() {
	if m.netSelected == nil && m.netData != nil {
		m.netSelected = make(map[string]bool)
		for _, n := range m.netData {
			m.netSelected[n.Interface] = true
		}
	}
	if m.tempSelected == nil && m.tempData != nil {
		m.tempSelected = make(map[string]bool)
		for _, t := range m.tempData {
			m.tempSelected[t.Sensor] = true
		}
	}
	if m.powerSelected == nil && m.powerData != nil {
		m.powerSelected = make(map[string]bool)
		for _, p := range m.powerData {
			m.powerSelected[p.Zone] = true
		}
	}
}

// fetchHistoryForCapture fetches and renders history chart for a view.
func (m *Model) fetchHistoryForCapture(v view) {
	end := time.Now()
	start := end.Add(-m.historyRanges[m.historyRange].Duration)

	metric := viewMetric(v)
	if v == viewHardware {
		metric = "temperature"
	}
	if metric != "" {
		series, err := m.client.GetHistory(metric, start, end)
		if err == nil && len(series) > 0 {
			m.historySeries = series
			m.historyStart = start
			m.historyEnd = end
			m.regenerateHistoryChart()
		} else {
			m.cachedHistoryCharts[m.current] = ""
		}
	} else {
		m.cachedHistoryCharts[m.current] = ""
	}
}

// refreshDataForView fetches fresh metric data for the given view.
func (m *Model) refreshDataForView(v view) {
	switch v {
	case viewDashboard:
		m.refreshDashData()
		m.refreshCPUData()
		m.refreshMemData()
		m.refreshDiskData()
		m.refreshNetData()
		m.refreshTempData()
		m.refreshPowerData()
		m.refreshGPUData()
	case viewCPU:
		m.refreshCPUData()
	case viewMemory:
		m.refreshMemData()
	case viewDisk:
		m.refreshDiskData()
	case viewNetwork:
		m.refreshNetData()
	case viewHardware:
		m.refreshTempData()
		m.refreshPowerData()
		m.refreshGPUData()
	case viewProcess:
		m.refreshProcessData()
	case viewAlerts:
		m.refreshAlertsData()
		m.refreshAlertRules()
	}
}

// clearETagCache clears the client's ETag cache so the next fetch returns fresh data.
func (m *Model) clearETagCache() {
	if dc, ok := m.client.(*DaemonClient); ok {
		dc.etagsMu.Lock()
		dc.etags = make(map[string]string)
		dc.etagsMu.Unlock()
	}
}

// normalizeFrameHeight pads or truncates content to exactly m.height lines.
func (m *Model) normalizeFrameHeight(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
