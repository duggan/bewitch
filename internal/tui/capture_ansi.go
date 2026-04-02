package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/duggan/bewitch/internal/api"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// alertAction represents a mutation applied to the alerts view state.
type alertAction int

const (
	alertActionNone    alertAction = iota
	alertActionToggle              // space: toggle enabled/disabled on rule at cursor
	alertActionDelete              // d: delete rule at cursor
	alertActionClear               // c: clear all fired alerts
)

// DemoState identifies a unique renderable TUI state.
type DemoState struct {
	View            view
	HistoryRange    int           // index into historyRanges
	HardwareSection int           // hwSectionTemp..hwSectionGPU (hardware view only)
	ProcessSort     procSortField // process view only
	AlertFocus      int           // 0=rules, 1=alerts (alerts view only)
	CursorPos       int           // cursor position in selector list (-1 = no cursor)
	CursorDeselected bool         // true = item at cursor is toggled off
	AlertAction     alertAction   // mutation applied to alerts view
}

// StateKey returns a string identifier for this state, used as JSON map key
// and transition table reference. Format: "view" or "view/qualifier/range".
func (s DemoState) StateKey(rangeLabels []string) string {
	base := captureFileName[s.View]
	cursorSuffix := ""
	if s.CursorPos >= 0 {
		cursorSuffix = fmt.Sprintf("/c%d", s.CursorPos)
		if s.CursorDeselected {
			cursorSuffix += "/off"
		}
	}
	switch s.View {
	case viewDashboard:
		return base
	case viewAlerts:
		suffix := ""
		if s.CursorPos >= 0 {
			suffix = fmt.Sprintf("/c%d", s.CursorPos)
		}
		switch s.AlertAction {
		case alertActionToggle:
			suffix += "/toggled"
		case alertActionDelete:
			suffix += "/deleted"
		case alertActionClear:
			return base + "/cleared"
		}
		if s.AlertFocus == 1 {
			return base + "/fired" + suffix
		}
		return base + suffix
	case viewHardware:
		section := [...]string{"temp", "power", "ecc", "gpu"}[s.HardwareSection]
		return fmt.Sprintf("%s/%s/%s%s", base, section, rangeLabels[s.HistoryRange], cursorSuffix)
	case viewNetwork:
		return fmt.Sprintf("%s/%s%s", base, rangeLabels[s.HistoryRange], cursorSuffix)
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

// SelectorItemCounts holds the number of items in each selector list.
type SelectorItemCounts struct {
	TempSensors int
	PowerZones  int
	GPUDevices  int
	NetIfaces   int
	AlertRules  int
}

// EnumerateStates returns all valid DemoState combinations for the given config.
// For views with selectors (hw/network), each item gets a cursor state with
// selected and deselected variants.
func EnumerateStates(numRanges int, items SelectorItemCounts) []DemoState {
	var states []DemoState
	views := []view{viewDashboard, viewCPU, viewMemory, viewDisk, viewNetwork, viewHardware, viewProcess, viewAlerts}

	// addSelectorStates adds states for a selector with N items:
	// one base state (no cursor, all selected) plus per-item cursor states.
	addSelectorStates := func(base DemoState, numItems int) {
		// Base state: no cursor active, all selected
		base.CursorPos = -1
		states = append(states, base)
		// Per-item states: cursor on each item, selected and deselected
		for i := 0; i < numItems; i++ {
			sel := base
			sel.CursorPos = i
			sel.CursorDeselected = false
			states = append(states, sel)
			desel := base
			desel.CursorPos = i
			desel.CursorDeselected = true
			states = append(states, desel)
		}
	}

	for _, v := range views {
		switch v {
		case viewDashboard:
			states = append(states, DemoState{View: v, CursorPos: -1})
		case viewAlerts:
			// Base states: rules panel and fired alerts panel
			states = append(states, DemoState{View: v, AlertFocus: 0, CursorPos: -1})
			states = append(states, DemoState{View: v, AlertFocus: 1, CursorPos: -1})
			// Cursor states in rules panel with toggle/delete actions
			for i := 0; i < items.AlertRules; i++ {
				states = append(states, DemoState{View: v, AlertFocus: 0, CursorPos: i})
				states = append(states, DemoState{View: v, AlertFocus: 0, CursorPos: i, AlertAction: alertActionToggle})
				states = append(states, DemoState{View: v, AlertFocus: 0, CursorPos: i, AlertAction: alertActionDelete})
			}
			// Cleared fired alerts state
			states = append(states, DemoState{View: v, AlertFocus: 0, CursorPos: -1, AlertAction: alertActionClear})
		case viewHardware:
			for sec := hwSectionTemp; sec <= hwSectionGPU; sec++ {
				var itemCount int
				switch sec {
				case hwSectionTemp:
					itemCount = items.TempSensors
				case hwSectionPower:
					itemCount = items.PowerZones
				case hwSectionGPU:
					itemCount = items.GPUDevices
				default:
					itemCount = 0 // ECC has no selector
				}
				for r := 0; r < numRanges; r++ {
					base := DemoState{View: v, HistoryRange: r, HardwareSection: sec}
					if itemCount > 0 {
						addSelectorStates(base, itemCount)
					} else {
						base.CursorPos = -1
						states = append(states, base)
					}
				}
			}
		case viewNetwork:
			for r := 0; r < numRanges; r++ {
				base := DemoState{View: v, HistoryRange: r}
				addSelectorStates(base, items.NetIfaces)
			}
		case viewProcess:
			for sort := procSortCPU; sort <= procSortFDs; sort++ {
				for r := 0; r < numRanges; r++ {
					states = append(states, DemoState{View: v, HistoryRange: r, ProcessSort: sort, CursorPos: -1})
				}
			}
		default:
			for r := 0; r < numRanges; r++ {
				states = append(states, DemoState{View: v, HistoryRange: r, CursorPos: -1})
			}
		}
	}
	return states
}

// selectorItemCount returns the number of items in the selector list for the
// given state, or 0 if the state's view has no selector.
func selectorItemCount(s DemoState, items SelectorItemCounts) int {
	switch s.View {
	case viewHardware:
		switch s.HardwareSection {
		case hwSectionTemp:
			return items.TempSensors
		case hwSectionPower:
			return items.PowerZones
		case hwSectionGPU:
			return items.GPUDevices
		}
	case viewNetwork:
		return items.NetIfaces
	}
	return 0
}

// BuildTransitions computes the key→state transition table for all states.
// defaultRange is the index used when switching from a rangeless view (dashboard/alerts)
// to a view that has ranges, matching the TUI's default (typically index 3 = "7d").
func BuildTransitions(states []DemoState, rangeLabels []string, defaultRange int, items SelectorItemCounts) map[string]map[string]string {
	numRanges := len(rangeLabels)
	numViews := int(viewCount)
	if defaultRange >= numRanges {
		defaultRange = numRanges - 1
	}
	keyMap := make(map[string]string, len(states))
	for _, s := range states {
		keyMap[s.StateKey(rangeLabels)] = "" // just for existence check later
	}

	// effectiveRange returns the range to use when switching to a target view.
	// Views without ranges (dashboard, alerts) always use 0.
	// When coming from a rangeless view, use defaultRange instead of 0.
	effectiveRange := func(targetView view, sourceRange int, sourceView view) int {
		if targetView == viewDashboard || targetView == viewAlerts {
			return 0
		}
		if sourceView == viewDashboard || sourceView == viewAlerts {
			return defaultRange
		}
		return sourceRange
	}

	result := make(map[string]map[string]string, len(states))
	for _, s := range states {
		key := s.StateKey(rangeLabels)
		t := make(map[string]string)

		// Number keys 1-8: switch view, preserve range
		for vi := 0; vi < numViews; vi++ {
			target := DemoState{View: view(vi), HistoryRange: effectiveRange(view(vi), s.HistoryRange, s.View)}
			switch view(vi) {
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
		prevState := DemoState{View: view(prevView), HistoryRange: effectiveRange(view(prevView), s.HistoryRange, s.View)}
		nextState := DemoState{View: view(nextView), HistoryRange: effectiveRange(view(nextView), s.HistoryRange, s.View)}
		switch view(prevView) {
		case viewHardware:
			prevState.HardwareSection = s.HardwareSection
		case viewProcess:
			prevState.ProcessSort = s.ProcessSort
		}
		switch view(nextView) {
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
			for _, dir := range []struct {
				key  string
				delta int
			}{{"Tab", 1}, {"Shift+Tab", 3}} {
				sec := s
				sec.HardwareSection = (s.HardwareSection + dir.delta) % 4
				sec.CursorDeselected = false
				// Clamp cursor if target section has fewer items
				targetItems := selectorItemCount(sec, items)
				if sec.CursorPos >= targetItems {
					sec.CursorPos = -1
				}
				t[dir.key] = sec.StateKey(rangeLabels)
			}
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

		// Alerts: Tab, cursor nav, space/d/c
		if s.View == viewAlerts {
			// Tab toggles focus (reset cursor when switching panels)
			target := s
			target.AlertFocus = (s.AlertFocus + 1) % 2
			target.CursorPos = -1
			target.AlertAction = alertActionNone
			t["Tab"] = target.StateKey(rangeLabels)

			if s.AlertFocus == 0 && s.AlertAction == alertActionNone {
				// Rules panel: cursor navigation
				numRules := items.AlertRules
				if s.CursorPos == -1 && numRules > 0 {
					// Enter cursor mode
					enter := s
					enter.CursorPos = 0
					t["ArrowDown"] = enter.StateKey(rangeLabels)
					t["j"] = enter.StateKey(rangeLabels)
				}
				if s.CursorPos >= 0 {
					if s.CursorPos > 0 {
						prev := s
						prev.CursorPos--
						t["ArrowUp"] = prev.StateKey(rangeLabels)
						t["k"] = prev.StateKey(rangeLabels)
					}
					if s.CursorPos < numRules-1 {
						next := s
						next.CursorPos++
						t["ArrowDown"] = next.StateKey(rangeLabels)
						t["j"] = next.StateKey(rangeLabels)
					}
					// space toggles enabled/disabled
					toggled := s
					toggled.AlertAction = alertActionToggle
					t[" "] = toggled.StateKey(rangeLabels)
					// d deletes rule
					deleted := s
					deleted.AlertAction = alertActionDelete
					t["d"] = deleted.StateKey(rangeLabels)
				}
				// c clears fired alerts (from any cursor position)
				cleared := DemoState{View: s.View, AlertFocus: 0, CursorPos: -1, AlertAction: alertActionClear}
				t["c"] = cleared.StateKey(rangeLabels)
			}
			// From action states, allow navigating back
			if s.AlertAction != alertActionNone {
				back := s
				back.AlertAction = alertActionNone
				if s.AlertAction == alertActionDelete {
					// After delete, cursor stays but action resets
					back.CursorPos = -1
				}
				// Any key goes back to base
				t["ArrowDown"] = back.StateKey(rangeLabels)
				t["ArrowUp"] = back.StateKey(rangeLabels)
				t["j"] = back.StateKey(rangeLabels)
				t["k"] = back.StateKey(rangeLabels)
				t[" "] = back.StateKey(rangeLabels)
			}
		}

		// Selector cursor navigation (up/down/j/k) and toggle (space)
		// Alerts view handles its own cursor separately below.
		if s.CursorPos >= 0 && s.View != viewAlerts {
			numItems := selectorItemCount(s, items)
			// up/down/j/k move cursor
			if s.CursorPos > 0 {
				prev := s
				prev.CursorPos--
				prev.CursorDeselected = false // moving cursor resets to selected view
				t["ArrowUp"] = prev.StateKey(rangeLabels)
				t["k"] = prev.StateKey(rangeLabels)
			}
			if s.CursorPos < numItems-1 {
				next := s
				next.CursorPos++
				next.CursorDeselected = false
				t["ArrowDown"] = next.StateKey(rangeLabels)
				t["j"] = next.StateKey(rangeLabels)
			}
			// space toggles the item at cursor
			toggle := s
			toggle.CursorDeselected = !s.CursorDeselected
			t[" "] = toggle.StateKey(rangeLabels)
		} else if s.View == viewHardware || s.View == viewNetwork {
			// From base state (no cursor), down/j enters cursor mode at item 0
			enter := s
			enter.CursorPos = 0
			t["ArrowDown"] = enter.StateKey(rangeLabels)
			t["j"] = enter.StateKey(rangeLabels)
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

	// Fetch initial data so we know item counts for selectors.
	m.refreshDataForView(viewDashboard)
	m.refreshDataForView(viewAlerts)
	m.initSelectionMaps()

	rangeLabels := make([]string, len(m.historyRanges))
	for i, r := range m.historyRanges {
		rangeLabels[i] = r.Label
	}

	// Count selector items from the currently loaded data.
	items := SelectorItemCounts{
		TempSensors: len(m.tempData),
		PowerZones:  len(m.powerData),
		GPUDevices:  len(m.gpuData),
		NetIfaces:   len(m.netData),
		AlertRules:  len(m.alertRules),
	}

	allStates := EnumerateStates(len(m.historyRanges), items)
	// Default to 24h (index 2) for the demo — close enough to show meaningful
	// chart data without the sparse look of 7d/30d ranges.
	defaultRange := 2
	if defaultRange >= len(m.historyRanges) {
		defaultRange = len(m.historyRanges) - 1
	}
	transitions := BuildTransitions(allStates, rangeLabels, defaultRange, items)

	result := &DemoStateMap{
		Cols:        m.width,
		Rows:        m.height,
		States:      make(map[string][]string, len(allStates)),
		Transitions: transitions,
		Initial:     "dashboard",
	}

	// Group states by (view, historyRange, hardwareSection) so we share fetched
	// data within a group. Hardware sub-sections need different history metrics
	// (temperature vs power vs gpu), so they can't share a single fetch.
	type groupKey struct {
		v   view
		rng int
		hw  int // only meaningful for viewHardware
	}
	groups := make(map[groupKey][]DemoState)
	var groupOrder []groupKey
	for _, s := range allStates {
		gk := groupKey{s.View, s.HistoryRange, s.HardwareSection}
		if s.View != viewHardware {
			gk.hw = 0
		}
		if _, seen := groups[gk]; !seen {
			groupOrder = append(groupOrder, gk)
		}
		groups[gk] = append(groups[gk], s)
	}

	for _, gk := range groupOrder {
		statesInGroup := groups[gk]
		m.current = gk.v
		m.historyRange = gk.rng
		if gk.v == viewHardware {
			m.hardwareSection = gk.hw
		}

		// Fetch history for this group (metric depends on view + hw section).
		m.fetchHistoryForCapture(gk.v)

		// For each frame tick, refresh data then render all states in this group.
		for f := 0; f < frameCount; f++ {
			if f > 0 {
				time.Sleep(frameDelay)
				m.clearETagCache()
			}
			m.refreshDataForView(gk.v)

			// Save base alert data for mutation states.
			baseRules := make([]api.AlertRuleMetric, len(m.alertRules))
			copy(baseRules, m.alertRules)
			baseAlerts := m.alertsData

			for _, s := range statesInGroup {
				// Set the state-specific model fields.
				m.hardwareSection = s.HardwareSection
				m.procSortBy = s.ProcessSort

				// Apply view-specific state.
				if s.View == viewAlerts {
					m.applyAlertState(s, baseRules, baseAlerts)
				} else {
					m.alertFocus = s.AlertFocus
					m.applySelectorState(s)
				}

				frame := m.renderCurrentContent()
				frame = m.padFrameHeight(frame)

				key := s.StateKey(rangeLabels)
				result.States[key] = append(result.States[key], frame)
			}
		}
	}

	return result, nil
}

// applySelectorState sets the model's cursor position and selection maps
// to match the given DemoState. For states with CursorPos >= 0, the item
// at the cursor is toggled based on CursorDeselected; all other items
// remain selected. For base states (CursorPos == -1), all items are selected.
func (m *Model) applySelectorState(s DemoState) {
	// Reset all selections to true (default)
	for k := range m.tempSelected {
		m.tempSelected[k] = true
	}
	for k := range m.powerSelected {
		m.powerSelected[k] = true
	}
	for k := range m.netSelected {
		m.netSelected[k] = true
	}
	for k := range m.gpuSelected {
		m.gpuSelected[k] = true
	}

	// Set cursor position per view
	switch s.View {
	case viewHardware:
		switch s.HardwareSection {
		case hwSectionTemp:
			if s.CursorPos >= 0 {
				m.tempCursor = s.CursorPos
				if s.CursorDeselected && s.CursorPos < len(m.tempSensorNames) {
					m.tempSelected[m.tempSensorNames[s.CursorPos]] = false
				}
			}
		case hwSectionPower:
			if s.CursorPos >= 0 {
				m.powerCursor = s.CursorPos
				if s.CursorDeselected && s.CursorPos < len(m.powerZoneNames) {
					m.powerSelected[m.powerZoneNames[s.CursorPos]] = false
				}
			}
		case hwSectionGPU:
			if s.CursorPos >= 0 {
				m.gpuCursor = s.CursorPos
				if s.CursorDeselected && s.CursorPos < len(m.gpuDeviceNames) {
					m.gpuSelected[m.gpuDeviceNames[s.CursorPos]] = false
				}
			}
		}
	case viewNetwork:
		if s.CursorPos >= 0 {
			m.netCursor = s.CursorPos
			if s.CursorDeselected && s.CursorPos < len(m.netIfaceNames) {
				m.netSelected[m.netIfaceNames[s.CursorPos]] = false
			}
		}
	}

	// Regenerate chart to reflect selection changes
	if s.View == viewHardware || s.View == viewNetwork {
		m.regenerateHistoryChart()
	}
}

// applyAlertState sets alert-specific model state: cursor position, focus,
// and mutations (toggle/delete/clear). It works on copies of data to avoid
// permanently modifying the base state.
func (m *Model) applyAlertState(s DemoState, baseRules []api.AlertRuleMetric, baseAlerts []api.AlertMetric) {
	m.alertFocus = s.AlertFocus
	m.alertRuleCursor = s.CursorPos
	if m.alertRuleCursor < 0 {
		m.alertRuleCursor = 0
	}

	// Start from copies of the base data
	rules := make([]api.AlertRuleMetric, len(baseRules))
	copy(rules, baseRules)
	alerts := baseAlerts

	switch s.AlertAction {
	case alertActionToggle:
		if s.CursorPos >= 0 && s.CursorPos < len(rules) {
			rules[s.CursorPos].Enabled = !rules[s.CursorPos].Enabled
		}
	case alertActionDelete:
		if s.CursorPos >= 0 && s.CursorPos < len(rules) {
			rules = append(rules[:s.CursorPos], rules[s.CursorPos+1:]...)
		}
	case alertActionClear:
		alerts = nil
	}

	m.alertRules = rules
	m.alertsData = alerts
}

// initSelectionMaps ensures selection maps are populated for chart rendering.
func (m *Model) initSelectionMaps() {
	if m.netSelected == nil && m.netData != nil {
		m.netSelected = make(map[string]bool)
		m.netIfaceNames = make([]string, len(m.netData))
		for i, n := range m.netData {
			m.netSelected[n.Interface] = true
			m.netIfaceNames[i] = n.Interface
		}
	}
	if m.tempSelected == nil && m.tempData != nil {
		m.tempSelected = make(map[string]bool)
		m.tempSensorNames = make([]string, len(m.tempData))
		for i, t := range m.tempData {
			m.tempSelected[t.Sensor] = true
			m.tempSensorNames[i] = t.Sensor
		}
	}
	if m.powerSelected == nil && m.powerData != nil {
		m.powerSelected = make(map[string]bool)
		m.powerZoneNames = make([]string, len(m.powerData))
		for i, p := range m.powerData {
			m.powerSelected[p.Zone] = true
			m.powerZoneNames[i] = p.Zone
		}
	}
	if m.gpuSelected == nil && m.gpuData != nil {
		m.gpuSelected = make(map[string]bool)
		m.gpuDeviceNames = make([]string, len(m.gpuData))
		for i, g := range m.gpuData {
			m.gpuSelected[g.Name] = true
			m.gpuDeviceNames[i] = g.Name
		}
	}
}

// fetchHistoryForCapture fetches and renders history chart for a view.
func (m *Model) fetchHistoryForCapture(v view) {
	end := time.Now()
	start := end.Add(-m.historyRanges[m.historyRange].Duration)

	metric := viewMetric(v)
	if v == viewHardware {
		switch m.hardwareSection {
		case hwSectionTemp:
			metric = "temperature"
		case hwSectionPower:
			metric = "power"
		case hwSectionGPU:
			metric = "gpu"
		default:
			metric = "" // ECC has no history
		}
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

// padFrameHeight pads content to at least m.height lines but preserves extra
// lines beyond that, enabling client-side viewport scrolling in the demo.
func (m *Model) padFrameHeight(content string) string {
	lines := strings.Split(content, "\n")
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
