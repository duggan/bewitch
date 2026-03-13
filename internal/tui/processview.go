package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ross/bewitch/internal/api"

	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
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
)

func renderProcessView(procs *api.ProcessResponse, width int, cachedChart string, sortBy procSortField, cursor int, searchActive bool, searchQuery string, pinnedMap map[string]bool, pinnedOnly bool, chartPinned bool) (string, int) {
	var b strings.Builder

	if procs == nil {
		return renderPanel("Processes", dimStyle.Render("loading..."), width), 0
	}

	// Search input or filter indicator
	if searchActive {
		searchBox := fmt.Sprintf("/%s█", searchQuery)
		b.WriteString(lipgloss.NewStyle().Foreground(colorPink).Render(searchBox) + "\n")
	} else if searchQuery != "" {
		// Show filter indicator when not in input mode but filter is active
		filterIndicator := fmt.Sprintf("filter: %s", searchQuery)
		b.WriteString(lipgloss.NewStyle().Foreground(colorPink).Render(filterIndicator) + "  " +
			lipgloss.NewStyle().Foreground(colorDeepPurple).Render("(/:edit  esc:clear)") + "\n")
	}

	// Filter processes if search is active
	toDisplay := procs.Processes
	if searchQuery != "" {
		queryLower := strings.ToLower(searchQuery)
		var filtered []api.ProcessMetric
		for _, p := range procs.Processes {
			if strings.Contains(strings.ToLower(p.Name), queryLower) ||
				strings.Contains(strings.ToLower(p.Cmdline), queryLower) {
				filtered = append(filtered, p)
			}
		}
		toDisplay = filtered
	}
	// Filter to pinned only
	if pinnedOnly {
		var pinFiltered []api.ProcessMetric
		for _, p := range toDisplay {
			if pinnedMap[p.Name] {
				pinFiltered = append(pinFiltered, p)
			}
		}
		toDisplay = pinFiltered
	}
	filteredLen := len(toDisplay)

	// Summary line (show filter count if searching)
	baseSummary := fmt.Sprintf("%d total │ %d active │ %d running │ CPU: %.1f%% │ Mem: %s",
		procs.TotalProcs, procs.ActiveProcs, procs.RunningProcs, procs.TotalCPUPct, humanBytes(procs.TotalRSSBytes))
	summaryLine := summaryStyle.Render(baseSummary)
	activeFilterStyle := lipgloss.NewStyle().Foreground(colorPink).Bold(true)
	if pinnedOnly {
		summaryLine += summaryStyle.Render(" │ ") + activeFilterStyle.Render(fmt.Sprintf("pinned (%d)", filteredLen))
	}
	if searchQuery != "" {
		summaryLine += summaryStyle.Render(" │ ") + activeFilterStyle.Render(fmt.Sprintf("%d matches", filteredLen))
	}
	b.WriteString(summaryLine + "\n\n")

	// Sort processes
	sorted := make([]api.ProcessMetric, len(toDisplay))
	copy(sorted, toDisplay)
	sortProcesses(sorted, sortBy)

	// Build process table
	var table strings.Builder

	// Header
	sortIndicator := func(field procSortField) string {
		if sortBy == field {
			return "▼"
		}
		return ""
	}

	// Adaptive column widths based on terminal width
	nameWidth := 16
	cmdlineWidth := 0
	if width >= 120 {
		nameWidth = 20
		cmdlineWidth = 30
	} else if width >= 100 {
		nameWidth = 18
		cmdlineWidth = 20
	}

	header := fmt.Sprintf("   %-6s %-*s", "PID", nameWidth, "NAME"+sortIndicator(procSortName))
	header += fmt.Sprintf(" %7s", "CPU%"+sortIndicator(procSortCPU))
	header += fmt.Sprintf(" %8s", "MEM"+sortIndicator(procSortMem))
	header += " STATE"
	header += fmt.Sprintf(" %4s", "THR"+sortIndicator(procSortThreads))
	header += fmt.Sprintf(" %5s", "FDs"+sortIndicator(procSortFDs))
	header += " AGE"
	if cmdlineWidth > 0 {
		header += fmt.Sprintf(" %-*s", cmdlineWidth, "CMDLINE")
	}
	table.WriteString(headerStyle.Render(header) + "\n")

	// Rows
	maxRows := 50
	if len(sorted) < maxRows {
		maxRows = len(sorted)
	}
	for i := 0; i < maxRows; i++ {
		p := sorted[i]
		cpuPct := p.CPUUserPct + p.CPUSystemPct
		isSelected := i == cursor

		// Process name (truncate if needed)
		name := p.Name
		if len(name) > nameWidth {
			name = name[:nameWidth-1] + "…"
		}

		// State with color (only apply colors for non-selected rows)
		stateStr := p.State
		if !isSelected {
			stateStyle := valueStyle
			switch p.State {
			case "R":
				stateStyle = lipgloss.NewStyle().Foreground(colorPurple)
			case "S":
				stateStyle = dimStyle
			case "D":
				stateStyle = alertWarnStyle
			case "Z":
				stateStyle = alertCritStyle
			}
			stateStr = stateStyle.Render(p.State)
		}

		// Age calculation
		age := ""
		if p.StartTimeNs > 0 {
			started := time.Unix(0, p.StartTimeNs)
			dur := time.Since(started)
			age = formatAge(dur)
		}

		// Pin indicator
		pinChar := " "
		if pinnedMap[p.Name] {
			if isSelected {
				pinChar = "*"
			} else {
				pinChar = lipgloss.NewStyle().Foreground(colorPink).Render("*")
			}
		}

		row := fmt.Sprintf("%s %-6d %-*s", pinChar, p.PID, nameWidth, name)
		row += fmt.Sprintf(" %6.1f%%", cpuPct)
		row += fmt.Sprintf(" %8s", humanBytes(p.RSSBytes))
		row += fmt.Sprintf("  %s  ", stateStr)
		row += fmt.Sprintf(" %4d", p.NumThreads)
		if p.Enriched {
			row += fmt.Sprintf(" %5d", p.NumFDs)
		} else {
			row += "    --"
		}
		row += fmt.Sprintf(" %6s", age)

		if cmdlineWidth > 0 {
			if p.Enriched && p.Cmdline != "" {
				cmdline := p.Cmdline
				if len(cmdline) > cmdlineWidth {
					cmdline = cmdline[:cmdlineWidth-1] + "…"
				}
				if !isSelected {
					row += " " + dimStyle.Render(cmdline)
				} else {
					row += " " + cmdline
				}
			} else if !p.Enriched {
				if !isSelected {
					row += " " + dimStyle.Render("--")
				} else {
					row += " --"
				}
			}
		}

		// Apply highlight style to selected row
		if isSelected {
			row = selectedRowStyle.Render(row)
		}

		table.WriteString(row + "\n")
	}

	if len(sorted) > maxRows {
		table.WriteString(dimStyle.Render(fmt.Sprintf("  ... and %d more processes", len(sorted)-maxRows)) + "\n")
	}

	// Build help line with active-state highlighting
	normalHelp := lipgloss.NewStyle().Foreground(colorDeepPurple)
	activeHelp := lipgloss.NewStyle().Foreground(colorPink).Bold(true)

	type helpItem struct {
		text   string
		active bool
	}

	var helpLine string
	if searchActive {
		helpLine = normalHelp.Render("enter:confirm  esc:clear  searching name/cmdline")
	} else {
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
		}
		var parts []string
		for _, item := range items {
			if item.active {
				parts = append(parts, activeHelp.Render(item.text))
			} else {
				parts = append(parts, normalHelp.Render(item.text))
			}
		}
		helpLine = strings.Join(parts, normalHelp.Render("  "))
	}

	b.WriteString(renderPanel("Process List", table.String()+helpLine, width))

	// Chart mode tabs and history chart (pre-rendered)
	if cachedChart != "" {
		b.WriteString("\n")
		b.WriteString(renderChartModeTabs(chartPinned, width))
		b.WriteString(cachedChart)
	}

	return b.String(), filteredLen
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

