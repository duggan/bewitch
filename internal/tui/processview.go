package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/duggan/bewitch/internal/api"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// procSortField represents the field to sort processes by.
type procSortField int

const (
	procSortCPU procSortField = iota
	procSortMem
	procSortPID
	procSortName
	procSortThreads
	procSortFDs
	procSortDiskIO
	procSortNet
)

// orderedProcessList filters, sorts, and splits processes into enriched (above the fold)
// and non-enriched (below the fold) slices. Both renderProcessView and selectedProcess
// must use this to ensure cursor indices map to the same processes.
func orderedProcessList(procs []api.ProcessMetric, searchQuery string, pinnedMap map[string]bool, pinnedOnly bool, sortBy procSortField) (enriched, nonEnriched []api.ProcessMetric) {
	toDisplay := procs
	if searchQuery != "" {
		queryLower := strings.ToLower(searchQuery)
		var filtered []api.ProcessMetric
		for _, p := range procs {
			if strings.Contains(strings.ToLower(p.Name), queryLower) ||
				strings.Contains(strings.ToLower(p.Cmdline), queryLower) {
				filtered = append(filtered, p)
			}
		}
		toDisplay = filtered
	}
	if pinnedOnly {
		var pinFiltered []api.ProcessMetric
		for _, p := range toDisplay {
			if pinnedMap[p.Name] {
				pinFiltered = append(pinFiltered, p)
			}
		}
		toDisplay = pinFiltered
	}
	sorted := make([]api.ProcessMetric, len(toDisplay))
	copy(sorted, toDisplay)
	sortProcesses(sorted, sortBy)

	for _, p := range sorted {
		if p.Enriched {
			enriched = append(enriched, p)
		} else {
			nonEnriched = append(nonEnriched, p)
		}
	}
	return
}

// orderedCombined returns the displayed processes in render order (enriched first, then
// non-enriched) — the single source of truth that the table rows and the cursor index
// (procTable.Cursor()) both map onto.
func orderedCombined(procs []api.ProcessMetric, searchQuery string, pinnedMap map[string]bool, pinnedOnly bool, sortBy procSortField) []api.ProcessMetric {
	enriched, nonEnriched := orderedProcessList(procs, searchQuery, pinnedMap, pinnedOnly, sortBy)
	combined := make([]api.ProcessMetric, 0, len(enriched)+len(nonEnriched))
	combined = append(combined, enriched...)
	combined = append(combined, nonEnriched...)
	return combined
}

// renderProcessView lays out the "Split Deck": a height-capped, scrolling process table
// over an always-visible history chart, with a one-line detail strip for the selected
// row in between. The table is hand-rendered (renderProcessTable) rather than via
// bubbles/table so cells can carry colour (process state, pin marker, dimmed lightweight
// rows) — bubbles/table's runewidth truncation corrupts ANSI inside a cell. Scroll/cursor
// state lives on the Model; this function is render-pure over the pre-clamped values.
//
// `combined` is the displayed process slice in render order (caller-built so the cursor
// maps to it). Returns the rendered content and the row count (for cursor bookkeeping).
func renderProcessView(procs *api.ProcessResponse, combined []api.ProcessMetric, width, tableHeight, scroll, cursor int, searchInput textinput.Model, cachedChart string, sortBy procSortField, searchActive bool, searchQuery string, pinnedMap map[string]bool, pinnedOnly bool, chartPinned bool) (string, int) {
	if procs == nil {
		return renderPanel("Processes", dimStyle.Render("loading..."), width), 0
	}

	cols := processColumns(width, sortBy)
	table := renderProcessTable(combined, cols, cursor, scroll, tableHeight, pinnedMap)

	var b strings.Builder

	// Search box (active) or a filter indicator (filter set but not editing).
	if searchActive {
		b.WriteString(searchInput.View() + "\n")
	} else if searchQuery != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(colorPink).Render("filter: "+searchQuery) + "  " +
			dimStyle.Render("(/:edit  esc:clear)") + "\n")
	}

	b.WriteString(renderPanel(processPanelTitle(procs, sortBy, len(combined), cursor, pinnedOnly, searchQuery), table, width))

	// Detail strip: the columns that don't fit (and the full cmdline) for the cursor row.
	b.WriteString("\n" + renderProcessDetailStrip(combined, cursor, width))

	// Chart-mode tabs + the pre-rendered history chart, always below the (bounded) table.
	if cachedChart != "" {
		b.WriteString("\n")
		b.WriteString(renderChartModeTabs(chartPinned, width))
		b.WriteString(cachedChart)
	}

	return b.String(), len(combined)
}

