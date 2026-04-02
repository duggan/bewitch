package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// ANSIFrameSet holds pre-rendered ANSI frames for all views, suitable for
// playback in a browser terminal emulator (e.g. xterm.js).
type ANSIFrameSet struct {
	Cols  int                 `json:"cols"`
	Rows  int                 `json:"rows"`
	Views map[string][]string `json:"views"`
}

// CaptureAllViewsANSI renders all views to ANSI strings, capturing multiple
// frames per view with a delay between each to show data changing over time.
// The mock daemon must be running so fresh data is available between frames.
func (m *Model) CaptureAllViewsANSI(frameCount int, frameDelay time.Duration) (*ANSIFrameSet, error) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)

	m.hardwareSection = hwSectionTemp

	// Clear ETag cache so fetches don't return 304.
	if dc, ok := m.client.(*DaemonClient); ok {
		dc.etagsMu.Lock()
		dc.etags = make(map[string]string)
		dc.etagsMu.Unlock()
	}

	// Initialize selection maps (same as CaptureAllViews).
	m.initSelectionMaps()

	result := &ANSIFrameSet{
		Cols:  m.width,
		Rows:  m.height,
		Views: make(map[string][]string),
	}

	views := []view{viewDashboard, viewCPU, viewMemory, viewDisk, viewNetwork, viewHardware, viewProcess, viewAlerts}

	for _, v := range views {
		name := captureFileName[v]
		m.current = v

		// Fetch history once per view (charts don't change between frames).
		m.fetchHistoryForCapture(v)

		frames := make([]string, 0, frameCount)
		for f := 0; f < frameCount; f++ {
			if f > 0 {
				// Wait for mock data to shift, then refresh metrics.
				time.Sleep(frameDelay)
				m.clearETagCache()
			}
			m.refreshDataForView(v)
			frame := m.captureViewContent()
			frame = m.normalizeFrameHeight(frame)
			frames = append(frames, frame)
		}

		result.Views[name] = frames
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