func renderProcessHistoryChart(series []api.TimeSeries, width int, start, end time.Time, pinnedMap map[string]bool) string {
	// Reuse the existing chart rendering but with different Y axis scaling
	// Process CPU can exceed 100% with multiple cores
	maxVal := 100.0
	for _, s := range series {
		for _, p := range s.Points {
			if p.Value > maxVal {
				maxVal = p.Value
			}
		}
	}
	// Round up to nice number
	if maxVal > 100 {
		maxVal = float64(int(maxVal/50)+1) * 50
	}

	return renderHistoryChartWithMax(series, width, start, end, maxVal, pinnedMap)
}

func renderHistoryChartWithMax(series []api.TimeSeries, width int, start, end time.Time, maxY float64, pinnedMap map[string]bool) string {
	chartWidth := width
	if chartWidth < 20 {
		chartWidth = 20
	}
	chartHeight := 12

	// Determine the bucket interval so we can detect stale series.
	// A process whose last data point is more than 2 buckets before
	// the chart end is no longer running — drop it to zero instead
	// of forward-filling.
	bucket := processBucketDuration(end.Sub(start))
	staleThreshold := 2 * bucket

	opts := []timeserieslinechart.Option{
		timeserieslinechart.WithTimeRange(start, end),
		timeserieslinechart.WithYRange(0, maxY),
		timeserieslinechart.WithXLabelFormatter(xLabelFormatter(end.Sub(start))),
		timeserieslinechart.WithYLabelFormatter(func(_ int, v float64) string {
			return fmt.Sprintf("%.0f%%", v)
		}),
	}

	for i, s := range series {
		if len(s.Points) == 0 {
			continue
		}
		color := chartColors[i%len(chartColors)]
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		points := make([]timeserieslinechart.TimePoint, len(s.Points))
		for j, p := range s.Points {
			points[j] = timeserieslinechart.TimePoint{
				Time:  time.Unix(0, p.TimestampNS),
				Value: p.Value,
			}
		}
		lastTime := points[len(points)-1].Time
		if end.Sub(lastTime) > staleThreshold {
			// Process stopped running — drop to zero one bucket after
			// its last real data point.
			points = append(points, timeserieslinechart.TimePoint{
				Time:  lastTime.Add(bucket),
				Value: 0,
			})
		} else {
			points = forwardFillPoints(points, end)
		}
		opts = append(opts,
			timeserieslinechart.WithDataSetStyle(s.Label, style),
			timeserieslinechart.WithDataSetTimeSeries(s.Label, points),
		)
	}

	chart := timeserieslinechart.New(chartWidth, chartHeight, opts...)
	chart.DrawAll()

	// Build legend
	var legend strings.Builder
	for i, s := range series {
		if len(s.Points) == 0 {
			continue
		}
		color := chartColors[i%len(chartColors)]
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		if i > 0 {
			legend.WriteString("  ")
		}
		label := s.Label
		if pinnedMap[label] {
			label = "* " + label
		}
		legend.WriteString(style.Render("━ " + label))
	}

	return chart.View() + "\n" + legend.String()
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
