package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/duggan/bewitch/internal/api"

	"github.com/charmbracelet/lipgloss"
)

// renderCustomView renders the Services tab for one active source sub-section:
// an optional source sub-tab bar, a live status strip, a metrics list (with the
// charted metric highlighted), and the pre-rendered history chart. Pure over its
// inputs — like the other render*View functions.
func renderCustomView(
	sources []api.CustomSourceInfo,
	metrics []api.CustomMetric,
	status []api.CustomStatus,
	width int,
	cachedChart string,
	activeSection int,
	metricCursor int,
) string {
	if len(sources) == 0 {
		return renderPanel("Services", dimStyle.Render("No custom sources configured."), width)
	}
	if activeSection < 0 || activeSection >= len(sources) {
		activeSection = 0
	}
	src := sources[activeSection]

	var b strings.Builder

	// Source sub-tab bar (only when more than one source).
	if len(sources) > 1 {
		b.WriteString(renderServiceSubTabs(sources, activeSection, width))
		b.WriteString("\n")
	}

	// Live status strip for the active source.
	var statusLines []string
	for _, st := range status {
		if st.Source != src.Name {
			continue
		}
		statusLines = append(statusLines,
			labelStyle.Render(st.Label+":")+" "+badgeStyle(st.Badge).Render(st.Value))
	}
	if len(statusLines) > 0 {
		b.WriteString(renderPanel("Status", strings.Join(statusLines, "\n"), width))
		b.WriteString("\n")
	}

	// Metrics list. Values come from the flattened live cache; missing values
	// (not yet polled) render as "--".
	if len(src.Metrics) > 0 {
		vals := make(map[string]api.CustomMetric, len(metrics))
		for _, mt := range metrics {
			if mt.Source == src.Name {
				vals[mt.Name] = mt
			}
		}
		if metricCursor < 0 || metricCursor >= len(src.Metrics) {
			metricCursor = 0
		}
		var lines []string
		for i, f := range src.Metrics {
			valStr := dimStyle.Render("--")
			if mv, ok := vals[f.Name]; ok {
				valStr = valueStyle.Render(formatCustomValue(mv.Value, f.Unit))
			}
			if i == metricCursor {
				lines = append(lines, selectedMetricStyle.Render("▸ "+padRight(f.Name, 14))+" "+valStr)
			} else {
				lines = append(lines, "  "+labelStyle.Render(f.Name)+" "+valStr)
			}
		}
		b.WriteString(renderPanel("Metrics", strings.Join(lines, "\n"), width))
	} else if len(statusLines) == 0 {
		b.WriteString(renderPanel(src.Name, dimStyle.Render("No data."), width))
	}

	// History chart for the selected metric (already wrapped in its own panel).
	if cachedChart != "" {
		b.WriteString("\n")
		b.WriteString(cachedChart)
	}

	return b.String()
}

var selectedMetricStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPink)

// renderServiceSubTabs renders the per-source sub-tab bar, mirroring the
// Hardware tab's renderHardwareSubTabs.
func renderServiceSubTabs(sources []api.CustomSourceInfo, active, width int) string {
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(colorPink)
	inactiveStyle := lipgloss.NewStyle().Foreground(colorDeepPurple)
	var parts []string
	for i, s := range sources {
		style := inactiveStyle
		if i == active {
			style = activeStyle
		}
		parts = append(parts, style.Render(s.Name))
	}
	bar := strings.Join(parts, inactiveStyle.Render("  │  "))
	help := lipgloss.NewStyle().Foreground(colorDeepPurple).Render("  tab:switch service")
	return bar + help
}

// servicesFooter is the Services tab's shortcut help line.
func servicesFooter(multiSource, hasHistory bool, rangeLabel string) string {
	sep := normalHelpStyle.Render("  ")
	var parts []string
	if multiSource {
		parts = append(parts, normalHelpStyle.Render("tab:switch service"))
	}
	if hasHistory {
		parts = append(parts,
			normalHelpStyle.Render("↑↓:select metric"),
			normalHelpStyle.Render(fmt.Sprintf("< >:range [%s]", rangeLabel)),
			normalHelpStyle.Render("r:pick dates"))
	}
	return strings.Join(parts, sep)
}

// badgeStyle maps a status badge token to a style.
func badgeStyle(badge string) lipgloss.Style {
	switch badge {
	case "ok":
		return lipgloss.NewStyle().Foreground(colorGreen)
	case "warn":
		return alertWarnStyle
	case "crit":
		return alertCritStyle
	default:
		return valueStyle
	}
}

// formatCustomValue renders a numeric value according to its declared unit.
func formatCustomValue(v float64, unit string) string {
	switch unit {
	case "bytes":
		if v < 0 {
			v = 0
		}
		return humanBytes(uint64(v))
	case "bits":
		return formatBits(v)
	case "percent":
		return fmt.Sprintf("%.1f%%", v)
	case "count":
		return fmt.Sprintf("%.0f", v)
	case "duration":
		return time.Duration(v * float64(time.Second)).Truncate(time.Second).String()
	default:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
}

// formatBits renders a value already expressed in bits (decimal units).
func formatBits(bits float64) string {
	switch {
	case bits >= 1e12:
		return fmt.Sprintf("%.1fTb", bits/1e12)
	case bits >= 1e9:
		return fmt.Sprintf("%.1fGb", bits/1e9)
	case bits >= 1e6:
		return fmt.Sprintf("%.1fMb", bits/1e6)
	case bits >= 1e3:
		return fmt.Sprintf("%.1fKb", bits/1e3)
	default:
		return fmt.Sprintf("%.0fb", bits)
	}
}

// renderCustomHistoryChart renders the selected metric's history with a
// unit-appropriate Y-axis.
func renderCustomHistoryChart(series []api.TimeSeries, width, height int, start, end time.Time, unit string) string {
	if len(series) == 0 {
		return dimStyle.Render("No history data yet.")
	}
	yMax := 100.0
	if unit != "percent" {
		yMax = autoMaxY(series, 1, 0)
	}
	return renderBrailleChart(chartConfig{
		series:     series,
		width:      width,
		height:     height,
		start:      start,
		end:        end,
		yMin:       0,
		yMax:       yMax,
		yFormatter: func(_ int, v float64) string { return formatCustomValue(v, unit) },
	})
}

// padRight pads s with spaces to width n (for column alignment in the metrics list).
func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