// renderProcessTable renders the bounded, scrolling, COLOURED process table: a styled
// header (purple, with the ▼ sort marker) and a bottom rule, then the window of data rows
// [scroll, scroll+body) around the cursor, padded to exactly tableHeight lines. Hand-
// rendered (not bubbles/table) so each cell can carry colour the table widget would
// corrupt: process state (R/D/Z), the pink pin marker, dimmed lightweight (Phase-1) rows,
// and the pink selected row.
func renderProcessTable(combined []api.ProcessMetric, cols []procColumn, cursor, scroll, tableHeight int, pinnedMap map[string]bool) string {
	tableW := 0
	for _, c := range cols {
		tableW += c.width
	}

	// Header + bottom rule (2 lines), mirroring the bubbles/table look.
	var head strings.Builder
	for _, c := range cols {
		head.WriteString(headerStyle.Render(alignCell(c.title, c.width, c.right)))
	}
	lines := []string{
		head.String(),
		lipgloss.NewStyle().Foreground(colorDeepPurple).Render(strings.Repeat("─", tableW)),
	}

	body := tableHeight - 2
	if body < 1 {
		body = 1
	}
	for i := scroll; i < scroll+body; i++ {
		if i >= 0 && i < len(combined) {
			lines = append(lines, renderProcessRow(combined[i], cols, i == cursor, pinnedMap, tableW))
		} else {
			lines = append(lines, "") // blank padding keeps the panel a fixed height
		}
	}
	return strings.Join(lines, "\n")
}

// renderProcessRow renders one process as a single coloured line of width tableW. The
// selected row is drawn plain and wrapped in the pink selected style (so per-cell colours
// don't fight the highlight); other rows colour the state and pin marker, and dim
// lightweight (non-enriched) rows so the fully-sampled processes stand out.
func renderProcessRow(p api.ProcessMetric, cols []procColumn, selected bool, pinnedMap map[string]bool, tableW int) string {
	if selected {
		var plain strings.Builder
		for _, c := range cols {
			plain.WriteString(alignCell(processCellValue(p, c.key, pinnedMap), c.width, c.right))
		}
		return selectedRowStyle.Render(plain.String())
	}

	var b strings.Builder
	for _, c := range cols {
		cell := alignCell(processCellValue(p, c.key, pinnedMap), c.width, c.right)
		switch {
		case c.key == "mark":
			if pinnedMap[p.Name] {
				cell = lipgloss.NewStyle().Foreground(colorPink).Render(cell)
			} else {
				cell = dimStyle.Render(cell)
			}
		case c.key == "st":
			cell = procStateStyle(p.State).Render(cell)
		case !p.Enriched:
			cell = dimStyle.Render(cell) // lightweight rows recede
		default:
			cell = valueStyle.Render(cell)
		}
		b.WriteString(cell)
	}
	return b.String()
}

// procStateStyle colours a process state letter: running purple, sleeping dim,
// uninterruptible-sleep (D) warning, zombie (Z) critical.
func procStateStyle(state string) lipgloss.Style {
	switch state {
	case "R":
		return lipgloss.NewStyle().Foreground(colorPurple)
	case "D":
		return alertWarnStyle
	case "Z":
		return alertCritStyle
	case "S", "I":
		return dimStyle
	}
	return valueStyle
}

// procColumn describes one process-table column: its key (matched in buildProcessRow),
// header title, fixed cell width, and alignment.
type procColumn struct {
	key   string
	title string
	width int
	right bool
}

