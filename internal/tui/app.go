package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/duggan/bewitch/internal/alert"
	"github.com/duggan/bewitch/internal/api"
	"github.com/duggan/bewitch/internal/config"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

type view int

const (
	viewDashboard view = iota
	viewCPU
	viewMemory
	viewDisk
	viewNetwork
	viewHardware
	viewProcess
	viewAlerts
	viewServices // user-defined custom HTTP sources; appended only when configured
	viewCount    // sentinel for wrapping
)

// Hardware sub-sections within the Hardware tab.
const (
	hwSectionTemp  = 0
	hwSectionPower = 1
	hwSectionECC   = 2
	hwSectionGPU   = 3
)

type tickMsg time.Time
type historyResultMsg struct {
	forView     view
	series      []api.TimeSeries
	start       time.Time
	end         time.Time
	err         error
	duration    time.Duration
	incremental bool      // true if this was a narrow tail fetch
	windowStart time.Time // left edge of rolling window (for trim)
}
type pinnedHistoryResultMsg struct {
	series      []api.TimeSeries
	start       time.Time
	end         time.Time
	err         error
	duration    time.Duration
	incremental bool      // true if this was a narrow tail fetch
	windowStart time.Time // left edge of rolling window (for trim)
}
type prefetchHistoryResultMsg struct {
	forView  view
	series   []api.TimeSeries
	start    time.Time
	end      time.Time
	err      error
	duration time.Duration
}
type notifyTestResultMsg struct {
	results []alert.NotifyResult
	err     error
	sentAt  time.Time
}

type captureResultMsg struct {
	path string
	err  error
}

type notifyLogEntry struct {
	Method     string
	Dest       string
	StatusCode int
	Latency    time.Duration
	Error      string
	Body       string
	SentAt     time.Time
}

type viewHistoryCache struct {
	series       []api.TimeSeries
	chart        string
	start        time.Time
	end          time.Time
	lastFetchEnd time.Time // end time of last successful fetch (for incremental)
	rangeIndex   int       // historyRange index when cache was created
	customRange  bool      // true if custom date range (static, no sliding)
	// Process Top-N incremental state: after the first full Top-N fetch,
	// subsequent ticks use GetHistoryByName with cached names + tail window.
	// A full Top-N refetch runs periodically to update the selection.
	topNNames      []string  // process names from last full Top-N fetch
	topNLastFullAt time.Time // when the last full Top-N query ran
}

type Model struct {
	historyRanges    []config.HistoryRange
	client           daemonClient
	current          view
	width            int
	height           int
	interval         time.Duration
	historyRange     int
	historySeries    []api.TimeSeries
	historyStart     time.Time // actual start time used when fetching history
	historyEnd       time.Time // actual end time used when fetching history
	viewport         viewport.Model
	alertTable       table.Model
	ready            bool
	visibleTabs      []view // ordered list of currently visible tabs
	tempSparkData    map[string][]float64
	tempSparkInited  bool
	tempSelected     map[string]bool
	tempCursor       int
	tempSensorNames  []string             // ordered sensor names for cursor navigation
	netSparkData     map[string][]float64 // keys: "iface_rx", "iface_tx"
	netSparkInited   bool
	netSelected      map[string]bool // keys: interface names
	netCursor        int
	netIfaceNames    []string
	netDisplayBits   bool
	powerSparkData   map[string][]float64
	powerSparkInited bool
	powerSelected    map[string]bool
	powerCursor      int
	powerZoneNames   []string
	gpuData          []api.GPUMetric
	gpuHints         []string
	gpuSparkData     map[string][]float64
	gpuSparkInited   bool
	gpuSelected      map[string]bool
	gpuCursor        int
	gpuDeviceNames   []string
	hardwareSection  int                  // active hardware sub-tab: hwSectionTemp, hwSectionPower, hwSectionECC, hwSectionGPU
	dashSparkData    map[string][]float64 // keys: "cpu", "mem"
	dashSparkInited  bool
	// Services view state (custom HTTP sources)
	customSources        []api.CustomSourceInfo // catalog, fetched once at startup
	customMetrics        []api.CustomMetric     // live values, refreshed on tick
	customStatus         []api.CustomStatus
	servicesSection      int // active source sub-section index (persisted)
	servicesMetricCursor int // selected metric within the active source (drives the chart)
	// Alert view state
	alertRules         []api.AlertRuleMetric
	alertRuleCursor    int
	alertFocus         int // 0 = rules panel, 1 = alerts table
	alertFormActive    bool
	alertForm          *huh.Form
	alertFormState     *alertFormState
	alertFormErr       string // last create/update error, surfaced in the alerts view
	alertConfirmDelete bool   // awaiting y/N confirmation to delete the rule under the cursor
	alertConfirmName   string // name of the rule pending delete confirmation
	notifyLog          []notifyLogEntry
	notifySending      bool
	// Process view state
	procSortBy             procSortField
	procCursor             int                  // selected row index into the displayed (ordered) process list
	procScroll             int                  // first visible row of the bounded table (kept so the cursor stays in view)
	procTableHeight        int                  // table height in lines (header + body), set per-frame by sizeProcTableForLayout
	procSearchInput        textinput.Model      // '/' search box (filters by name/cmdline)
	procData               *api.ProcessResponse // cached process data, refreshed on tick
	procSearchActive       bool                 // true when the search box is focused
	procSearchQuery        string               // current search filter text
	procFilteredLen        int                  // cached count of filtered results
	pinnedProcesses        map[string]bool      // process names pinned via TUI (stored in preferences)
	procPinnedOnly         bool                 // table filter: show only pinned processes
	procChartPinned        bool                 // chart mode: false=top CPU, true=pinned
	procPinnedSeries       []api.TimeSeries     // cached history data for pinned chart mode
	procPinnedChart        string               // pre-rendered pinned chart string
	procPinnedLastFetchEnd time.Time            // end time of last pinned fetch (for incremental)
	procPinnedNames        []string             // sorted pinned names at last fetch (detect changes)
	// Cached rendered chart strings (regenerated only when history data changes)
	cachedHistoryCharts map[view]string
	// Per-view history cache for instant view switching
	historyCache    map[view]*viewHistoryCache
	historyFetching map[view]bool // per-view: true while async history fetch is in flight
	// Cached view data (refreshed on tick, not on render)
	cpuData    []api.CPUCoreMetric
	memData    *api.MemoryMetric
	eccData    *api.ECCMetric
	diskData   []api.DiskMetric
	netData    []api.NetworkMetric
	tempData   []api.TemperatureMetric
	powerData  []api.PowerMetric
	alertsData []api.AlertMetric
	dashData   *api.DashboardData
	// Active (unacknowledged) alert summary, refreshed every tick on all views so the
	// status bar can surface problems at a glance even with no notifier backend configured.
	activeAlerts        int    // count of unacknowledged alerts
	activeAlertSeverity string // highest severity among them: "", "info", "warning", "critical"
	// Date picker overlay
	datePickerActive bool
	datePicker       datePickerModel
	customStart      *time.Time // non-nil = custom range active
	customEnd        *time.Time
	// Screen capture overlay
	captureFormActive bool
	captureForm       *huh.Form
	captureFormState  *captureFormState
	capturedContent   string          // ANSI string captured when 'x' pressed
	captureFlash      string          // success/error message shown briefly
	captureFlashUntil time.Time       // when flash message expires
	captureSettings   CaptureSettings // PNG rendering settings from config
	// Status data (fetched once at startup, rendered per-view)
	statusData map[string]any
	// Last time fresh data arrived per view (for staleness detection)
	lastDataChange map[view]time.Time
	// Debug console (nil when -debug not passed)
	debug *debugLog
}

func NewModel(client daemonClient, interval time.Duration, historyRanges []config.HistoryRange, captureSettings CaptureSettings, enableDebug bool) Model {
	// Initialize alert table
	columns := []table.Column{
		{Title: "Time", Width: 14},
		{Title: "Severity", Width: 10},
		{Title: "Rule", Width: 20},
		{Title: "Message", Width: 40},
		{Title: "Status", Width: 8},
	}
	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	t.SetStyles(metricTableStyles())

	// Process search box: '/' focuses it; its value is the name/cmdline filter.
	procSearch := textinput.New()
	procSearch.Prompt = "/"
	procSearch.PromptStyle = lipgloss.NewStyle().Foreground(colorPink)
	procSearch.Placeholder = "filter by name or cmdline"
	procSearch.CharLimit = 64

	// Default to last range or index 3, whichever is smaller
	defaultIdx := 3
	if defaultIdx >= len(historyRanges) {
		defaultIdx = len(historyRanges) - 1
	}
	var dbg *debugLog
	if enableDebug {
		dbg = newDebugLog(100)
	}
	m := Model{
		client:              client,
		current:             viewDashboard,
		width:               80,
		height:              24,
		interval:            interval,
		historyRanges:       historyRanges,
		historyRange:        defaultIdx,
		alertTable:          t,
		procSearchInput:     procSearch,
		debug:               dbg,
		captureSettings:     captureSettings,
		lastDataChange:      make(map[view]time.Time),
		cachedHistoryCharts: make(map[view]string),
		historyFetching:     make(map[view]bool),
	}
	m.loadHistoryRange()
	m.loadSelections()
	// Seed temperature and power data so updateVisibleTabs can check
	// data availability without making API calls on every tick.
	if temps, err := client.GetTemperature(); err == nil {
		m.tempData = temps
	}
	if zones, err := client.GetPower(); err == nil {
		m.powerData = zones
	}
	// Custom-source catalog is static; fetch once so updateVisibleTabs can decide
	// whether to show the Services tab.
	if sources, err := client.GetCustomSources(); err == nil {
		m.customSources = sources
	}
	if m.servicesSection >= len(m.customSources) {
		m.servicesSection = 0
	}
	m.updateVisibleTabs()
	m.initDashSparklines()
	// Fetch status info for persistent status bar
	if status, err := client.GetStatus(); err == nil {
		m.statusData = status
	}
	// Prefetch data for all views so tabs are populated immediately on switch
	m.refreshDashData()
	m.refreshCPUData()
	m.refreshMemData()
	m.refreshDiskData()
	m.refreshNetData()
	m.refreshProcessData()
	m.refreshAlertRules()
	m.refreshAlertsData()
	if len(m.customSources) > 0 {
		m.refreshCustomData()
	}
	m.d("init: interval=%s ranges=%d tabs=%d pinned=%d",
		interval, len(historyRanges), len(m.visibleTabs), len(m.pinnedProcesses))
	m.d("init: {/}=scroll debug, (/)=resize debug")
	return m
}

func (m Model) Init() tea.Cmd {
	// Fire an immediate tick so live data loads instantly on startup,
	// rather than waiting for the first interval to elapse.
	return func() tea.Msg { return tickMsg(time.Now()) }
}

func (m *Model) d(format string, args ...any) {
	if m.debug != nil {
		m.debug.Printf(format, args...)
	}
}

func viewName(v view) string {
	if info, ok := tabInfo[v]; ok {
		return info.name
	}
	return "?"
}

// metricTableStyles returns the shared bubbles/table styling for the alerts and process
// tables: deep-purple header rule, purple header text, and the hot-pink selected row.
func metricTableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorDeepPurple).
		BorderBottom(true).
		Bold(true).
		Foreground(colorPurple)
	s.Selected = s.Selected.
		Foreground(colorDarkBg).
		Background(colorPink).
		Bold(true)
	s.Cell = s.Cell.Foreground(colorText)
	return s
}

// setProcTableHeight sets a fallback process-table height (used before the first chart
// renders) and the search box width. The authoritative height is set per-frame by
// sizeProcTableForLayout, which measures the actual chart. The fallback budgets the
// chart panel (ch+4), the chart-mode tabs (3 lines), the detail strip (1), and the table
// panel's own title + borders (3) — ch+11 of non-table chrome.
func (m *Model) setProcTableHeight(contentHeight int) {
	ch := chartHeightForTerminal(m.height)
	h := contentHeight - ch - 10
	if h < 4 {
		h = 4
	}
	m.procTableHeight = h
	if w := m.width - 4; w > 0 {
		m.procSearchInput.Width = w
	}
}

// sizeProcTableForLayout sets the process table's height (m.procTableHeight) so the detail
// strip, chart-mode tabs, and history chart all stay on screen below it, and reports
// whether the chart fits. It measures the actual rendered widgets (chart == "" before the
// first one loads) so the budget can't drift. On a terminal too short for both a usable
// table and the chart, it returns false: the caller drops the chart and the table takes
// the space (the table is the more important half). Called per-frame from
// renderCurrentContent.
func (m *Model) sizeProcTableForLayout(chart string) (showChart bool) {
	avail := m.viewport.Height
	if m.procSearchActive {
		avail-- // the search box takes one line above the table
	}
	// The table panel costs tableHeight+3 rendered lines (pink title + two borders); the
	// detail strip is one line. The tabs+chart block is measured together (the chart-mode
	// tabs' trailing newline is absorbed by the chart that follows, so measuring them
	// concatenated avoids an off-by-one).
	const tablePanelChrome = 3
	const minRows = 5 // header (2) + a few data rows — below this the table is useless
	if chart != "" {
		below := 1 + lipgloss.Height(renderChartModeTabs(m.procChartPinned, m.width)+chart)
		if h := avail - tablePanelChrome - below; h >= minRows {
			m.procTableHeight = h
			return true
		}
	}
	// No chart, or no room for both: the table takes everything but the detail strip.
	h := avail - tablePanelChrome - 1
	if h < 4 {
		h = 4
	}
	m.procTableHeight = h
	return false
}

// procBody is the number of visible data rows in the process table (height minus the
// header + rule), used for page navigation.
func (m *Model) procBody() int {
	if b := m.procTableHeight - 2; b > 1 {
		return b
	}
	return 1
}

// moveProcCursor moves the selection by delta, flooring at 0. The upper bound is clamped
// per-frame in renderCurrentContent against the live (filtered) row count, so callers
// don't need the count here.
func (m *Model) moveProcCursor(delta int) {
	m.procCursor += delta
	if m.procCursor < 0 {
		m.procCursor = 0
	}
}

// clampProcView clamps the cursor to [0, rows-1] and scrolls the window so the cursor
// stays visible — the bookkeeping bubbles/table used to do internally. Called per-frame
// from renderCurrentContent with the current displayed row count.
func (m *Model) clampProcView(rows int) {
	if m.procCursor > rows-1 {
		m.procCursor = rows - 1
	}
	if m.procCursor < 0 {
		m.procCursor = 0
	}
	body := m.procBody()
	if m.procCursor < m.procScroll {
		m.procScroll = m.procCursor
	}
	if m.procCursor >= m.procScroll+body {
		m.procScroll = m.procCursor - body + 1
	}
	maxScroll := rows - body
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.procScroll > maxScroll {
		m.procScroll = maxScroll
	}
	if m.procScroll < 0 {
		m.procScroll = 0
	}
}

