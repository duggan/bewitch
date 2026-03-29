package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/duggan/bewitch/internal/api"

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

	// Filter, sort, and split into enriched (above fold) and non-enriched (below fold)
	enriched, nonEnriched := orderedProcessList(procs.Processes, searchQuery, pinnedMap, pinnedOnly, sortBy)
	totalFiltered := len(enriched) + len(nonEnriched)

	// Summary line (show filter count if searching)
	baseSummary := fmt.Sprintf("%d total │ %d active │ %d running │ CPU: %.1f%% │ Mem: %s",
		procs.TotalProcs, procs.ActiveProcs, procs.RunningProcs, procs.TotalCPUPct, humanBytes(procs.TotalRSSBytes))
	summaryLine := summaryStyle.Render(baseSummary)
	activeFilterStyle := lipgloss.NewStyle().Foreground(colorPink).Bold(true)
	if pinnedOnly {
		summaryLine += summaryStyle.Render(" │ ") + activeFilterStyle.Render(fmt.Sprintf("pinned (%d)", totalFiltered))
	}
	if searchQuery != "" {
		summaryLine += summaryStyle.Render(" │ ") + activeFilterStyle.Render(fmt.Sprintf("%d matches", totalFiltered))
	}
	b.WriteString(summaryLine + "\n\n")

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

	// renderRow renders a single process row at the given cursor-relative index.
	renderRow := func(p api.ProcessMetric, idx int) string {
		cpuPct := p.CPUUserPct + p.CPUSystemPct
		isSelected := idx == cursor

		name := p.Name
		if len(name) > nameWidth {
			name = name[:nameWidth-1] + "…"
		}

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

		age := ""
		if p.StartTimeNs > 0 {
			started := time.Unix(0, p.StartTimeNs)
			dur := time.Since(started)
			age = formatAge(dur)
		}

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

		if isSelected {
			row = selectedRowStyle.Render(row)
		}
		return row
	}

	// Render enriched processes (above the fold)
	rowIdx := 0
	for _, p := range enriched {
		table.WriteString(renderRow(p, rowIdx) + "\n")
		rowIdx++
	}

	// Fold separator between enriched and non-enriched
	maxBelowFold := 50
	belowFold := len(nonEnriched)
	if belowFold > maxBelowFold {
		belowFold = maxBelowFold
	}
	filteredLen := len(enriched) + belowFold

	if len(nonEnriched) > 0 && len(enriched) > 0 {
		foldLabel := fmt.Sprintf(" %d more processes ", len(nonEnriched))
		innerWidth := width - 4 // account for panel border + padding
		dashes := innerWidth - len(foldLabel)
		if dashes < 2 {
			dashes = 2
		}
		left := dashes / 2
		right := dashes - left
		foldLine := strings.Repeat("─", left) + foldLabel + strings.Repeat("─", right)
		table.WriteString(dimStyle.Render(foldLine) + "\n")
	}

	// Render non-enriched processes (below the fold), capped
	for i := 0; i < belowFold; i++ {
		table.WriteString(renderRow(nonEnriched[i], rowIdx) + "\n")
		rowIdx++
	}

	if len(nonEnriched) > maxBelowFold {
		table.WriteString(dimStyle.Render(fmt.Sprintf("  ... and %d more processes", len(nonEnriched)-maxBelowFold)) + "\n")
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