// processColumns returns the visible column set for the given terminal width, with a ▼
// appended to the active sort's header. Narrow terminals drop the busier columns; the
// data is still collected and reachable via the sort keys and the detail strip.
func processColumns(width int, sortBy procSortField) []procColumn {
	inner := width - 4 // panel border (2) + padding (2)
	if inner < 20 {
		inner = 20
	}
	nameW := 16
	switch {
	case width >= 150:
		nameW = 24
	case width >= 120:
		nameW = 20
	case width >= 100:
		nameW = 18
	}

	cols := []procColumn{
		{key: "mark", title: "", width: 2},
		{key: "pid", title: "PID", width: 7, right: true},
		{key: "name", title: "NAME", width: nameW},
		{key: "cpu", title: "CPU%", width: 7, right: true},
		{key: "mem", title: "MEM", width: 8, right: true},
		{key: "st", title: "ST", width: 3},
	}
	if width >= 100 {
		cols = append(cols,
			procColumn{key: "thr", title: "THR", width: 5, right: true},
			procColumn{key: "fds", title: "FDs", width: 6, right: true},
			procColumn{key: "disk", title: "DISK R/W", width: 12, right: true},
		)
	}
	if width >= 120 {
		cols = append(cols, procColumn{key: "net", title: "NET R/W", width: 12, right: true})
	}
	cols = append(cols, procColumn{key: "age", title: "AGE", width: 5, right: true})

	// CMDLINE absorbs the leftover width when there's room for a useful amount.
	used := 0
	for _, c := range cols {
		used += c.width
	}
	if cmdW := inner - used; cmdW >= 14 {
		cols = append(cols, procColumn{key: "cmd", title: "CMDLINE", width: cmdW})
	}

	if active := sortColumnKey(sortBy); active != "" {
		for i := range cols {
			if cols[i].key == active && cols[i].title != "" {
				cols[i].title += "▼"
			}
		}
	}
	return cols
}

// sortColumnKey maps a sort field to the column key it highlights (empty if that column
// isn't part of the table layout).
func sortColumnKey(sortBy procSortField) string {
	switch sortBy {
	case procSortCPU:
		return "cpu"
	case procSortMem:
		return "mem"
	case procSortPID:
		return "pid"
	case procSortName:
		return "name"
	case procSortThreads:
		return "thr"
	case procSortFDs:
		return "fds"
	case procSortDiskIO:
		return "disk"
	case procSortNet:
		return "net"
	}
	return ""
}

// sortName is the human label for a sort field, shown in the panel title so the active
// sort is visible even when its column is hidden on a narrow terminal.
func sortName(sortBy procSortField) string {
	switch sortBy {
	case procSortCPU:
		return "cpu"
	case procSortMem:
		return "mem"
	case procSortPID:
		return "pid"
	case procSortName:
		return "name"
	case procSortThreads:
		return "threads"
	case procSortFDs:
		return "fds"
	case procSortDiskIO:
		return "disk"
	case procSortNet:
		return "net"
	}
	return "cpu"
}

// alignCell formats s to exactly width w (left- or right-aligned) with a one-column gap,
// so adjacent cells (rendered flush) don't run together. The plain result is exactly w
// wide; callers may then wrap it in a colour style (which doesn't change visible width).
func alignCell(s string, w int, right bool) string {
	if w <= 1 {
		return truncate(s, w)
	}
	s = truncate(s, w-1) // reserve one column for the inter-column gap
	if right {
		return fmt.Sprintf("%*s ", w-1, s)
	}
	return fmt.Sprintf("%-*s", w, s)
}

// processCellValue returns the raw (unaligned) string for one column of one process.
func processCellValue(p api.ProcessMetric, key string, pinnedMap map[string]bool) string {
	switch key {
	case "mark":
		switch {
		case pinnedMap[p.Name]:
			return "*"
		case !p.Enriched:
			return "·"
		default:
			return " "
		}
	case "pid":
		return fmt.Sprintf("%d", p.PID)
	case "name":
		return p.Name
	case "cpu":
		return fmt.Sprintf("%.1f%%", p.CPUUserPct+p.CPUSystemPct)
	case "mem":
		return humanBytes(p.RSSBytes)
	case "st":
		return p.State
	case "thr":
		return fmt.Sprintf("%d", p.NumThreads)
	case "fds":
		if !p.Enriched {
			return "--"
		}
		return fmt.Sprintf("%d", p.NumFDs)
	case "disk":
		if !p.Enriched {
			return "--"
		}
		return humanBytes(uint64(p.ReadBytesSec)) + "/" + humanBytes(uint64(p.WriteBytesSec))
	case "net":
		if !p.Enriched {
			return "--"
		}
		return humanBytes(uint64(p.RxBytesSec)) + "/" + humanBytes(uint64(p.TxBytesSec))
	case "age":
		return processAge(p)
	case "cmd":
		if p.Enriched && p.Cmdline != "" {
			return p.Cmdline
		}
		return "--"
	}
	return ""
}