// recalcViewport adjusts viewport height when debug panel size changes.
func (m *Model) recalcViewport() {
	if !m.ready {
		return
	}
	tabBarHeight := 3
	gutterHeight := 0
	if m.statusData != nil {
		gutterHeight = 1
	}
	debugHeight := 0
	if m.debug != nil {
		debugHeight = m.debug.Height()
	}
	helpHeight := 2
	contentHeight := m.height - tabBarHeight - gutterHeight - debugHeight - helpHeight
	if contentHeight < 1 {
		contentHeight = 1
	}
	m.viewport.Height = contentHeight
	m.setProcTableHeight(contentHeight)
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func viewMetric(v view) string {
	switch v {
	case viewCPU:
		return "cpu"
	case viewMemory:
		return "memory"
	case viewDisk:
		return "disk"
	case viewNetwork:
		return "network"
	case viewHardware:
		return "hardware" // placeholder; overridden in fetchHistoryCmd
	case viewProcess:
		return "process"
	default:
		return ""
	}
}

// processTopNRefreshInterval controls how often the process view runs a full
// Top-N query to potentially update which processes are shown. Between full
// refreshes, incremental by-name fetches keep the chart updated cheaply.
const processTopNRefreshInterval = 60 * time.Second

// fetchHistoryCmd returns a tea.Cmd that fetches history data asynchronously.
// The result is delivered as a historyResultMsg. If the current view also has
// pinned process data, a pinnedHistoryResultMsg follows automatically.
//
// When a valid cache exists for the current view, the fetch is incremental:
// only the tail window is requested and merged into the cache.
//
// For the process view, a full Top-N query runs on first load and then
// periodically (every processTopNRefreshInterval). Between full refreshes,
// incremental ticks use GetHistoryByName with the cached process names,
// avoiding the expensive Top-N query.
func (m *Model) fetchHistoryCmd() tea.Cmd {
	v := m.current
	// Services history is keyed by source+metric (not a single metric string),
	// so it has its own command and skips the metric-string machinery below.
	if v == viewServices {
		return m.fetchCustomHistoryCmd()
	}
	metric := viewMetric(v)
	if metric == "" {
		return nil
	}
	// Hardware view: resolve to temp/power based on active sub-section.
	// ECC has no history chart.
	if v == viewHardware {
		switch m.hardwareSection {
		case hwSectionTemp:
			metric = "temperature"
		case hwSectionPower:
			metric = "power"
		case hwSectionGPU:
			metric = "gpu"
		default:
			return nil // ECC has no history
		}
	}
	var start, end time.Time
	isCustom := m.customStart != nil && m.customEnd != nil
	if isCustom {
		start = *m.customStart
		end = *m.customEnd
	} else {
		end = time.Now()
		start = end.Add(-m.historyRanges[m.historyRange].Duration)
	}

	// Check if we can do an incremental (tail-only) fetch.
	cache := m.historyCache[v]
	canIncremental := cache != nil && !isCustom && !cache.customRange &&
		cache.rangeIndex == m.historyRange &&
		!cache.lastFetchEnd.IsZero() &&
		len(cache.series) > 0

	if canIncremental {
		rangeDur := m.historyRanges[m.historyRange].Duration
		bucket := bucketDuration(rangeDur)
		fetchStart := cache.lastFetchEnd.Add(-2 * bucket)
		if fetchStart.Before(start) {
			fetchStart = start
		}
		windowStart := start

		// Process view: use by-name fetch with cached Top-N names between
		// full refreshes. Fall through to full fetch when it's time to
		// re-evaluate which processes are in the Top N.
		if v == viewProcess && len(cache.topNNames) > 0 &&
			time.Since(cache.topNLastFullAt) < processTopNRefreshInterval {
			names := cache.topNNames
			client := m.client
			return func() tea.Msg {
				t := time.Now()
				series, err := client.GetHistoryByName("process", fetchStart, end, names)
				d := time.Since(t)
				if errors.Is(err, ErrNotModified) || err != nil {
					return historyResultMsg{forView: v, err: err, start: start, end: end, duration: d, incremental: true, windowStart: windowStart}
				}
				return historyResultMsg{forView: v, series: series, start: start, end: end, duration: d, incremental: true, windowStart: windowStart}
			}
		}

		// Non-process views: incremental tail fetch via GetHistory.
		if v != viewProcess {
			client := m.client
			return func() tea.Msg {
				t := time.Now()
				series, err := client.GetHistory(metric, fetchStart, end)
				d := time.Since(t)
				if errors.Is(err, ErrNotModified) || err != nil {
					return historyResultMsg{forView: v, err: err, start: start, end: end, duration: d, incremental: true, windowStart: windowStart}
				}
				return historyResultMsg{forView: v, series: series, start: start, end: end, duration: d, incremental: true, windowStart: windowStart}
			}
		}
	}

	// Full fetch (first load, range change, process Top-N refresh, custom dates).
	client := m.client
	return func() tea.Msg {
		t := time.Now()
		series, err := client.GetHistory(metric, start, end)
		d := time.Since(t)
		if errors.Is(err, ErrNotModified) || err != nil {
			return historyResultMsg{forView: v, err: err, start: start, end: end, duration: d}
		}
		return historyResultMsg{forView: v, series: series, start: start, end: end, duration: d}
	}
}

// fetchCustomHistoryCmd fetches the history series for the currently-selected
// custom source/metric on the Services tab. Always a full fetch (no incremental
// tail merge) — custom series are low-cardinality and cheap to re-query.
func (m *Model) fetchCustomHistoryCmd() tea.Cmd {
	source, metric, _ := m.selectedCustomSeries()
	if source == "" || metric == "" {
		return nil
	}
	var start, end time.Time
	if m.customStart != nil && m.customEnd != nil {
		start, end = *m.customStart, *m.customEnd
	} else {
		end = time.Now()
		start = end.Add(-m.historyRanges[m.historyRange].Duration)
	}
	client := m.client
	return func() tea.Msg {
		t := time.Now()
		series, err := client.GetCustomHistory(source, metric, start, end)
		d := time.Since(t)
		if errors.Is(err, ErrNotModified) || err != nil {
			return historyResultMsg{forView: viewServices, err: err, start: start, end: end, duration: d}
		}
		return historyResultMsg{forView: viewServices, series: series, start: start, end: end, duration: d}
	}
}

// fetchPinnedHistoryCmd returns a tea.Cmd that fetches pinned process history.
// When a prior pinned fetch exists with the same set of names and range, it
// uses an incremental tail fetch (same approach as fetchHistoryCmd).
func (m *Model) fetchPinnedHistoryCmd(start, end time.Time) tea.Cmd {
	if len(m.pinnedProcesses) == 0 {
		return nil
	}
	names := make([]string, 0, len(m.pinnedProcesses))
	for name := range m.pinnedProcesses {
		names = append(names, name)
	}
	sort.Strings(names)

	// Check if incremental fetch is possible: same pinned names, have prior data.
	isCustom := m.customStart != nil && m.customEnd != nil
	canIncremental := !isCustom &&
		len(m.procPinnedSeries) > 0 &&
		!m.procPinnedLastFetchEnd.IsZero() &&
		pinnedNamesEqual(names, m.procPinnedNames)

	client := m.client

	if canIncremental {
		rangeDur := end.Sub(start)
		bucket := bucketDuration(rangeDur)
		fetchStart := m.procPinnedLastFetchEnd.Add(-2 * bucket)
		if fetchStart.Before(start) {
			fetchStart = start
		}
		windowStart := start
		return func() tea.Msg {
			t := time.Now()
			series, err := client.GetHistoryByName("process", fetchStart, end, names)
			d := time.Since(t)
			if errors.Is(err, ErrNotModified) || err != nil {
				return pinnedHistoryResultMsg{err: err, start: start, end: end, duration: d, incremental: true, windowStart: windowStart}
			}
			return pinnedHistoryResultMsg{series: series, start: start, end: end, duration: d, incremental: true, windowStart: windowStart}
		}
	}

	return func() tea.Msg {
		t := time.Now()
		series, err := client.GetHistoryByName("process", start, end, names)
		d := time.Since(t)
		if errors.Is(err, ErrNotModified) || err != nil {
			return pinnedHistoryResultMsg{err: err, start: start, end: end, duration: d}
		}
		return pinnedHistoryResultMsg{series: series, start: start, end: end, duration: d}
	}
}

// prefetchHistoryForCmd returns a tea.Cmd that fetches history for a single
// view asynchronously, delivering the result as a prefetchHistoryResultMsg.
func (m *Model) prefetchHistoryForCmd(v view) tea.Cmd {
	metric := viewMetric(v)
	if metric == "" {
		return nil
	}
	// Hardware view: prefetch based on active sub-section
	if v == viewHardware {
		switch m.hardwareSection {
		case hwSectionTemp:
			metric = "temperature"
		case hwSectionPower:
			metric = "power"
		case hwSectionGPU:
			metric = "gpu"
		default:
			return nil
		}
	}
	end := time.Now()
	start := end.Add(-m.historyRanges[m.historyRange].Duration)
	client := m.client
	return func() tea.Msg {
		t := time.Now()
		series, err := client.GetHistory(metric, start, end)
		d := time.Since(t)
		return prefetchHistoryResultMsg{
			forView:  v,
			series:   series,
			start:    start,
			end:      end,
			err:      err,
			duration: d,
		}
	}
}

func (m *Model) renderHistoryCacheEntry(v view) {
	entry := m.historyCache[v]
	if entry == nil || len(entry.series) == 0 {
		return
	}
	chartWidth := m.width - 4
	ch := chartHeightForTerminal(m.height)
	rangeLabel := m.historyRangeLabel()
	switch v {
	case viewProcess:
		entry.chart = renderProcessHistoryChart(entry.series, chartWidth, ch, entry.start, entry.end, m.pinnedProcesses)
		entry.chart = renderPanel(fmt.Sprintf("Process CPU History [%s]", rangeLabel), entry.chart+historyHelpInline(rangeLabel), m.width)
	case viewCPU:
		entry.chart = renderPercentChart(entry.series, chartWidth, ch, entry.start, entry.end)
		entry.chart = renderPanel(fmt.Sprintf("CPU History [%s]", rangeLabel), entry.chart, m.width)
	case viewMemory:
		entry.chart = renderPercentChart(entry.series, chartWidth, ch, entry.start, entry.end)
		entry.chart = renderPanel(fmt.Sprintf("Memory History [%s]", rangeLabel), entry.chart, m.width)
	case viewDisk:
		entry.chart = renderPercentChart(entry.series, chartWidth, ch, entry.start, entry.end)
		entry.chart = renderPanel(fmt.Sprintf("Disk History [%s]", rangeLabel), entry.chart, m.width)
	case viewNetwork:
		netFiltered := entry.series
		if m.netSelected != nil {
			netFiltered = nil
			for _, s := range entry.series {
				ifaceName := strings.TrimSuffix(strings.TrimSuffix(s.Label, "_rx"), "_tx")
				if m.netSelected[ifaceName] {
					netFiltered = append(netFiltered, s)
				}
			}
		}
		entry.chart = renderNetHistoryChart(netFiltered, chartWidth, ch, entry.start, entry.end, m.netDisplayBits)
		entry.chart = renderPanel(fmt.Sprintf("Network History [%s]", rangeLabel), entry.chart+historyHelpInline(rangeLabel), m.width)
	case viewHardware:
		switch m.hardwareSection {
		case hwSectionTemp:
			tempFiltered := entry.series
			if m.tempSelected != nil {
				tempFiltered = nil
				for _, s := range entry.series {
					if m.tempSelected[s.Label] {
						tempFiltered = append(tempFiltered, s)
					}
				}
			}
			entry.chart = renderTempHistoryChart(tempFiltered, chartWidth, ch, entry.start, entry.end)
			entry.chart = renderPanel(fmt.Sprintf("Temperature History [%s]", rangeLabel), entry.chart+historyHelpInline(rangeLabel), m.width)
		case hwSectionPower:
			powerFiltered := entry.series
			if m.powerSelected != nil {
				powerFiltered = nil
				for _, s := range entry.series {
					if m.powerSelected[s.Label] {
						powerFiltered = append(powerFiltered, s)
					}
				}
			}
			entry.chart = renderPowerHistoryChart(powerFiltered, chartWidth, ch, entry.start, entry.end)
			entry.chart = renderPanel(fmt.Sprintf("Power History [%s]", rangeLabel), entry.chart+historyHelpInline(rangeLabel), m.width)
		case hwSectionGPU:
			gpuFiltered := entry.series
			if m.gpuSelected != nil {
				gpuFiltered = nil
				for _, s := range entry.series {
					if m.gpuSelected[s.Label] {
						gpuFiltered = append(gpuFiltered, s)
					}
				}
			}
			entry.chart = renderGPUHistoryChart(gpuFiltered, chartWidth, ch, entry.start, entry.end)
			entry.chart = renderPanel(fmt.Sprintf("GPU History [%s]", rangeLabel), entry.chart+historyHelpInline(rangeLabel), m.width)
		}
	}
}

// prefetchAllHistoryCmds returns tea.Cmds that fetch history for all views
// concurrently in the background. Results arrive as prefetchHistoryResultMsg.
func (m *Model) prefetchAllHistoryCmds() []tea.Cmd {
	m.historyCache = make(map[view]*viewHistoryCache)
	views := []view{viewCPU, viewMemory, viewDisk, viewNetwork, viewHardware, viewProcess}
	m.d("prefetch: %d views (async)", len(views))
	cmds := make([]tea.Cmd, 0, len(views))
	for _, v := range views {
		m.historyFetching[v] = true
		cmds = append(cmds, m.prefetchHistoryForCmd(v))
	}
	return cmds
}

func (m *Model) historyRangeLabel() string {
	if m.customStart != nil && m.customEnd != nil {
		return formatTimeRange(*m.customStart, *m.customEnd)
	}
	return m.historyRanges[m.historyRange].Label
}

func formatTimeRange(s, e time.Time) string {
	dur := e.Sub(s)
	sameDay := s.Year() == e.Year() && s.YearDay() == e.YearDay()
	switch {
	case sameDay:
		// e.g. "Feb 02 15:00 – 16:30"
		return fmt.Sprintf("%s %s – %s", s.Format("Jan 02"), s.Format("15:04"), e.Format("15:04"))
	case dur < 24*time.Hour:
		// Spans midnight but less than a day, e.g. "Feb 01 23:00 – Feb 02 01:00"
		return fmt.Sprintf("%s – %s", s.Format("Jan 02 15:04"), e.Format("Jan 02 15:04"))
	case s.Year() == e.Year():
		return fmt.Sprintf("%s – %s", s.Format("Jan 02"), e.Format("Jan 02"))
	default:
		return fmt.Sprintf("%s – %s", s.Format("Jan 02 '06"), e.Format("Jan 02 '06"))
	}
}

func (m *Model) historyTimeRange() (time.Time, time.Time) {
	// Return the actual time range used when fetching history data,
	// not a newly computed range, to avoid chart/data mismatch
	return m.historyStart, m.historyEnd
}

func (m *Model) regenerateHistoryChart() {
	if len(m.historySeries) == 0 {
		m.cachedHistoryCharts[m.current] = ""
		return
	}
	chartWidth := m.width - 4 // panel border (2) + padding (2)
	ch := chartHeightForTerminal(m.height)
	rangeLabel := m.historyRangeLabel()
	switch m.current {
	case viewProcess:
		topCPUChart := renderProcessHistoryChart(m.historySeries, chartWidth, ch, m.historyStart, m.historyEnd, m.pinnedProcesses)
		topCPUChart = renderPanel(fmt.Sprintf("Process CPU History [%s]", rangeLabel), topCPUChart+historyHelpInline(rangeLabel), m.width)
		m.cachedHistoryCharts[m.current] = topCPUChart
		// Always sync per-view cache with the top CPU chart (create if needed).
		// switchView() will overlay the pinned chart when appropriate.
		if m.historyCache == nil {
			m.historyCache = make(map[view]*viewHistoryCache)
		}
		if entry, ok := m.historyCache[m.current]; ok {
			entry.chart = topCPUChart
		} else {
			m.historyCache[m.current] = &viewHistoryCache{
				series:       m.historySeries,
				chart:        topCPUChart,
				start:        m.historyStart,
				end:          m.historyEnd,
				lastFetchEnd: m.historyEnd,
				rangeIndex:   m.historyRange,
				customRange:  m.customStart != nil && m.customEnd != nil,
			}
		}
		return
	case viewCPU:
		m.cachedHistoryCharts[m.current] = renderPercentChart(m.historySeries, chartWidth, ch, m.historyStart, m.historyEnd)
		m.cachedHistoryCharts[m.current] = renderPanel(fmt.Sprintf("CPU History [%s]", rangeLabel), m.cachedHistoryCharts[m.current], m.width)
	case viewMemory:
		m.cachedHistoryCharts[m.current] = renderPercentChart(m.historySeries, chartWidth, ch, m.historyStart, m.historyEnd)
		m.cachedHistoryCharts[m.current] = renderPanel(fmt.Sprintf("Memory History [%s]", rangeLabel), m.cachedHistoryCharts[m.current], m.width)
	case viewDisk:
		m.cachedHistoryCharts[m.current] = renderPercentChart(m.historySeries, chartWidth, ch, m.historyStart, m.historyEnd)
		m.cachedHistoryCharts[m.current] = renderPanel(fmt.Sprintf("Disk History [%s]", rangeLabel), m.cachedHistoryCharts[m.current], m.width)
	case viewNetwork:
		// Filter to only selected interfaces
		var netFiltered []api.TimeSeries
		for _, s := range m.historySeries {
			// Labels are "iface_rx" or "iface_tx" - extract interface name
			ifaceName := strings.TrimSuffix(strings.TrimSuffix(s.Label, "_rx"), "_tx")
			if m.netSelected[ifaceName] {
				netFiltered = append(netFiltered, s)
			}
		}
		m.cachedHistoryCharts[m.current] = renderNetHistoryChart(netFiltered, chartWidth, ch, m.historyStart, m.historyEnd, m.netDisplayBits)
		m.cachedHistoryCharts[m.current] = renderPanel(fmt.Sprintf("Network History [%s]", rangeLabel), m.cachedHistoryCharts[m.current]+historyHelpInline(rangeLabel), m.width)
	case viewHardware:
		switch m.hardwareSection {
		case hwSectionTemp:
			var tempFiltered []api.TimeSeries
			for _, s := range m.historySeries {
				if m.tempSelected[s.Label] {
					tempFiltered = append(tempFiltered, s)
				}
			}
			m.cachedHistoryCharts[m.current] = renderTempHistoryChart(tempFiltered, chartWidth, ch, m.historyStart, m.historyEnd)
			m.cachedHistoryCharts[m.current] = renderPanel(fmt.Sprintf("Temperature History [%s]", rangeLabel), m.cachedHistoryCharts[m.current]+historyHelpInline(rangeLabel), m.width)
		case hwSectionPower:
			var powerFiltered []api.TimeSeries
			for _, s := range m.historySeries {
				if m.powerSelected[s.Label] {
					powerFiltered = append(powerFiltered, s)
				}
			}
			m.cachedHistoryCharts[m.current] = renderPowerHistoryChart(powerFiltered, chartWidth, ch, m.historyStart, m.historyEnd)
			m.cachedHistoryCharts[m.current] = renderPanel(fmt.Sprintf("Power History [%s]", rangeLabel), m.cachedHistoryCharts[m.current]+historyHelpInline(rangeLabel), m.width)
		case hwSectionGPU:
			var gpuFiltered []api.TimeSeries
			for _, s := range m.historySeries {
				if m.gpuSelected[s.Label] {
					gpuFiltered = append(gpuFiltered, s)
				}
			}
			m.cachedHistoryCharts[m.current] = renderGPUHistoryChart(gpuFiltered, chartWidth, ch, m.historyStart, m.historyEnd)
			m.cachedHistoryCharts[m.current] = renderPanel(fmt.Sprintf("GPU History [%s]", rangeLabel), m.cachedHistoryCharts[m.current]+historyHelpInline(rangeLabel), m.width)
		default:
			m.cachedHistoryCharts[m.current] = ""
		}
	case viewServices:
		source, metric, unit := m.selectedCustomSeries()
		title := "Service History"
		if source != "" {
			title = fmt.Sprintf("%s · %s History [%s]", source, metric, rangeLabel)
		}
		chart := renderCustomHistoryChart(m.historySeries, chartWidth, ch, m.historyStart, m.historyEnd, unit)
		m.cachedHistoryCharts[m.current] = renderPanel(title, chart+historyHelpInline(rangeLabel), m.width)
	default:
		m.cachedHistoryCharts[m.current] = ""
	}
	// Sync per-view cache
	if m.historyCache != nil {
		if entry, ok := m.historyCache[m.current]; ok {
			entry.chart = m.cachedHistoryCharts[m.current]
		}
	}
}

func (m *Model) hasHistory() bool {
	switch m.current {
	case viewCPU, viewMemory, viewDisk, viewNetwork, viewHardware, viewProcess:
		return true
	case viewServices:
		// Only metric-bearing sources have a chart; a status-only source has no
		// range controls (and no stuck history fetch).
		src := m.activeCustomSource()
		return src != nil && len(src.Metrics) > 0
	}
	return false
}

// activeCustomSource returns the catalog entry for the active Services sub-section.
func (m *Model) activeCustomSource() *api.CustomSourceInfo {
	if m.servicesSection < 0 || m.servicesSection >= len(m.customSources) {
		return nil
	}
	return &m.customSources[m.servicesSection]
}

// selectedCustomSeries returns the source, metric name and unit currently
// selected for charting on the Services tab.
func (m *Model) selectedCustomSeries() (source, metric, unit string) {
	src := m.activeCustomSource()
	if src == nil || len(src.Metrics) == 0 {
		return "", "", ""
	}
	idx := m.servicesMetricCursor
	if idx < 0 || idx >= len(src.Metrics) {
		idx = 0
	}
	f := src.Metrics[idx]
	return src.Name, f.Name, f.Unit
}

// bucketDuration returns the aggregation bucket size for a given time range,
// matching the server's bucketInterval() logic in internal/api/history.go.
func bucketDuration(d time.Duration) time.Duration {
	switch {
	case d <= time.Hour:
		return time.Minute
	case d <= 24*time.Hour:
		return 10 * time.Minute
	case d <= 7*24*time.Hour:
		return time.Hour
	default:
		return 6 * time.Hour
	}
}

// mergeSeriesIncremental merges a narrow-window (tail) response into existing
// cached series. It trims points older than windowStart, replaces existing
// points at matching timestamps, and appends new points. Returns the merged
// series and whether any values actually changed.
func mergeSeriesIncremental(cached, incremental []api.TimeSeries, windowStart time.Time) ([]api.TimeSeries, bool) {
	changed := false
	windowStartNS := windowStart.UnixNano()

	// Build label→index lookup for cached series.
	cachedIdx := make(map[string]int, len(cached))
	for i, s := range cached {
		cachedIdx[s.Label] = i
	}

	for _, inc := range incremental {
		ci, exists := cachedIdx[inc.Label]
		if !exists {
			// New series label appeared — append it.
			cached = append(cached, inc)
			changed = true
			continue
		}
		cs := &cached[ci]

		// Trim points that fell off the left edge of the rolling window.
		trimIdx := 0
		for trimIdx < len(cs.Points) && cs.Points[trimIdx].TimestampNS < windowStartNS {
			trimIdx++
		}
		if trimIdx > 0 {
			cs.Points = cs.Points[trimIdx:]
			changed = true
		}

		// Build index of existing timestamps for O(1) lookup.
		existingTS := make(map[int64]int, len(cs.Points))
		for j, p := range cs.Points {
			existingTS[p.TimestampNS] = j
		}

		// Merge incremental points: update existing or append new.
		for _, p := range inc.Points {
			if j, ok := existingTS[p.TimestampNS]; ok {
				if cs.Points[j].Value != p.Value {
					cs.Points[j].Value = p.Value
					changed = true
				}
			} else if p.TimestampNS >= windowStartNS {
				cs.Points = append(cs.Points, p)
				changed = true
			}
		}
	}

	return cached, changed
}

// pinnedNamesEqual returns true if two sorted name slices are identical.
func pinnedNamesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (m *Model) switchView(v view) tea.Cmd {
	if m.current == viewAlerts && v != viewAlerts {
		m.notifyLog = nil
		m.alertConfirmDelete = false
		m.alertConfirmName = ""
		m.alertFormErr = ""
	}
	if m.current == viewProcess && v != viewProcess {
		m.procSearchActive = false
		m.procSearchQuery = ""
	}
	prev := m.current
	m.current = v
	var cmd tea.Cmd
	// Services drives its own (source+metric) history fetch in the refresh switch
	// below, outside the generic per-view cache/prefetch machinery.
	if m.hasHistory() && v != viewServices {
		if cached, ok := m.historyCache[v]; ok && cached != nil {
			m.d("view: %s→%s (cache hit, %d series)", viewName(prev), viewName(v), len(cached.series))
			m.historySeries = cached.series
			m.historyStart = cached.start
			m.historyEnd = cached.end
			m.cachedHistoryCharts[m.current] = cached.chart
		} else if !m.historyFetching[v] {
			m.d("view: %s→%s (cache miss, async fetch)", viewName(prev), viewName(v))
			m.historyFetching[v] = true
			cmd = m.prefetchHistoryForCmd(v)
		} else {
			m.d("view: %s→%s (cache miss, fetch in flight)", viewName(prev), viewName(v))
		}
	} else {
		m.d("view: %s→%s", viewName(prev), viewName(v))
	}
	// Refresh cached data for the new view
	switch v {
	case viewDashboard:
		m.refreshDashData()
		if !m.dashSparkInited {
			m.initDashSparklines()
		}
	case viewCPU:
		m.refreshCPUData()
	case viewMemory:
		m.refreshMemData()
	case viewDisk:
		m.refreshDiskData()
	case viewNetwork:
		m.refreshNetData()
		if !m.netSparkInited {
			m.initNetSparklines()
		}
	case viewHardware:
		m.refreshTempData()
		m.refreshPowerData()
		m.refreshGPUData()
		m.refreshMemData() // for ECC
		if !m.tempSparkInited {
			m.initTempSparklines()
		}
		if !m.powerSparkInited {
			m.initPowerSparklines()
		}
		if !m.gpuSparkInited {
			m.initGPUSparklines()
		}
	case viewProcess:
		m.refreshProcessData()
	case viewAlerts:
		m.refreshAlertRules()
		m.refreshAlertsData()
	case viewServices:
		m.refreshCustomData()
		cmd = m.fetchCustomHistoryCmd()
	}
	if m.ready {
		m.viewport.GotoTop()
	}
	return cmd
}

func testAlertFromRule(rules []api.AlertRuleMetric, cursor int) TestNotificationAlert {
	if cursor >= len(rules) {
		return TestNotificationAlert{
			RuleName: "test",
			Severity: "info",
			Message:  "Test notification from bewitch",
		}
	}
	r := rules[cursor]
	var msg string
	switch r.Type {
	case "threshold":
		msg = fmt.Sprintf("%s %.1f %s %.1f for %s", r.Metric, r.Value, r.Operator, r.Value, r.Duration)
	case "predictive":
		msg = fmt.Sprintf("%s on %s predicted to reach %.0f%% in %d hours", r.Metric, r.Mount, r.ThresholdPct, r.PredictHours)
	case "variance":
		msg = fmt.Sprintf("memory variance: changes exceeding %.1f%% in %s", r.Value, r.Duration)
	default:
		msg = fmt.Sprintf("%s %s alert triggered", r.Metric, r.Type)
	}
	return TestNotificationAlert{
		RuleName: r.Name,
		Severity: r.Severity,
		Message:  msg,
	}
}

func (m *Model) refreshAlertRules() {
	t := time.Now()
	rules, err := m.client.GetAlertRules()
	if errors.Is(err, ErrNotModified) {
		m.d("refresh: alertRules 304 (%s)", time.Since(t))
		return
	}
	if err != nil {
		m.d("refresh: alertRules err=%v (%s)", err, time.Since(t))
		m.alertRules = nil
		return
	}
	m.d("refresh: alertRules (%s)", time.Since(t))
	m.alertRules = rules
}

func (m *Model) refreshProcessData() {
	t := time.Now()
	procs, err := m.client.GetProcesses()
	if errors.Is(err, ErrNotModified) {
		m.d("refresh: process 304 (%s)", time.Since(t))
		return
	}
	if err != nil {
		m.d("refresh: process err=%v (%s)", err, time.Since(t))
		m.procData = nil
		return
	}
	m.d("refresh: process (%s)", time.Since(t))
	m.procData = procs
	m.lastDataChange[viewProcess] = time.Now()
}

func (m *Model) refreshCPUData() {
	t := time.Now()
	cores, err := m.client.GetCPU()
	if errors.Is(err, ErrNotModified) {
		m.d("refresh: cpu 304 (%s)", time.Since(t))
		return
	}
	if err != nil {
		m.d("refresh: cpu err=%v (%s)", err, time.Since(t))
		m.cpuData = nil
		return
	}
	m.d("refresh: cpu (%s)", time.Since(t))
	m.cpuData = cores
	m.lastDataChange[viewCPU] = time.Now()
}

func (m *Model) refreshMemData() {
	t := time.Now()
	mem, err := m.client.GetMemory()
	if errors.Is(err, ErrNotModified) {
		m.d("refresh: memory 304 (%s)", time.Since(t))
		// Still try ECC in case it changed independently
	} else if err != nil {
		m.d("refresh: memory err=%v (%s)", err, time.Since(t))
		m.memData = nil
		return
	} else {
		m.d("refresh: memory (%s)", time.Since(t))
		m.memData = mem
		m.lastDataChange[viewMemory] = time.Now()
	}
	// Also refresh ECC data since it's shown on memory view
	t2 := time.Now()
	ecc, err := m.client.GetECC()
	if err == nil {
		m.d("refresh: ecc (%s)", time.Since(t2))
		m.eccData = ecc
	}
}

func (m *Model) refreshDiskData() {
	t := time.Now()
	disks, err := m.client.GetDisk()
	if errors.Is(err, ErrNotModified) {
		m.d("refresh: disk 304 (%s)", time.Since(t))
		return
	}
	if err != nil {
		m.d("refresh: disk err=%v (%s)", err, time.Since(t))
		m.diskData = nil
		return
	}
	m.d("refresh: disk (%s)", time.Since(t))
	m.diskData = disks
	m.lastDataChange[viewDisk] = time.Now()
}

// detectFormCapabilities reports which hardware-dependent alert categories the
// host can actually satisfy, so the create form doesn't offer (e.g.) SMART alerts
// on a box with no SMART-capable disks. Disk data is loaded at startup, but
// GPU/temperature/ECC are only fetched when the Hardware view is visited — so
// refresh them here, otherwise jumping straight to Alerts and pressing 'n' would
// wrongly hide them. A failed/empty snapshot simply gates the category off, which
// is the safe direction (you can't alert on what the daemon can't see).
func (m *Model) detectFormCapabilities() formCapabilities {
	m.refreshDiskData()
	m.refreshGPUData()
	m.refreshTempData()
	m.refreshMemData() // also fetches the ECC snapshot
	caps := formCapabilities{
		gpu:         len(m.gpuData) > 0,
		temperature: len(m.tempData) > 0,
		ecc:         m.eccData != nil && m.eccData.Available,
	}
	for _, d := range m.diskData {
		if d.SMARTAvailable {
			caps.smart = true
			break
		}
	}
	return caps
}

func (m *Model) refreshNetData() {
	t := time.Now()
	ifaces, err := m.client.GetNetwork()
	if errors.Is(err, ErrNotModified) {
		m.d("refresh: network 304 (%s)", time.Since(t))
		return
	}
	if err != nil {
		m.d("refresh: network err=%v (%s)", err, time.Since(t))
		m.netData = nil
		return
	}
	m.d("refresh: network (%s)", time.Since(t))
	m.netData = ifaces
	m.lastDataChange[viewNetwork] = time.Now()
}

func (m *Model) refreshTempData() {
	t := time.Now()
	temps, err := m.client.GetTemperature()
	if errors.Is(err, ErrNotModified) {
		m.d("refresh: temp 304 (%s)", time.Since(t))
		return
	}
	if err != nil {
		m.d("refresh: temp err=%v (%s)", err, time.Since(t))
		m.tempData = nil
		return
	}
	m.d("refresh: temp (%s)", time.Since(t))
	m.tempData = temps
	m.lastDataChange[viewHardware] = time.Now()
}

func (m *Model) refreshPowerData() {
	t := time.Now()
	zones, err := m.client.GetPower()
	if errors.Is(err, ErrNotModified) {
		m.d("refresh: power 304 (%s)", time.Since(t))
		return
	}
	if err != nil {
		m.d("refresh: power err=%v (%s)", err, time.Since(t))
		m.powerData = nil
		return
	}
	m.d("refresh: power (%s)", time.Since(t))
	m.powerData = zones
	m.lastDataChange[viewHardware] = time.Now()
}

func (m *Model) refreshGPUData() {
	t := time.Now()
	gpus, hints, err := m.client.GetGPU()
	if errors.Is(err, ErrNotModified) {
		m.d("refresh: gpu 304 (%s)", time.Since(t))
		return
	}
	if err != nil {
		m.d("refresh: gpu err=%v (%s)", err, time.Since(t))
		m.gpuData = nil
		return
	}
	m.d("refresh: gpu (%s)", time.Since(t))
	m.gpuData = gpus
	m.gpuHints = hints
	m.lastDataChange[viewHardware] = time.Now()
}

func (m *Model) refreshCustomData() {
	t := time.Now()
	metrics, status, err := m.client.GetCustom()
	if errors.Is(err, ErrNotModified) {
		m.d("refresh: custom 304 (%s)", time.Since(t))
		return
	}
	if err != nil {
		m.d("refresh: custom err=%v (%s)", err, time.Since(t))
		return
	}
	m.d("refresh: custom (%s)", time.Since(t))
	m.customMetrics = metrics
	m.customStatus = status
	m.lastDataChange[viewServices] = time.Now()
}

func (m *Model) refreshAlertsData() {
	t := time.Now()
	alerts, err := m.client.GetAlerts()
	if errors.Is(err, ErrNotModified) {
		m.d("refresh: alerts 304 (%s)", time.Since(t))
		return
	}
	if err != nil {
		m.d("refresh: alerts err=%v (%s)", err, time.Since(t))
		m.alertsData = nil
		return
	}
	m.d("refresh: alerts (%s)", time.Since(t))
	m.alertsData = alerts
	m.lastDataChange[viewAlerts] = time.Now()
}

// refreshActiveAlerts fetches the unacknowledged alerts and caches the count plus the
// highest severity, so the status bar can surface active alerts on every view.
func (m *Model) refreshActiveAlerts() {
	t := time.Now()
	alerts, err := m.client.GetActiveAlerts()
	if errors.Is(err, ErrNotModified) {
		m.d("refresh: active-alerts 304 (%s)", time.Since(t))
		return
	}
	if err != nil {
		m.d("refresh: active-alerts err=%v (%s)", err, time.Since(t))
		m.activeAlerts = 0
		m.activeAlertSeverity = ""
		return
	}
	m.d("refresh: active-alerts n=%d (%s)", len(alerts), time.Since(t))
	m.setActiveAlerts(alerts)
}

// setActiveAlerts caches the count and highest severity from a slice of unacknowledged
// alerts. (When on the Alerts view we pass the already-fetched list, filtered to
// unacknowledged, to avoid a redundant query.)
func (m *Model) setActiveAlerts(alerts []api.AlertMetric) {
	m.activeAlerts = len(alerts)
	m.activeAlertSeverity = ""
	for _, a := range alerts {
		if severityRank(a.Severity) > severityRank(m.activeAlertSeverity) {
			m.activeAlertSeverity = a.Severity
		}
	}
}

// severityRank orders alert severities so the most urgent wins. Unknown/empty rank 0.
func severityRank(s string) int {
	switch s {
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

func (m *Model) refreshDashData() {
	t := time.Now()
	dash, err := m.client.GetDashboard()
	if errors.Is(err, ErrNotModified) {
		m.d("refresh: dashboard 304 (%s)", time.Since(t))
		return
	}
	if err != nil {
		m.d("refresh: dashboard err=%v (%s)", err, time.Since(t))
		m.dashData = nil
		return
	}
	m.d("refresh: dashboard (%s)", time.Since(t))
	m.dashData = dash
	m.lastDataChange[viewDashboard] = time.Now()
}

func (m *Model) initTempSparklines() {
	m.tempSparkData = make(map[string][]float64)
	end := time.Now()
	start := end.Add(-time.Hour)
	series, err := m.client.GetHistory("temperature", start, end)
	if err != nil {
		return
	}
	for _, s := range series {
		vals := make([]float64, len(s.Points))
		for i, p := range s.Points {
			vals[i] = p.Value
		}
		m.tempSparkData[s.Label] = vals
	}
	// Initialize sensor selection: load saved prefs or default all selected
	if m.tempSelected == nil {
		m.tempSelected = make(map[string]bool)
		temps, err := m.client.GetTemperature()
		if err == nil {
			m.tempSensorNames = make([]string, len(temps))
			for i, t := range temps {
				m.tempSensorNames[i] = t.Sensor
				m.tempSelected[t.Sensor] = true
			}
		}
		// Load saved selection from preferences
		if prefs, err := m.client.GetPreferences(); err == nil {
			if saved, ok := prefs["temp_selected_sensors"]; ok {
				// Clear defaults, apply saved selection
				for k := range m.tempSelected {
					m.tempSelected[k] = false
				}
				for _, part := range strings.Split(saved, "\x1f") {
					if part == "" {
						continue
					}
					if idx := strings.LastIndex(part, ":"); idx != -1 && idx < len(part)-1 {
						name := part[:idx]
						if _, exists := m.tempSelected[name]; exists {
							m.tempSelected[name] = part[idx+1:] == "1"
						}
					} else {
						// Legacy format
						if _, exists := m.tempSelected[part]; exists {
							m.tempSelected[part] = true
						}
					}
				}
			}
		}
	}
	m.tempSparkInited = true
}

func (m *Model) saveTempSelection() {
	var parts []string
	seen := make(map[string]bool, len(m.tempSensorNames))
	for _, name := range m.tempSensorNames {
		seen[name] = true
		if m.tempSelected[name] {
			parts = append(parts, name+":1")
		} else {
			parts = append(parts, name+":0")
		}
	}
	for name, selected := range m.tempSelected {
		if !seen[name] {
			if selected {
				parts = append(parts, name+":1")
			} else {
				parts = append(parts, name+":0")
			}
		}
	}
	go m.client.SetPreference("temp_selected_sensors", strings.Join(parts, "\x1f"))
}

func (m *Model) updateTempSparklines(temps []api.TemperatureMetric) {
	if temps == nil {
		return
	}
	if m.tempSparkData == nil {
		m.tempSparkData = make(map[string][]float64)
	}
	if m.tempSelected == nil {
		m.tempSelected = make(map[string]bool)
	}
	// Update sensor names list and ensure new sensors get selected by default
	names := make([]string, len(temps))
	for i, t := range temps {
		names[i] = t.Sensor
		if _, exists := m.tempSelected[t.Sensor]; !exists {
			m.tempSelected[t.Sensor] = true
		}
	}
	m.tempSensorNames = names

	const maxPoints = 60
	for _, t := range temps {
		vals := m.tempSparkData[t.Sensor]
		vals = append(vals, t.TempCelsius)
		if len(vals) > maxPoints {
			vals = vals[len(vals)-maxPoints:]
		}
		m.tempSparkData[t.Sensor] = vals
	}
}

func (m *Model) initDashSparklines() {
	m.dashSparkData = make(map[string][]float64)
	end := time.Now()
	start := end.Add(-time.Hour)
	// CPU history
	if series, err := m.client.GetHistory("cpu", start, end); err == nil {
		for _, s := range series {
			if s.Label == "cpu_user" {
				vals := make([]float64, len(s.Points))
				for i, p := range s.Points {
					vals[i] = p.Value
				}
				m.dashSparkData["cpu"] = vals
			}
		}
	}
	// Memory history
	if series, err := m.client.GetHistory("memory", start, end); err == nil {
		for _, s := range series {
			if s.Label == "mem_used_pct" {
				vals := make([]float64, len(s.Points))
				for i, p := range s.Points {
					vals[i] = p.Value
				}
				m.dashSparkData["mem"] = vals
			}
		}
	}
	m.dashSparkInited = true
}

func (m *Model) updateDashSparklines(dash *api.DashboardData) {
	if dash == nil {
		return
	}
	if m.dashSparkData == nil {
		m.dashSparkData = make(map[string][]float64)
	}
	const maxPoints = 30
	// CPU
	cpuPct := 0.0
	for _, c := range dash.CPU {
		if c.Core == -1 {
			cpuPct = 100 - c.IdlePct // true utilization (counts steal/nice/irq/softirq)
			break
		}
	}
	cpuVals := append(m.dashSparkData["cpu"], cpuPct)
	if len(cpuVals) > maxPoints {
		cpuVals = cpuVals[len(cpuVals)-maxPoints:]
	}
	m.dashSparkData["cpu"] = cpuVals
	// Memory
	if dash.Memory != nil && dash.Memory.TotalBytes > 0 {
		memPct := float64(dash.Memory.UsedBytes) / float64(dash.Memory.TotalBytes) * 100
		memVals := append(m.dashSparkData["mem"], memPct)
		if len(memVals) > maxPoints {
			memVals = memVals[len(memVals)-maxPoints:]
		}
		m.dashSparkData["mem"] = memVals
	}
}

// loadSelections loads user preferences for network/temperature/power selections
// without loading sparkline history. Called at startup for dashboard filtering.
func (m *Model) loadSelections() {
	prefs, err := m.client.GetPreferences()
	if err != nil {
		return
	}

	// Network interfaces
	if saved, ok := prefs["net_selected_interfaces"]; ok {
		m.netSelected = make(map[string]bool)
		for _, part := range strings.Split(saved, "\x1f") {
			if part == "" {
				continue
			}
			// New format: "name:1" or "name:0"
			if idx := strings.LastIndex(part, ":"); idx != -1 && idx < len(part)-1 {
				name := part[:idx]
				m.netSelected[name] = part[idx+1:] == "1"
			} else {
				// Legacy format: just the name (was selected)
				m.netSelected[part] = true
			}
		}
	}
	if v, ok := prefs["net_display_bits"]; ok {
		m.netDisplayBits = v == "true"
	}

	// Temperature sensors
	if saved, ok := prefs["temp_selected_sensors"]; ok {
		m.tempSelected = make(map[string]bool)
		for _, part := range strings.Split(saved, "\x1f") {
			if part == "" {
				continue
			}
			if idx := strings.LastIndex(part, ":"); idx != -1 && idx < len(part)-1 {
				name := part[:idx]
				m.tempSelected[name] = part[idx+1:] == "1"
			} else {
				m.tempSelected[part] = true
			}
		}
	}

	// Pinned processes
	if saved, ok := prefs["pinned_processes"]; ok && saved != "" {
		var pins []string
		if json.Unmarshal([]byte(saved), &pins) == nil {
			m.pinnedProcesses = make(map[string]bool, len(pins))
			for _, p := range pins {
				m.pinnedProcesses[p] = true
			}
		}
	}

	// Power zones
	if saved, ok := prefs["power_selected_zones"]; ok {
		m.powerSelected = make(map[string]bool)
		for _, part := range strings.Split(saved, "\x1f") {
			if part == "" {
				continue
			}
			if idx := strings.LastIndex(part, ":"); idx != -1 && idx < len(part)-1 {
				name := part[:idx]
				m.powerSelected[name] = part[idx+1:] == "1"
			} else {
				m.powerSelected[part] = true
			}
		}
	}

	// GPU devices
	if saved, ok := prefs["gpu_selected_devices"]; ok {
		m.gpuSelected = make(map[string]bool)
		for _, part := range strings.Split(saved, "\x1f") {
			if part == "" {
				continue
			}
			if idx := strings.LastIndex(part, ":"); idx != -1 && idx < len(part)-1 {
				name := part[:idx]
				m.gpuSelected[name] = part[idx+1:] == "1"
			} else {
				m.gpuSelected[part] = true
			}
		}
	}

	// Hardware section
	if v, ok := prefs["hardware_section"]; ok {
		switch v {
		case "1":
			m.hardwareSection = hwSectionPower
		case "2":
			m.hardwareSection = hwSectionECC
		case "3":
			m.hardwareSection = hwSectionGPU
		default:
			m.hardwareSection = hwSectionTemp
		}
	}

	// Services sub-section (clamped to the live source count when rendered).
	if v, ok := prefs["services_section"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			m.servicesSection = n
		}
	}
}

// selectedProcess returns the currently selected process after applying filter+sort,
// matching what the user sees on screen (enriched first, then non-enriched).
func (m *Model) selectedProcess() (api.ProcessMetric, bool) {
	if m.procData == nil || len(m.procData.Processes) == 0 {
		return api.ProcessMetric{}, false
	}
	// Same order the table rows are built in, so m.procCursor indexes it directly.
	combined := orderedCombined(m.procData.Processes, m.procSearchQuery, m.pinnedProcesses, m.procPinnedOnly, m.procSortBy)
	idx := m.procCursor
	if idx >= 0 && idx < len(combined) {
		return combined[idx], true
	}
	return api.ProcessMetric{}, false
}

func (m *Model) togglePinnedProcess(name string) {
	if m.pinnedProcesses == nil {
		m.pinnedProcesses = make(map[string]bool)
	}
	if m.pinnedProcesses[name] {
		delete(m.pinnedProcesses, name)
		m.d("unpin: %q (%d pinned)", name, len(m.pinnedProcesses))
	} else {
		m.pinnedProcesses[name] = true
		m.d("pin: %q (%d pinned)", name, len(m.pinnedProcesses))
	}
	m.savePinnedProcesses()
}

func (m *Model) savePinnedProcesses() {
	pins := make([]string, 0, len(m.pinnedProcesses))
	for name := range m.pinnedProcesses {
		pins = append(pins, name)
	}
	data, _ := json.Marshal(pins)
	go m.client.SetPreference("pinned_processes", string(data))
}

func (m *Model) initNetSparklines() {
	m.netSparkData = make(map[string][]float64)
	end := time.Now()
	start := end.Add(-time.Hour)
	series, err := m.client.GetHistory("network", start, end)
	if err != nil {
		return
	}
	for _, s := range series {
		vals := make([]float64, len(s.Points))
		for i, p := range s.Points {
			vals[i] = p.Value
		}
		m.netSparkData[s.Label] = vals
	}
	// Initialize interface selection: load saved prefs or default all selected
	if m.netSelected == nil {
		m.netSelected = make(map[string]bool)
		ifaces, err := m.client.GetNetwork()
		if err == nil {
			m.netIfaceNames = make([]string, len(ifaces))
			for i, n := range ifaces {
				m.netIfaceNames[i] = n.Interface
				m.netSelected[n.Interface] = true
			}
		}
		if prefs, err := m.client.GetPreferences(); err == nil {
			if saved, ok := prefs["net_selected_interfaces"]; ok {
				for k := range m.netSelected {
					m.netSelected[k] = false
				}
				for _, part := range strings.Split(saved, "\x1f") {
					if part == "" {
						continue
					}
					if idx := strings.LastIndex(part, ":"); idx != -1 && idx < len(part)-1 {
						name := part[:idx]
						if _, exists := m.netSelected[name]; exists {
							m.netSelected[name] = part[idx+1:] == "1"
						}
					} else {
						// Legacy format
						if _, exists := m.netSelected[part]; exists {
							m.netSelected[part] = true
						}
					}
				}
			}
			if v, ok := prefs["net_display_bits"]; ok {
				m.netDisplayBits = v == "true"
			}
		}
	}
	m.netSparkInited = true
}

func (m *Model) saveNetSelection() {
	var parts []string
	seen := make(map[string]bool, len(m.netIfaceNames))
	for _, name := range m.netIfaceNames {
		seen[name] = true
		if m.netSelected[name] {
			parts = append(parts, name+":1")
		} else {
			parts = append(parts, name+":0")
		}
	}
	// Preserve selections for interfaces not currently reported by collector
	for name, selected := range m.netSelected {
		if !seen[name] {
			if selected {
				parts = append(parts, name+":1")
			} else {
				parts = append(parts, name+":0")
			}
		}
	}
	go m.client.SetPreference("net_selected_interfaces", strings.Join(parts, "\x1f"))
}

func (m *Model) saveHistoryRange() {
	// Only persist dynamic preset ranges, not fixed custom date ranges
	if m.customStart != nil && m.customEnd != nil {
		return
	}
	go m.client.SetPreference("history_range", m.historyRanges[m.historyRange].Label)
}

func (m *Model) loadHistoryRange() {
	prefs, err := m.client.GetPreferences()
	if err != nil {
		return
	}
	saved, ok := prefs["history_range"]
	if !ok {
		return
	}
	for i, r := range m.historyRanges {
		if r.Label == saved {
			m.historyRange = i
			return
		}
	}
}

// updateVisibleTabs rebuilds the list of visible tabs.
// Hardware tab is always present (contains temp, power, ECC sub-sections).
func (m *Model) updateVisibleTabs() {
	tabs := []view{viewDashboard, viewCPU, viewMemory, viewDisk, viewNetwork, viewHardware, viewProcess, viewAlerts}
	// Services tab is appended only when at least one custom source is configured,
	// kept last so the fixed 1-8 number-key mapping never shifts.
	if len(m.customSources) > 0 {
		tabs = append(tabs, viewServices)
	}
	m.visibleTabs = tabs

	// If current view is no longer visible, switch to dashboard
	if !m.isViewVisible(m.current) {
		m.current = viewDashboard
	}
}

// isViewVisible returns true if the given view is in visibleTabs.
func (m *Model) isViewVisible(v view) bool {
	for _, tab := range m.visibleTabs {
		if tab == v {
			return true
		}
	}
	return false
}

// viewIndex returns the index of the given view in visibleTabs, or -1 if not found.
func (m *Model) viewIndex(v view) int {
	for i, tab := range m.visibleTabs {
		if tab == v {
			return i
		}
	}
	return -1
}

// nextVisibleView returns the next view in the visibleTabs cycle.
func (m *Model) nextVisibleView() view {
	idx := m.viewIndex(m.current)
	if idx < 0 {
		return viewDashboard
	}
	return m.visibleTabs[(idx+1)%len(m.visibleTabs)]
}

// prevVisibleView returns the previous view in the visibleTabs cycle.
func (m *Model) prevVisibleView() view {
	idx := m.viewIndex(m.current)
	if idx < 0 {
		return viewDashboard
	}
	return m.visibleTabs[(idx-1+len(m.visibleTabs))%len(m.visibleTabs)]
}

func (m *Model) updateNetSparklines(ifaces []api.NetworkMetric) {
	if ifaces == nil {
		return
	}
	if m.netSparkData == nil {
		m.netSparkData = make(map[string][]float64)
	}
	if m.netSelected == nil {
		m.netSelected = make(map[string]bool)
	}
	names := make([]string, len(ifaces))
	for i, n := range ifaces {
		names[i] = n.Interface
		if _, exists := m.netSelected[n.Interface]; !exists {
			m.netSelected[n.Interface] = true
		}
	}
	m.netIfaceNames = names

	const maxPoints = 20
	for _, n := range ifaces {
		rxKey := n.Interface + "_rx"
		txKey := n.Interface + "_tx"
		rxVals := m.netSparkData[rxKey]
		txVals := m.netSparkData[txKey]
		rxVals = append(rxVals, n.RxBytesSec)
		txVals = append(txVals, n.TxBytesSec)
		if len(rxVals) > maxPoints {
			rxVals = rxVals[len(rxVals)-maxPoints:]
		}
		if len(txVals) > maxPoints {
			txVals = txVals[len(txVals)-maxPoints:]
		}
		m.netSparkData[rxKey] = rxVals
		m.netSparkData[txKey] = txVals
	}
}

func (m *Model) initPowerSparklines() {
	m.powerSparkData = make(map[string][]float64)
	end := time.Now()
	start := end.Add(-time.Hour)
	series, err := m.client.GetHistory("power", start, end)
	if err != nil {
		return
	}
	for _, s := range series {
		vals := make([]float64, len(s.Points))
		for i, p := range s.Points {
			vals[i] = p.Value
		}
		m.powerSparkData[s.Label] = vals
	}
	if m.powerSelected == nil {
		m.powerSelected = make(map[string]bool)
		zones, err := m.client.GetPower()
		if err == nil {
			m.powerZoneNames = make([]string, len(zones))
			for i, z := range zones {
				m.powerZoneNames[i] = z.Zone
				m.powerSelected[z.Zone] = true
			}
		}
		if prefs, err := m.client.GetPreferences(); err == nil {
			if saved, ok := prefs["power_selected_zones"]; ok {
				for k := range m.powerSelected {
					m.powerSelected[k] = false
				}
				for _, part := range strings.Split(saved, "\x1f") {
					if part == "" {
						continue
					}
					if idx := strings.LastIndex(part, ":"); idx != -1 && idx < len(part)-1 {
						name := part[:idx]
						if _, exists := m.powerSelected[name]; exists {
							m.powerSelected[name] = part[idx+1:] == "1"
						}
					} else {
						// Legacy format
						if _, exists := m.powerSelected[part]; exists {
							m.powerSelected[part] = true
						}
					}
				}
			}
		}
	}
	m.powerSparkInited = true
}

func (m *Model) savePowerSelection() {
	var parts []string
	seen := make(map[string]bool, len(m.powerZoneNames))
	for _, name := range m.powerZoneNames {
		seen[name] = true
		if m.powerSelected[name] {
			parts = append(parts, name+":1")
		} else {
			parts = append(parts, name+":0")
		}
	}
	for name, selected := range m.powerSelected {
		if !seen[name] {
			if selected {
				parts = append(parts, name+":1")
			} else {
				parts = append(parts, name+":0")
			}
		}
	}
	go m.client.SetPreference("power_selected_zones", strings.Join(parts, "\x1f"))
}

func (m *Model) updatePowerSparklines(zones []api.PowerMetric) {
	if zones == nil {
		return
	}
	if m.powerSparkData == nil {
		m.powerSparkData = make(map[string][]float64)
	}
	if m.powerSelected == nil {
		m.powerSelected = make(map[string]bool)
	}
	names := make([]string, len(zones))
	for i, z := range zones {
		names[i] = z.Zone
		if _, exists := m.powerSelected[z.Zone]; !exists {
			m.powerSelected[z.Zone] = true
		}
	}
	m.powerZoneNames = names

	const maxPoints = 60
	for _, z := range zones {
		vals := m.powerSparkData[z.Zone]
		vals = append(vals, z.Watts)
		if len(vals) > maxPoints {
			vals = vals[len(vals)-maxPoints:]
		}
		m.powerSparkData[z.Zone] = vals
	}
}

func (m *Model) initGPUSparklines() {
	m.gpuSparkData = make(map[string][]float64)
	end := time.Now()
	start := end.Add(-time.Hour)
	series, err := m.client.GetHistory("gpu", start, end)
	if err != nil {
		return
	}
	for _, s := range series {
		vals := make([]float64, len(s.Points))
		for i, p := range s.Points {
			vals[i] = p.Value
		}
		m.gpuSparkData[s.Label] = vals
	}
	if m.gpuSelected == nil {
		m.gpuSelected = make(map[string]bool)
		gpus, _, err := m.client.GetGPU()
		if err == nil {
			m.gpuDeviceNames = make([]string, len(gpus))
			for i, g := range gpus {
				m.gpuDeviceNames[i] = g.Name
				m.gpuSelected[g.Name] = true
			}
		}
		if prefs, err := m.client.GetPreferences(); err == nil {
			if saved, ok := prefs["gpu_selected_devices"]; ok {
				for k := range m.gpuSelected {
					m.gpuSelected[k] = false
				}
				for _, part := range strings.Split(saved, "\x1f") {
					if part == "" {
						continue
					}
					if idx := strings.LastIndex(part, ":"); idx != -1 && idx < len(part)-1 {
						name := part[:idx]
						if _, exists := m.gpuSelected[name]; exists {
							m.gpuSelected[name] = part[idx+1:] == "1"
						}
					} else {
						if _, exists := m.gpuSelected[part]; exists {
							m.gpuSelected[part] = true
						}
					}
				}
			}
		}
	}
	m.gpuSparkInited = true
}

func (m *Model) saveGPUSelection() {
	var parts []string
	seen := make(map[string]bool, len(m.gpuDeviceNames))
	for _, name := range m.gpuDeviceNames {
		seen[name] = true
		if m.gpuSelected[name] {
			parts = append(parts, name+":1")
		} else {
			parts = append(parts, name+":0")
		}
	}
	for name, selected := range m.gpuSelected {
		if !seen[name] {
			if selected {
				parts = append(parts, name+":1")
			} else {
				parts = append(parts, name+":0")
			}
		}
	}
	go m.client.SetPreference("gpu_selected_devices", strings.Join(parts, "\x1f"))
}

func (m *Model) updateGPUSparklines(gpus []api.GPUMetric) {
	if gpus == nil {
		return
	}
	if m.gpuSparkData == nil {
		m.gpuSparkData = make(map[string][]float64)
	}
	if m.gpuSelected == nil {
		m.gpuSelected = make(map[string]bool)
	}
	names := make([]string, len(gpus))
	for i, g := range gpus {
		names[i] = g.Name
		if _, exists := m.gpuSelected[g.Name]; !exists {
			m.gpuSelected[g.Name] = true
		}
	}
	m.gpuDeviceNames = names

	const maxPoints = 60
	for _, g := range gpus {
		vals := m.gpuSparkData[g.Name]
		vals = append(vals, g.UtilizationPct)
		if len(vals) > maxPoints {
			vals = vals[len(vals)-maxPoints:]
		}
		m.gpuSparkData[g.Name] = vals
	}
}

// Update is the bubbletea entry point. It delegates to updateModel and then
// refreshes the viewport content exactly once per message. Previously the inner
// handlers set content via scattered SetContent calls AND View() re-rendered the
// whole view on every frame, so each data tick composed the dashboard and sorted
// the full process list twice. Rendering once here removes that double work while
// keeping every path (data ticks, resize, view switch, and interaction keys that
// have no SetContent of their own) covered.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := m.updateModel(msg)
	if m.ready {
		m.viewport.SetContent(m.renderCurrentContent())
	}
	return m, cmd
}

func (m Model) updateModel(msg tea.Msg) (Model, tea.Cmd) {
	// If capture form is active, route ALL messages to it
	if m.captureFormActive && m.captureForm != nil {
		if msg, ok := msg.(tea.KeyMsg); ok && msg.String() == "esc" {
			m.captureFormActive = false
			m.captureForm = nil
			m.captureFormState = nil
			m.capturedContent = ""
			return m, tickCmd(m.interval)
		}
		form, cmd := m.captureForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			m.captureForm = f
			if f.State == huh.StateCompleted {
				m.captureFormActive = false
				state := m.captureFormState
				content := m.capturedContent
				m.captureForm = nil
				m.captureFormState = nil
				m.capturedContent = ""
				settings := m.captureSettings
				return m, func() tea.Msg {
					return runCapture(state, content, settings)
				}
			}
			if f.State == huh.StateAborted {
				m.captureFormActive = false
				m.captureForm = nil
				m.captureFormState = nil
				m.capturedContent = ""
				return m, tickCmd(m.interval)
			}
		}
		return m, cmd
	}

	// If alert form is active, route ALL messages to it (not just KeyMsg)
	if m.alertFormActive && m.alertForm != nil {
		if msg, ok := msg.(tea.KeyMsg); ok && msg.String() == "esc" {
			m.alertFormActive = false
			m.alertForm = nil
			m.alertFormState = nil
			return m, tickCmd(m.interval)
		}
		form, cmd := m.alertForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			m.alertForm = f
			if f.State == huh.StateCompleted {
				m.alertFormActive = false
				rule := m.alertFormState.toAlertRuleMetric()
				var err error
				if rule.ID != 0 {
					err = m.client.UpdateAlertRule(rule)
				} else {
					err = m.client.CreateAlertRule(rule)
				}
				if err != nil {
					m.alertFormErr = err.Error()
					m.d("alert form submit err=%v", err)
				} else {
					m.alertFormErr = ""
				}
				m.alertForm = nil
				m.alertFormState = nil
				m.refreshAlertRules()
				return m, tickCmd(m.interval)
			}
			if f.State == huh.StateAborted {
				m.alertFormActive = false
				m.alertForm = nil
				m.alertFormState = nil
				return m, tickCmd(m.interval)
			}
		}
		return m, cmd
	}

	// If date picker is active, route key messages to it (but let ticks through)
	if m.datePickerActive {
		if msg, ok := msg.(tea.KeyMsg); ok {
			m.datePicker = m.datePicker.update(msg.String())
			if m.datePicker.confirmed {
				m.datePickerActive = false
				if m.datePicker.presetIdx >= 0 {
					// Dynamic preset — rolling window, not fixed
					m.historyRange = m.datePicker.presetIdx
					m.customStart = nil
					m.customEnd = nil
				} else {
					// Fixed custom date range
					start := m.datePicker.startDate
					end := m.datePicker.endDate
					m.customStart = &start
					m.customEnd = &end
				}
				m.historyCache = nil
				m.procPinnedLastFetchEnd = time.Time{}
				m.d("datePicker: range=%s", m.historyRangeLabel())
				m.historyFetching[m.current] = true
				m.saveHistoryRange()
				return m, m.fetchHistoryCmd()
			}
			if m.datePicker.cancelled {
				m.datePickerActive = false
			}
			return m, nil
		}
		// Fall through for non-key messages (ticks, window resize, etc.)
	}

	// Process search mode: route typing to the textinput, keep arrows driving the table,
	// and let non-key messages (ticks, resizes) fall through so live data keeps refreshing.
	if m.procSearchActive && m.current == viewProcess {
		if km, ok := msg.(tea.KeyMsg); ok {
			switch km.String() {
			case "esc":
				m.procSearchActive = false
				m.procSearchInput.Blur()
				m.procSearchInput.SetValue("")
				m.procSearchQuery = ""
				m.procCursor = 0
				return m, nil
			case "enter":
				// Keep the filter active, just leave edit mode.
				m.procSearchActive = false
				m.procSearchInput.Blur()
				return m, nil
			case "up":
				m.moveProcCursor(-1)
				return m, nil
			case "down":
				m.moveProcCursor(1)
				return m, nil
			case "pgup":
				m.moveProcCursor(-m.procBody())
				return m, nil
			case "pgdown":
				m.moveProcCursor(m.procBody())
				return m, nil
			}
			var cmd tea.Cmd
			m.procSearchInput, cmd = m.procSearchInput.Update(km)
			m.procSearchQuery = m.procSearchInput.Value()
			m.procCursor = 0 // filter changed — back to the top
			return m, cmd
		}
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Debug panel controls (when active)
		// { / } = scroll up/down, ( / ) = shrink/grow
		if m.debug != nil {
			switch msg.String() {
			case "{":
				m.debug.ScrollUp()
				return m, nil
			case "}":
				m.debug.ScrollDown()
				return m, nil
			case "(":
				m.debug.Shrink()
				m.recalcViewport()
				return m, nil
			case ")":
				m.debug.Grow()
				m.recalcViewport()
				return m, nil
			}
		}
		// Network view: j/k/up/down navigate interface list, space toggles
		if m.current == viewNetwork {
			// Forward scroll keys to viewport
			switch msg.String() {
			case "pgup", "pgdown", "ctrl+u", "ctrl+d", "home", "end":
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd
			}
			switch msg.String() {
			case "down":
				if len(m.netIfaceNames) > 0 {
					m.netCursor = (m.netCursor + 1) % len(m.netIfaceNames)
				}
				return m, nil
			case "up":
				if len(m.netIfaceNames) > 0 {
					m.netCursor = (m.netCursor - 1 + len(m.netIfaceNames)) % len(m.netIfaceNames)
				}
				return m, nil
			case " ":
				if m.netCursor < len(m.netIfaceNames) {
					name := m.netIfaceNames[m.netCursor]
					m.netSelected[name] = !m.netSelected[name]
				}
				m.saveNetSelection()
				m.regenerateHistoryChart()
				return m, nil
			case "a":
				allSelected := true
				for _, name := range m.netIfaceNames {
					if !m.netSelected[name] {
						allSelected = false
						break
					}
				}
				for _, name := range m.netIfaceNames {
					m.netSelected[name] = !allSelected
				}
				m.saveNetSelection()
				m.regenerateHistoryChart()
				return m, nil
			case "b":
				m.netDisplayBits = !m.netDisplayBits
				v := "false"
				if m.netDisplayBits {
					v = "true"
				}
				go m.client.SetPreference("net_display_bits", v)
				m.regenerateHistoryChart()
				return m, nil
			}
		}
		// Hardware view: tab cycles sub-sections, up/down/space/a control active section
		if m.current == viewHardware {
			// Forward scroll keys to viewport
			switch msg.String() {
			case "pgup", "pgdown", "ctrl+u", "ctrl+d", "home", "end":
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd
			}
			switch msg.String() {
			case "tab":
				m.hardwareSection = (m.hardwareSection + 1) % 4
				m.cachedHistoryCharts[m.current] = ""
				delete(m.historyCache, viewHardware)
				m.historyFetching[viewHardware] = true
				go m.client.SetPreference("hardware_section", fmt.Sprintf("%d", m.hardwareSection))
				return m, m.fetchHistoryCmd()
			case "shift+tab":
				m.hardwareSection = (m.hardwareSection + 3) % 4
				m.cachedHistoryCharts[m.current] = ""
				delete(m.historyCache, viewHardware)
				m.historyFetching[viewHardware] = true
				go m.client.SetPreference("hardware_section", fmt.Sprintf("%d", m.hardwareSection))
				return m, m.fetchHistoryCmd()
			}
			// Delegate to active sub-section
			switch m.hardwareSection {
			case hwSectionTemp:
				switch msg.String() {
				case "down":
					if len(m.tempSensorNames) > 0 {
						m.tempCursor = (m.tempCursor + 1) % len(m.tempSensorNames)
					}
					return m, nil
				case "up":
					if len(m.tempSensorNames) > 0 {
						m.tempCursor = (m.tempCursor - 1 + len(m.tempSensorNames)) % len(m.tempSensorNames)
					}
					return m, nil
				case " ":
					if m.tempCursor < len(m.tempSensorNames) {
						name := m.tempSensorNames[m.tempCursor]
						m.tempSelected[name] = !m.tempSelected[name]
					}
					m.saveTempSelection()
					m.regenerateHistoryChart()
					return m, nil
				case "a":
					allSelected := true
					for _, name := range m.tempSensorNames {
						if !m.tempSelected[name] {
							allSelected = false
							break
						}
					}
					for _, name := range m.tempSensorNames {
						m.tempSelected[name] = !allSelected
					}
					m.saveTempSelection()
					m.regenerateHistoryChart()
					return m, nil
				}
			case hwSectionPower:
				switch msg.String() {
				case "down":
					if len(m.powerZoneNames) > 0 {
						m.powerCursor = (m.powerCursor + 1) % len(m.powerZoneNames)
					}
					return m, nil
				case "up":
					if len(m.powerZoneNames) > 0 {
						m.powerCursor = (m.powerCursor - 1 + len(m.powerZoneNames)) % len(m.powerZoneNames)
					}
					return m, nil
				case " ":
					if m.powerCursor < len(m.powerZoneNames) {
						name := m.powerZoneNames[m.powerCursor]
						m.powerSelected[name] = !m.powerSelected[name]
					}
					m.savePowerSelection()
					m.regenerateHistoryChart()
					return m, nil
				case "a":
					allSelected := true
					for _, name := range m.powerZoneNames {
						if !m.powerSelected[name] {
							allSelected = false
							break
						}
					}
					for _, name := range m.powerZoneNames {
						m.powerSelected[name] = !allSelected
					}
					m.savePowerSelection()
					m.regenerateHistoryChart()
					return m, nil
				}
			case hwSectionGPU:
				switch msg.String() {
				case "down":
					if len(m.gpuDeviceNames) > 0 {
						m.gpuCursor = (m.gpuCursor + 1) % len(m.gpuDeviceNames)
					}
					return m, nil
				case "up":
					if len(m.gpuDeviceNames) > 0 {
						m.gpuCursor = (m.gpuCursor - 1 + len(m.gpuDeviceNames)) % len(m.gpuDeviceNames)
					}
					return m, nil
				case " ":
					if m.gpuCursor < len(m.gpuDeviceNames) {
						name := m.gpuDeviceNames[m.gpuCursor]
						m.gpuSelected[name] = !m.gpuSelected[name]
					}
					m.saveGPUSelection()
					m.regenerateHistoryChart()
					return m, nil
				case "a":
					allSelected := true
					for _, name := range m.gpuDeviceNames {
						if !m.gpuSelected[name] {
							allSelected = false
							break
						}
					}
					for _, name := range m.gpuDeviceNames {
						m.gpuSelected[name] = !allSelected
					}
					m.saveGPUSelection()
					m.regenerateHistoryChart()
					return m, nil
				}
			}
		}
		// Services view: tab cycles source sub-sections; up/down selects which
		// metric the history chart shows.
		if m.current == viewServices {
			switch msg.String() {
			case "pgup", "pgdown", "ctrl+u", "ctrl+d", "home", "end":
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd
			case "tab":
				if len(m.customSources) > 0 {
					m.servicesSection = (m.servicesSection + 1) % len(m.customSources)
					m.servicesMetricCursor = 0
					m.cachedHistoryCharts[m.current] = ""
					go m.client.SetPreference("services_section", fmt.Sprintf("%d", m.servicesSection))
					return m, m.fetchHistoryCmd()
				}
				return m, nil
			case "shift+tab":
				if len(m.customSources) > 0 {
					m.servicesSection = (m.servicesSection - 1 + len(m.customSources)) % len(m.customSources)
					m.servicesMetricCursor = 0
					m.cachedHistoryCharts[m.current] = ""
					go m.client.SetPreference("services_section", fmt.Sprintf("%d", m.servicesSection))
					return m, m.fetchHistoryCmd()
				}
				return m, nil
			case "down", "j":
				if src := m.activeCustomSource(); src != nil && len(src.Metrics) > 0 {
					m.servicesMetricCursor = (m.servicesMetricCursor + 1) % len(src.Metrics)
					m.cachedHistoryCharts[m.current] = ""
					return m, m.fetchHistoryCmd()
				}
				return m, nil
			case "up", "k":
				if src := m.activeCustomSource(); src != nil && len(src.Metrics) > 0 {
					m.servicesMetricCursor = (m.servicesMetricCursor - 1 + len(src.Metrics)) % len(src.Metrics)
					m.cachedHistoryCharts[m.current] = ""
					return m, m.fetchHistoryCmd()
				}
				return m, nil
			}
		}
		// Process view: arrows/page keys move the table cursor (the window scrolls to
		// follow in renderCurrentContent); letter keys change sort / filter / pin.
		if m.current == viewProcess {
			switch msg.String() {
			case "up", "k":
				m.moveProcCursor(-1)
				return m, nil
			case "down", "j":
				m.moveProcCursor(1)
				return m, nil
			case "pgup", "ctrl+u":
				m.moveProcCursor(-m.procBody())
				return m, nil
			case "pgdown", "ctrl+d":
				m.moveProcCursor(m.procBody())
				return m, nil
			case "home":
				m.procCursor = 0
				return m, nil
			case "end":
				m.procCursor = 1 << 30 // clamped to the last row in renderCurrentContent
				return m, nil
			}
			switch msg.String() {
			case "/":
				m.procSearchActive = true
				m.procSearchInput.SetValue(m.procSearchQuery)
				m.procSearchInput.CursorEnd()
				return m, m.procSearchInput.Focus()
			case "esc":
				// Clear the filter when not in the search box.
				if m.procSearchQuery != "" {
					m.procSearchQuery = ""
					m.procSearchInput.SetValue("")
					m.procCursor = 0
				}
				return m, nil
			case "P":
				m.procPinnedOnly = !m.procPinnedOnly
				m.procCursor = 0
				return m, nil
			case "tab":
				if len(m.pinnedProcesses) > 0 {
					m.procChartPinned = !m.procChartPinned
					m.d("tab: procChartPinned=%v pinnedChart=%d topCPUChart=%d",
						m.procChartPinned, len(m.procPinnedChart), len(m.cachedHistoryCharts[m.current]))
					if m.procChartPinned && m.procPinnedChart == "" {
						// Pinned chart not yet available — kick off async fetch.
						m.d("tab: pinned chart empty, fetching async")
						start, end := m.historyTimeRange()
						return m, m.fetchPinnedHistoryCmd(start, end)
					}
				}
				return m, nil
			case "c":
				m.procSortBy = procSortCPU
				return m, nil
			case "m":
				m.procSortBy = procSortMem
				return m, nil
			case "p":
				m.procSortBy = procSortPID
				return m, nil
			case "n":
				m.procSortBy = procSortName
				return m, nil
			case "t":
				m.procSortBy = procSortThreads
				return m, nil
			case "f":
				m.procSortBy = procSortFDs
				return m, nil
			case "d":
				m.procSortBy = procSortDiskIO
				return m, nil
			case "w":
				m.procSortBy = procSortNet
				return m, nil
			case "*":
				if m.procData != nil && len(m.procData.Processes) > 0 {
					if proc, ok := m.selectedProcess(); ok {
						m.togglePinnedProcess(proc.Name)
					}
				}
				return m, nil
			case "a":
				if selectedProc, ok := m.selectedProcess(); ok {
					m.alertFormState = &alertFormState{
						enabled:     true,
						category:    "process",
						processName: selectedProc.Name,
					}
					// category is preset → the category group is skipped, so caps are unused.
					m.alertForm = buildAlertForm(m.alertFormState, formCapabilities{})
					m.alertFormActive = true
					return m, m.alertForm.Init()
				}
				return m, nil
			}
		}
		// If on alerts view, handle alert-specific keys
		if m.current == viewAlerts {
			// Forward scroll keys to viewport
			switch msg.String() {
			case "pgup", "pgdown", "ctrl+u", "ctrl+d", "home", "end":
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd
			}
			// While a delete confirmation is pending, only 'y' confirms; any other
			// key cancels it and is otherwise ignored.
			if m.alertConfirmDelete && msg.String() != "y" {
				m.alertConfirmDelete = false
				m.alertConfirmName = ""
				return m, nil
			}
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "n":
				m.alertFormErr = ""
				m.alertFormState = &alertFormState{enabled: true}
				m.alertForm = buildAlertForm(m.alertFormState, m.detectFormCapabilities())
				m.alertFormActive = true
				return m, m.alertForm.Init()
			case "e":
				if m.alertFocus == 0 && m.alertRuleCursor < len(m.alertRules) {
					m.alertFormErr = ""
					m.alertFormState = fromAlertRuleMetric(m.alertRules[m.alertRuleCursor])
					// editID set → category group is skipped, so caps are unused.
					m.alertForm = buildAlertForm(m.alertFormState, formCapabilities{})
					m.alertFormActive = true
					return m, m.alertForm.Init()
				}
				return m, nil
			case "d":
				if m.alertFocus == 0 && m.alertRuleCursor < len(m.alertRules) {
					// Require confirmation: a stray 'd' must not silently delete a rule
					// (and, with it, its fired alerts).
					m.alertConfirmDelete = true
					m.alertConfirmName = m.alertRules[m.alertRuleCursor].Name
				}
				return m, nil
			case "y":
				if m.alertConfirmDelete && m.alertFocus == 0 && m.alertRuleCursor < len(m.alertRules) {
					id := m.alertRules[m.alertRuleCursor].ID
					m.alertConfirmDelete = false
					m.alertConfirmName = ""
					// Synchronous (like the ack path) so the refreshes below observe the
					// delete. Deleting a rule also clears its fired alerts server-side, so
					// refresh the fired-alerts table and active count too — otherwise both
					// show the just-cleared alerts until the next tick.
					m.client.DeleteAlertRule(id)
					m.refreshAlertRules()
					m.refreshAlertsData()
					m.refreshActiveAlerts()
					if m.alertRuleCursor >= len(m.alertRules) && m.alertRuleCursor > 0 {
						m.alertRuleCursor--
					}
				}
				return m, nil
			case " ":
				if m.alertFocus == 0 && m.alertRuleCursor < len(m.alertRules) {
					id := m.alertRules[m.alertRuleCursor].ID
					go m.client.ToggleAlertRule(id)
					m.refreshAlertRules()
				}
				return m, nil
			case "enter":
				if m.alertFocus == 1 {
					// The table rows are built from m.alertsData in order, so the table
					// cursor indexes m.alertsData directly. (The previous code re-fetched
					// GetAlerts() and re-indexed the fresh slice, which could ack the wrong
					// row if the order changed between render and keypress.)
					idx := m.alertTable.Cursor()
					if idx >= 0 && idx < len(m.alertsData) {
						// Synchronous (like the surrounding refresh* calls) so the
						// follow-up refresh observes the ack and shows it immediately.
						m.client.AckAlert(m.alertsData[idx].ID)
						m.refreshAlertsData()
					}
				}
				return m, nil
			case "t":
				m.notifySending = true
				client := m.client
				alert := testAlertFromRule(m.alertRules, m.alertRuleCursor)
				return m, func() tea.Msg {
					sentAt := time.Now()
					results, err := client.TestNotifications(alert)
					return notifyTestResultMsg{results: results, err: err, sentAt: sentAt}
				}
			case "c":
				m.notifyLog = nil
				return m, nil
			case "tab":
				m.alertFocus = (m.alertFocus + 1) % 2
				return m, nil
			case "down":
				if m.alertFocus == 0 {
					if len(m.alertRules) > 0 {
						m.alertRuleCursor = (m.alertRuleCursor + 1) % len(m.alertRules)
					}
				} else {
					var cmd tea.Cmd
					m.alertTable, cmd = m.alertTable.Update(msg)
					return m, cmd
				}
				return m, nil
			case "up":
				if m.alertFocus == 0 {
					if len(m.alertRules) > 0 {
						m.alertRuleCursor = (m.alertRuleCursor - 1 + len(m.alertRules)) % len(m.alertRules)
					}
				} else {
					var cmd tea.Cmd
					m.alertTable, cmd = m.alertTable.Update(msg)
					return m, cmd
				}
				return m, nil
			case "1", "2", "3", "4", "5", "6", "7", "8", "9":
				// Map number keys to visible tabs (1-indexed)
				idx := int(msg.String()[0] - '1')
				if idx >= 0 && idx < len(m.visibleTabs) {
					return m, m.switchView(m.visibleTabs[idx])
				}
				return m, nil
			case "right":
				return m, m.switchView(m.nextVisibleView())
			case "left":
				return m, m.switchView(m.prevVisibleView())
			default:
				if m.alertFocus == 1 {
					var cmd tea.Cmd
					m.alertTable, cmd = m.alertTable.Update(msg)
					return m, cmd
				}
				return m, nil
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			// Map number keys to visible tabs (1-indexed)
			idx := int(msg.String()[0] - '1')
			if idx >= 0 && idx < len(m.visibleTabs) {
				return m, m.switchView(m.visibleTabs[idx])
			}
		case "right":
			return m, m.switchView(m.nextVisibleView())
		case "left":
			return m, m.switchView(m.prevVisibleView())
		case "<", ",":
			if m.hasHistory() && m.historyRange > 0 {
				m.historyRange--
				m.d("range: %s", m.historyRangeLabel())
				m.customStart = nil
				m.customEnd = nil
				m.historyCache = nil
				m.procPinnedLastFetchEnd = time.Time{}
				m.historyFetching[m.current] = true
				m.saveHistoryRange()
				return m, m.fetchHistoryCmd()
			}
		case ">", ".":
			if m.hasHistory() && m.historyRange < len(m.historyRanges)-1 {
				m.historyRange++
				m.d("range: %s", m.historyRangeLabel())
				m.customStart = nil
				m.customEnd = nil
				m.historyCache = nil
				m.procPinnedLastFetchEnd = time.Time{}
				m.historyFetching[m.current] = true
				m.saveHistoryRange()
				return m, m.fetchHistoryCmd()
			}
		case "r":
			if m.hasHistory() {
				m.datePicker = newDatePicker(m.historyRanges)
				m.datePickerActive = true
			}
		case "x":
			m.capturedContent = m.captureViewContent()
			m.captureFormState = &captureFormState{
				path: defaultCapturePath(m.current, m.captureSettings.Directory),
			}
			m.captureForm = buildCaptureForm(m.captureFormState)
			m.captureFormActive = true
			return m, m.captureForm.Init()
		default:
			// Pass to viewport for scrolling
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	case captureResultMsg:
		if msg.err != nil {
			m.captureFlash = "Export failed: " + msg.err.Error()
		} else {
			m.captureFlash = "Saved to " + msg.path
		}
		m.captureFlashUntil = time.Now().Add(3 * time.Second)
		return m, tickCmd(m.interval)
	case notifyTestResultMsg:
		m.notifySending = false
		if msg.err != nil {
			m.notifyLog = append(m.notifyLog, notifyLogEntry{
				Error:  msg.err.Error(),
				SentAt: msg.sentAt,
			})
		} else {
			for _, r := range msg.results {
				m.notifyLog = append(m.notifyLog, notifyLogEntry{
					Method:     r.Method,
					Dest:       r.Dest,
					StatusCode: r.StatusCode,
					Latency:    r.Latency,
					Error:      r.Error,
					Body:       r.Body,
					SentAt:     msg.sentAt,
				})
			}
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.d("resize: %dx%d", msg.Width, msg.Height)
		m.width = msg.Width
		m.height = msg.Height
		tabBarHeight := 3 // tab bar + divider + newline
		gutterHeight := 0
		if m.statusData != nil {
			gutterHeight = 1
		}
		debugHeight := 0
		if m.debug != nil {
			debugHeight = m.debug.Height()
		}
		helpHeight := 2
		contentHeight := m.height - tabBarHeight - gutterHeight - debugHeight - helpHeight
		if contentHeight < 1 {
			contentHeight = 1
		}
		var prefetchCmds []tea.Cmd
		if !m.ready {
			m.viewport = viewport.New(m.width, contentHeight)
			m.ready = true
			prefetchCmds = m.prefetchAllHistoryCmds()
		} else {
			m.procPinnedLastFetchEnd = time.Time{}
			m.viewport.Width = m.width
			m.viewport.Height = contentHeight
			prefetchCmds = m.prefetchAllHistoryCmds()
		}
		m.alertTable.SetHeight(contentHeight - 4)
		m.setProcTableHeight(contentHeight)
		return m, tea.Batch(prefetchCmds...)
	case historyResultMsg:
		m.historyFetching[msg.forView] = false
		vn := viewName(msg.forView)
		if errors.Is(msg.err, ErrNotModified) {
			m.d("historyResult(%s): 304 (%s)", vn, msg.duration)
			// Data unchanged — only chain pinned fetch if we don't have one yet
			if m.current == viewProcess && len(m.pinnedProcesses) > 0 && m.procPinnedChart == "" {
				return m, m.fetchPinnedHistoryCmd(m.historyStart, m.historyEnd)
			}
			return m, nil
		}
		if msg.err != nil {
			m.d("historyResult(%s): err=%v (%s)", vn, msg.err, msg.duration)
			// Still try to fetch pinned data if we don't have it yet
			if m.current == viewProcess && len(m.pinnedProcesses) > 0 && m.procPinnedChart == "" {
				return m, m.fetchPinnedHistoryCmd(m.historyStart, m.historyEnd)
			}
			return m, nil // keep existing chart on transient errors
		}
		// Only apply if still on the same view that requested it
		if msg.forView != m.current {
			m.d("historyResult(%s): stale (now on %s), discarding (%s)", vn, viewName(m.current), msg.duration)
			return m, nil
		}

		// Incremental merge: merge tail data into existing cache.
		if msg.incremental {
			cache := m.historyCache[msg.forView]
			if cache != nil && len(cache.series) > 0 {
				merged, changed := mergeSeriesIncremental(cache.series, msg.series, msg.windowStart)
				cache.lastFetchEnd = msg.end
				cache.start = msg.windowStart
				cache.end = msg.end
				if !changed {
					m.d("historyResult(%s): incremental, no change (%s)", vn, msg.duration)
					// Still chain pinned fetch for process view if needed.
					if m.current == viewProcess && len(m.pinnedProcesses) > 0 {
						return m, m.fetchPinnedHistoryCmd(msg.windowStart, msg.end)
					}
					return m, nil
				}
				m.d("historyResult(%s): incremental, %d series updated (%s)", vn, len(merged), msg.duration)
				cache.series = merged
				m.historySeries = merged
				m.historyStart = msg.windowStart
				m.historyEnd = msg.end
				m.regenerateHistoryChart()
				cache.chart = m.cachedHistoryCharts[m.current]
				if m.current == viewProcess && len(m.pinnedProcesses) > 0 {
					return m, m.fetchPinnedHistoryCmd(msg.windowStart, msg.end)
				}
				return m, nil
			}
			// Cache disappeared — fall through to full update.
		}

		// Full fetch result.
		m.d("historyResult(%s): %d series (%s)", vn, len(msg.series), msg.duration)
		m.historySeries = msg.series
		m.historyStart = msg.start
		m.historyEnd = msg.end
		m.regenerateHistoryChart()
		// For process view, regenerateHistoryChart() handles its own cache sync
		// (top CPU chart stored separately from the displayed pinned chart).
		// Seed the topN fields so subsequent ticks use by-name incremental.
		if m.current == viewProcess {
			if cache := m.historyCache[viewProcess]; cache != nil {
				cache.lastFetchEnd = msg.end
				cache.rangeIndex = m.historyRange
				cache.customRange = m.customStart != nil && m.customEnd != nil
				names := make([]string, 0, len(msg.series))
				for _, s := range msg.series {
					names = append(names, s.Label)
				}
				cache.topNNames = names
				cache.topNLastFullAt = time.Now()
			}
		} else {
			if m.historyCache == nil {
				m.historyCache = make(map[view]*viewHistoryCache)
			}
			isCustom := m.customStart != nil && m.customEnd != nil
			m.historyCache[m.current] = &viewHistoryCache{
				series:       m.historySeries,
				chart:        m.cachedHistoryCharts[m.current],
				start:        m.historyStart,
				end:          m.historyEnd,
				lastFetchEnd: msg.end,
				rangeIndex:   m.historyRange,
				customRange:  isCustom,
			}
		}
		if m.current == viewProcess && len(m.pinnedProcesses) > 0 {
			return m, m.fetchPinnedHistoryCmd(msg.start, msg.end)
		}
		return m, nil
	case pinnedHistoryResultMsg:
		if errors.Is(msg.err, ErrNotModified) {
			m.d("pinnedHistoryResult: 304 (%s)", msg.duration)
			return m, nil
		}
		if msg.err != nil {
			m.d("pinnedHistoryResult: err=%v (%s)", msg.err, msg.duration)
			return m, nil
		}

		// Incremental merge for pinned process history.
		if msg.incremental && len(m.procPinnedSeries) > 0 {
			merged, changed := mergeSeriesIncremental(m.procPinnedSeries, msg.series, msg.windowStart)
			m.procPinnedLastFetchEnd = msg.end
			if !changed {
				m.d("pinnedHistoryResult: incremental, no change (%s)", msg.duration)
				return m, nil
			}
			m.d("pinnedHistoryResult: incremental, %d series updated (%s)", len(merged), msg.duration)
			m.procPinnedSeries = merged
			chartWidth := m.width - 4
			ch := chartHeightForTerminal(m.height)
			rangeLabel := m.historyRangeLabel()
			m.procPinnedChart = renderProcessHistoryChart(merged, chartWidth, ch, msg.windowStart, msg.end, m.pinnedProcesses)
			m.procPinnedChart = renderPanel(fmt.Sprintf("Pinned Process CPU [%s]", rangeLabel), m.procPinnedChart+historyHelpInline(rangeLabel), m.width)
			return m, nil
		}

		// Full fetch result.
		var labels []string
		for _, s := range msg.series {
			labels = append(labels, s.Label)
		}
		m.d("pinnedHistoryResult: %d series: %v (%s)", len(msg.series), labels, msg.duration)
		m.procPinnedSeries = msg.series
		m.procPinnedLastFetchEnd = msg.end
		// Track sorted pinned names for incremental eligibility check.
		m.procPinnedNames = make([]string, 0, len(m.pinnedProcesses))
		for name := range m.pinnedProcesses {
			m.procPinnedNames = append(m.procPinnedNames, name)
		}
		sort.Strings(m.procPinnedNames)
		chartWidth := m.width - 4
		ch := chartHeightForTerminal(m.height)
		rangeLabel := m.historyRangeLabel()
		m.procPinnedChart = renderProcessHistoryChart(msg.series, chartWidth, ch, msg.start, msg.end, m.pinnedProcesses)
		m.procPinnedChart = renderPanel(fmt.Sprintf("Pinned Process CPU [%s]", rangeLabel), m.procPinnedChart+historyHelpInline(rangeLabel), m.width)
		return m, nil
	case prefetchHistoryResultMsg:
		m.historyFetching[msg.forView] = false
		vn := viewName(msg.forView)
		if msg.err != nil {
			m.d("prefetch(%s): err=%v (%s)", vn, msg.err, msg.duration)
			return m, nil
		}
		m.d("prefetch(%s): %d series (%s)", vn, len(msg.series), msg.duration)
		if m.historyCache == nil {
			m.historyCache = make(map[view]*viewHistoryCache)
		}
		entry := &viewHistoryCache{
			series:       msg.series,
			start:        msg.start,
			end:          msg.end,
			lastFetchEnd: msg.end,
			rangeIndex:   m.historyRange,
		}
		if msg.forView == viewProcess {
			names := make([]string, 0, len(msg.series))
			for _, s := range msg.series {
				names = append(names, s.Label)
			}
			entry.topNNames = names
			entry.topNLastFullAt = time.Now()
		}
		m.historyCache[msg.forView] = entry
		m.renderHistoryCacheEntry(msg.forView)
		// If the user is on this view, promote to live display.
		if msg.forView == m.current {
			m.historySeries = msg.series
			m.historyStart = msg.start
			m.historyEnd = msg.end
			if cached := m.historyCache[msg.forView]; cached != nil {
				m.cachedHistoryCharts[m.current] = cached.chart
			}
			if msg.forView == viewProcess && len(m.pinnedProcesses) > 0 {
				return m, m.fetchPinnedHistoryCmd(msg.start, msg.end)
			}
		}
		return m, nil
	case tickMsg:
		m.updateVisibleTabs()
		var cmds []tea.Cmd
		if m.hasHistory() && !m.historyFetching[m.current] {
			// Only mark in-flight when a fetch is actually dispatched; a nil cmd
			// (e.g. a sub-section with no history) must not stick the flag true.
			if cmd := m.fetchHistoryCmd(); cmd != nil {
				m.historyFetching[m.current] = true
				cmds = append(cmds, cmd)
			}
		}
		// Refresh cached data for current view (served from in-memory cache, fast)
		switch m.current {
		case viewDashboard:
			m.refreshDashData()
			m.updateDashSparklines(m.dashData)
		case viewCPU:
			m.refreshCPUData()
		case viewMemory:
			m.refreshMemData()
		case viewDisk:
			m.refreshDiskData()
		case viewNetwork:
			m.refreshNetData()
			m.updateNetSparklines(m.netData)
		case viewHardware:
			m.refreshTempData()
			m.updateTempSparklines(m.tempData)
			m.refreshPowerData()
			m.updatePowerSparklines(m.powerData)
			m.refreshGPUData()
			m.updateGPUSparklines(m.gpuData)
			m.refreshMemData() // for ECC
		case viewProcess:
			m.refreshProcessData()
		case viewAlerts:
			m.refreshAlertRules()
			m.refreshAlertsData()
		case viewServices:
			m.refreshCustomData()
		}
		// Refresh the active-alert summary on every view so the status bar always
		// reflects current problems. On the Alerts view, derive it from the list we
		// just fetched to avoid a redundant query.
		if m.current == viewAlerts {
			var active []api.AlertMetric
			for _, a := range m.alertsData {
				if !a.Acknowledged {
					active = append(active, a)
				}
			}
			m.setActiveAlerts(active)
		} else {
			m.refreshActiveAlerts()
		}
		cmds = append(cmds, tickCmd(m.interval))
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

// captureViewContent returns the full rendered frame (tab bar + content + status bar)
// for screen capture. Called before showing the capture dialog so the dialog itself
// is not included.
func (m *Model) captureViewContent() string {
	header := renderTabBar(m.current, m.width, m.visibleTabs)
	content := m.renderCurrentContent()
	var gutter string
	if m.statusData != nil {
		gutter = "\n" + renderStatusBar(buildStatusBar(m.statusData, m.current, m.lastDataChange[m.current], m.activeAlerts, m.activeAlertSeverity, time.Time{}), m.width, m.activeAlertSeverity)
	}
	return header + content + m.viewFooter() + gutter
}

// runCapture performs the actual file write. Called as an async tea.Cmd.
func runCapture(state *captureFormState, content string, settings CaptureSettings) captureResultMsg {
	path := expandHome(state.path)
	if !strings.HasSuffix(path, ".png") {
		path += ".png"
	}
	grid := ParseANSI(content, settings.Foreground, settings.Background)

	f, err := os.Create(path)
	if err != nil {
		return captureResultMsg{err: err}
	}
	defer f.Close()

	if err := RenderPNG(grid, f, settings); err != nil {
		return captureResultMsg{err: err}
	}
	return captureResultMsg{path: path}
}

// viewFooter returns the current view's shortcut-help footer line (with a leading
// newline), rendered OUTSIDE the scrollable viewport so it stays visible no matter
// how tall the content grows — otherwise a help line embedded in a table panel
// scrolls off the bottom once the table fills the screen. Empty when an overlay
// (form / date picker / capture) owns the screen, or the view has no shortcuts
// (dashboard) or only chart-range controls that stay with the chart (cpu/mem/disk).
// Shared by View() and captureViewContent() so screenshots match the live frame.
func (m Model) viewFooter() string {
	if m.alertFormActive || m.datePickerActive || m.captureFormActive {
		return ""
	}
	var line string
	switch m.current {
	case viewCPU, viewMemory, viewDisk:
		// These views' only control is the history-range cycling; pin it to the
		// footer (rather than inside the chart panel, where it scrolled off) so it's
		// always visible. The range is also shown in the chart's panel title.
		line = normalHelpStyle.Render(fmt.Sprintf("< >:range [%s]  r:pick dates", m.historyRangeLabel()))
	case viewAlerts:
		line = renderAlertFooter(m.alertFocus, m.alertConfirmDelete, m.alertConfirmName, m.alertFormErr)
	case viewProcess:
		line = processFooter(m.procSearchActive, m.procSearchQuery, m.procSortBy, m.procPinnedOnly)
	case viewNetwork:
		line = netFooter(m.netDisplayBits)
	case viewHardware:
		// Only the sections with selectable items (and only when populated) carry
		// selection controls; ECC and empty sections have none.
		switch m.hardwareSection {
		case hwSectionTemp:
			if len(m.tempData) > 0 {
				line = hardwareSelectionHelp()
			}
		case hwSectionPower:
			if len(m.powerData) > 0 {
				line = hardwareSelectionHelp()
			}
		case hwSectionGPU:
			if len(m.gpuData) > 0 {
				line = hardwareSelectionHelp()
			}
		}
	case viewServices:
		line = servicesFooter(len(m.customSources) > 1, m.hasHistory(), m.historyRangeLabel())
	}
	if line == "" {
		return ""
	}
	return "\n" + line
}

func (m *Model) renderCurrentContent() string {
	if m.datePickerActive {
		return renderPanel("Select Time Range", m.datePicker.view(m.width-6), m.width)
	}
	if m.alertFormActive && m.alertForm != nil {
		title := "Create Alert Rule"
		if m.alertFormState != nil && m.alertFormState.editID != 0 {
			title = "Edit Alert Rule"
		}
		return renderPanel(title, m.alertForm.View(), m.width) +
			"\n" + helpStyle.Render("esc: cancel")
	}
	switch m.current {
	case viewDashboard:
		return renderDashboard(m.dashData, m.width, m.dashSparkData, m.netSelected, m.tempSelected, m.powerSelected, m.gpuSelected)
	case viewCPU:
		return renderCPUView(m.cpuData, m.width, m.cachedHistoryCharts[m.current])
	case viewMemory:
		return renderMemView(m.memData, m.width, m.cachedHistoryCharts[m.current])
	case viewDisk:
		return renderDiskView(m.diskData, m.width, m.cachedHistoryCharts[m.current])
	case viewNetwork:
		return renderNetView(m.netData, m.width, m.cachedHistoryCharts[m.current], m.netSparkData, m.netSelected, m.netCursor, m.netIfaceNames, m.netDisplayBits)
	case viewHardware:
		return renderHardwareView(m.tempData, m.powerData, m.eccData, m.gpuData, m.width, m.cachedHistoryCharts[m.current],
			m.tempSparkData, m.tempSelected, m.tempCursor,
			m.powerSparkData, m.powerSelected, m.powerCursor,
			m.gpuSparkData, m.gpuSelected, m.gpuCursor, m.gpuHints,
			m.hardwareSection)
	case viewProcess:
		chart := m.cachedHistoryCharts[m.current]
		if m.procChartPinned && m.procPinnedChart != "" {
			chart = m.procPinnedChart
		}
		if m.procChartPinned {
			which := "pinned"
			if m.procPinnedChart == "" {
				which = "topCPU(pinned-empty)"
			}
			m.d("render: using=%s pinnedLen=%d topCPULen=%d",
				which, len(m.procPinnedChart), len(m.cachedHistoryCharts[m.current]))
		}
		if !m.sizeProcTableForLayout(chart) {
			chart = "" // terminal too short for both — drop the chart, table takes the space
		}
		var combined []api.ProcessMetric
		if m.procData != nil {
			combined = orderedCombined(m.procData.Processes, m.procSearchQuery, m.pinnedProcesses, m.procPinnedOnly, m.procSortBy)
		}
		m.clampProcView(len(combined)) // clamp cursor + scroll the window to keep it visible
		c, fl := renderProcessView(m.procData, combined, m.width, m.procTableHeight, m.procScroll, m.procCursor,
			m.procSearchInput, chart, m.procSortBy, m.procSearchActive, m.procSearchQuery, m.pinnedProcesses, m.procPinnedOnly, m.procChartPinned)
		m.procFilteredLen = fl
		return c
	case viewAlerts:
		return renderAlertView(m.alertsData, m.width, &m.alertTable, m.alertRules, m.alertRuleCursor, m.alertFocus, m.notifyLog, m.notifySending)
	case viewServices:
		return renderCustomView(m.customSources, m.customMetrics, m.customStatus, m.width,
			m.cachedHistoryCharts[m.current], m.servicesSection, m.servicesMetricCursor)
	default:
		return ""
	}
}

// SetSize sets the terminal dimensions for the model and updates visible tabs. Used by
// the headless screenshot-capture path (the interactive TUI sizes via WindowSizeMsg). It
// also sizes the viewport and process table the way the resize handler does, so the
// bounded process table lays out correctly when rendered without a live event loop.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.updateVisibleTabs()

	gutterHeight := 0
	if m.statusData != nil {
		gutterHeight = 1
	}
	debugHeight := 0
	if m.debug != nil {
		debugHeight = m.debug.Height()
	}
	contentHeight := h - 3 - gutterHeight - debugHeight - 2 // tab bar (3) + gutter + debug + help (2)
	if contentHeight < 1 {
		contentHeight = 1
	}
	m.viewport.Width = w
	m.viewport.Height = contentHeight
	m.setProcTableHeight(contentHeight)
}

// captureFileName maps views to website-compatible filenames.
var captureFileName = map[view]string{
	viewDashboard: "dashboard",
	viewCPU:       "cpu",
	viewMemory:    "memory",
	viewDisk:      "disk",
	viewNetwork:   "network",
	viewHardware:  "hardware",
	viewProcess:   "process",
	viewAlerts:    "alerts",
}

// CaptureAllViews renders all views to PNG files in dir.
// Returns the image dimensions and list of files written.
func (m *Model) CaptureAllViews(dir string) (imgW, imgH int, files []string, err error) {
	// Force truecolor output so lipgloss renders ANSI color sequences
	// even when not connected to a terminal.
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)

	// Reset hardware section to temperature (default) for consistent screenshots.
	m.hardwareSection = hwSectionTemp

	m.clearETagCache()
	m.initSelectionMaps()

	views := []view{viewDashboard, viewCPU, viewMemory, viewDisk, viewNetwork, viewHardware, viewProcess, viewAlerts}
	for _, v := range views {
		name, ok := captureFileName[v]
		if !ok {
			name = strings.ToLower(viewName(v))
		}
		m.current = v

		m.fetchHistoryForCapture(v)

		content := m.captureViewContent()
		content = m.normalizeFrameHeight(content)

		grid := ParseANSI(content, m.captureSettings.Foreground, m.captureSettings.Background)

		path := filepath.Join(dir, name+".png")
		f, ferr := os.Create(path)
		if ferr != nil {
			return 0, 0, files, fmt.Errorf("creating %s: %w", path, ferr)
		}
		if rerr := RenderPNG(grid, f, m.captureSettings); rerr != nil {
			f.Close()
			return 0, 0, files, fmt.Errorf("rendering %s: %w", path, rerr)
		}
		f.Close()
		files = append(files, path)

		if imgW == 0 {
			padding := m.captureSettings.DPI / 9
			initPNGFont(float64(m.captureSettings.DPI))
			imgW = grid.Width*pngFonts.cellW + padding*2
			imgH = grid.Height*pngFonts.cellH + padding*2
		}
	}
	return imgW, imgH, files, nil
}

func (m Model) View() string {
	header := renderTabBar(m.current, m.width, m.visibleTabs)

	unreachableSince := m.client.UnreachableSince()
	var gutter string
	if m.captureFlash != "" && time.Now().Before(m.captureFlashUntil) {
		flash := lipgloss.NewStyle().Foreground(colorGreen).Render(m.captureFlash)
		gutter = lipgloss.NewStyle().Width(m.width).Align(lipgloss.Center).Render(flash)
	} else if m.statusData != nil || m.activeAlerts > 0 || !unreachableSince.IsZero() {
		// Render the gutter when we have collector info to show, when there are
		// active alerts to surface (even if the status fetch failed at startup),
		// or when the daemon is unreachable so a mid-session disconnect is visible.
		gutter = renderStatusBar(buildStatusBar(m.statusData, m.current, m.lastDataChange[m.current], m.activeAlerts, m.activeAlertSeverity, unreachableSince), m.width, m.activeAlertSeverity)
	}

	var debugPanel string
	if m.debug != nil {
		debugPanel = m.debug.Render(m.width)
	}

	if m.ready {
		viewportView := m.viewport.View()

		// Overlay capture form as a popover
		if m.captureFormActive && m.captureForm != nil {
			popup := renderPanel("Export Screenshot", m.captureForm.View(), 62) +
				"\n" + helpStyle.Render("esc: cancel")
			viewportView = placeOverlay(viewportView, popup, m.width, m.viewport.Height)
		}

		// Fixed footer for the alerts view (shortcut help / delete-confirm prompt /
		// form error), kept OUT of the scrollable viewport so it stays visible no
		// matter how tall the fired-alerts table grows. The helpHeight reservation in
		// the resize handler budgets for this line alongside the scroll indicator.
		footer := m.viewFooter()

		// Show scroll indicator if content is scrollable
		totalLines := m.viewport.TotalLineCount()
		if totalLines > m.viewport.Height {
			scrollPct := m.viewport.ScrollPercent()
			prog := progress.New(
				progress.WithScaledGradient(gradientStart, gradientEnd),
				progress.WithWidth(20),
				progress.WithoutPercentage(),
			)
			pctStr := fmt.Sprintf(" %.0f%% ", scrollPct*100)
			scrollHint := lipgloss.NewStyle().Foreground(colorMuted).Render(pctStr + "PgUp/Dn to scroll")
			indicator := prog.ViewAs(scrollPct) + scrollHint
			return header + viewportView + "\n" + indicator + footer + "\n" + debugPanel + gutter
		}
		return header + viewportView + footer + "\n" + debugPanel + gutter
	}
	return header
}