func processAge(p api.ProcessMetric) string {
	if p.StartTimeNs <= 0 {
		return ""
	}
	return formatAge(time.Since(time.Unix(0, p.StartTimeNs)))
}

// processPanelTitle is the pink panel title: the summary line plus the active sort (so
// the sort is visible even when its column is hidden), the cursor position, and any
// active filter.
func processPanelTitle(procs *api.ProcessResponse, sortBy procSortField, shown, cursor int, pinnedOnly bool, searchQuery string) string {
	title := fmt.Sprintf("Processes · %d total · %d active · CPU %.1f%% · Mem %s · ↓%s",
		procs.TotalProcs, procs.ActiveProcs, procs.TotalCPUPct, humanBytes(procs.TotalRSSBytes), sortName(sortBy))
	if pinnedOnly {
		title += fmt.Sprintf(" · pinned %d", shown)
	}
	if searchQuery != "" {
		title += fmt.Sprintf(" · %d matches", shown)
	}
	if shown > 0 {
		title += fmt.Sprintf(" · %d/%d", cursor+1, shown)
	}
	return title
}

// renderProcessDetailStrip is the single line under the table showing the selected
// process in full — the columns the current width can't fit, plus the whole cmdline.
func renderProcessDetailStrip(combined []api.ProcessMetric, cursor, width int) string {
	if cursor < 0 || cursor >= len(combined) {
		return ""
	}
	p := combined[cursor]
	parts := []string{
		fmt.Sprintf("cpu %.1f%%", p.CPUUserPct+p.CPUSystemPct),
		fmt.Sprintf("mem %s", humanBytes(p.RSSBytes)),
		fmt.Sprintf("thr %d", p.NumThreads),
	}
	if p.Enriched {
		parts = append(parts,
			fmt.Sprintf("fds %d", p.NumFDs),
			fmt.Sprintf("disk %s/%s", humanBytes(uint64(p.ReadBytesSec)), humanBytes(uint64(p.WriteBytesSec))),
			fmt.Sprintf("net %s/%s", humanBytes(uint64(p.RxBytesSec)), humanBytes(uint64(p.TxBytesSec))),
		)
	}
	if age := processAge(p); age != "" {
		parts = append(parts, "age "+age)
	}
	rest := strings.Join(parts, " · ")
	if p.Enriched && p.Cmdline != "" {
		rest += " · " + p.Cmdline
	}

	head := fmt.Sprintf("%d %s", p.PID, p.Name)
	marker := lipgloss.NewStyle().Foreground(colorPink).Render("▸ ")
	// Budget: marker (2) + head + " · " (3), truncate the remainder to fit one line.
	avail := width - 2 - utf8.RuneCountInString(head) - 3
	if avail < 0 {
		avail = 0
	}
	return marker + valueStyle.Render(head) + dimStyle.Render(" · "+truncate(rest, avail))
}

// processFooter builds the process view's shortcut-help line for the fixed footer,
// with the active sort key / filter highlighted.
func processFooter(searchActive bool, searchQuery string, sortBy procSortField, pinnedOnly bool) string {
	if searchActive {
		return normalHelpStyle.Render("enter:confirm  esc:clear  searching name/cmdline")
	}
	type helpItem struct {
		text   string
		active bool
	}
	items := []helpItem{
		{"/:search", searchQuery != ""},
		{"a:alert", false},
		{"*:pin", false},
		{"P:pinned", pinnedOnly},
		{"↑↓:navigate", false},
		{"c:cpu", sortBy == procSortCPU},
		{"m:mem", sortBy == procSortMem},
		{"p:pid", sortBy == procSortPID},
		{"n:name", sortBy == procSortName},
		{"t:thr", sortBy == procSortThreads},
		{"f:fds", sortBy == procSortFDs},
		{"d:disk", sortBy == procSortDiskIO},
		{"w:net", sortBy == procSortNet},
	}
	var parts []string
	for _, item := range items {
		if item.active {
			parts = append(parts, activeHelpStyle.Render(item.text))
		} else {
			parts = append(parts, normalHelpStyle.Render(item.text))
		}
	}
	return strings.Join(parts, normalHelpStyle.Render("  "))
}

var (
	chartActiveTabBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      " ",
		Left:        "│",
		Right:       "│",
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "┘",
		BottomRight: "└",
	}
	chartInactiveTabBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      "─",
		Left:        "│",
		Right:       "│",
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "┴",
		BottomRight: "┴",
	}
	chartTabBase = lipgloss.NewStyle().
			Border(chartInactiveTabBorder, true).
			BorderForeground(colorDeepPurple).
			Padding(0, 1)
	chartActiveTab = chartTabBase.
			Border(chartActiveTabBorder, true).
			Bold(true).
			Foreground(colorPink)
	chartInactiveTab = chartTabBase.
				Foreground(colorMuted)
	chartTabGap = chartTabBase.
			BorderTop(false).
			BorderLeft(false).
			BorderRight(false)
)

func renderChartModeTabs(pinned bool, width int) string {
	var topCPU, pinnedTab string
	if !pinned {
		topCPU = chartActiveTab.Render("Top CPU")
		pinnedTab = chartInactiveTab.Render("Pinned")
	} else {
		topCPU = chartInactiveTab.Render("Top CPU")
		pinnedTab = chartActiveTab.Render("Pinned")
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, topCPU, pinnedTab)
	gapWidth := width - lipgloss.Width(row)
	if gapWidth < 0 {
		gapWidth = 0
	}
	gap := chartTabGap.Render(strings.Repeat(" ", gapWidth))
	return lipgloss.JoinHorizontal(lipgloss.Bottom, row, gap) + "\n"
}

func sortProcesses(procs []api.ProcessMetric, sortBy procSortField) {
	switch sortBy {
	case procSortCPU:
		sort.Slice(procs, func(i, j int) bool {
			return (procs[i].CPUUserPct + procs[i].CPUSystemPct) > (procs[j].CPUUserPct + procs[j].CPUSystemPct)
		})
	case procSortMem:
		sort.Slice(procs, func(i, j int) bool {
			return procs[i].RSSBytes > procs[j].RSSBytes
		})
	case procSortPID:
		sort.Slice(procs, func(i, j int) bool {
			return procs[i].PID < procs[j].PID
		})
	case procSortName:
		sort.Slice(procs, func(i, j int) bool {
			return procs[i].Name < procs[j].Name
		})
	case procSortThreads:
		sort.Slice(procs, func(i, j int) bool {
			return procs[i].NumThreads > procs[j].NumThreads
		})
	case procSortFDs:
		sort.Slice(procs, func(i, j int) bool {
			return procs[i].NumFDs > procs[j].NumFDs
		})
	case procSortDiskIO:
		sort.Slice(procs, func(i, j int) bool {
			return procs[i].ReadBytesSec+procs[i].WriteBytesSec > procs[j].ReadBytesSec+procs[j].WriteBytesSec
		})
	case procSortNet:
		sort.Slice(procs, func(i, j int) bool {
			return procs[i].RxBytesSec+procs[i].TxBytesSec > procs[j].RxBytesSec+procs[j].TxBytesSec
		})
	}
}

func formatAge(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	days := int(d.Hours() / 24)
	if days < 7 {
		return fmt.Sprintf("%dd", days)
	}
	if days < 30 {
		return fmt.Sprintf("%dw", days/7)
	}
	if days < 365 {
		return fmt.Sprintf("%dmo", days/30)
	}
	return fmt.Sprintf("%dy", days/365)
}

func renderProcessHistoryChart(series []api.TimeSeries, width, height int, start, end time.Time, pinnedMap map[string]bool) string {
	// Process CPU can exceed 100% with multiple cores
	maxVal := 100.0
	for _, s := range series {
		for _, p := range s.Points {
			if p.Value > maxVal {
				maxVal = p.Value
			}
		}
	}
	if maxVal > 100 {
		maxVal = float64(int(maxVal/50)+1) * 50
	}

	bucket := processBucketDuration(end.Sub(start))
	staleThreshold := 2 * bucket

	return renderBrailleChart(chartConfig{
		series:       series,
		width:        width,
		height:       height,
		start:        start,
		end:          end,
		yMin:         0,
		yMax:         maxVal,
		yFormatter:   yFmtPercent,
		pinnedMap:    pinnedMap,
		staleDropoff: staleThreshold,
	})
}

// processBucketDuration mirrors the API's bucketInterval logic, returning
// the time-bucket width used for process history queries.
func processBucketDuration(d time.Duration) time.Duration {
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
