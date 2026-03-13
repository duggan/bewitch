package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ross/bewitch/internal/api"

	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/lipgloss"
)

func renderCPUView(cores []api.CPUCoreMetric, width int, cachedChart string) string {
	var b strings.Builder
	barWidth := width - 58 // label(14) + wide value text(~40) + panel border+padding(4)
	if barWidth < 10 {
		barWidth = 10
	}

	if cores == nil {
		return renderPanel("CPU Details", dimStyle.Render("loading..."), width)
	}

	// Summary line
	var maxPct, totalPct float64
	numCores := 0
	for _, c := range cores {
		if c.Core == -1 {
			continue // skip aggregate
		}
		numCores++
		pct := c.UserPct + c.SystemPct
		totalPct += pct
		if pct > maxPct {
			maxPct = pct
		}
	}
	avgPct := 0.0
	if numCores > 0 {
		avgPct = totalPct / float64(numCores)
	}
	b.WriteString(summaryStyle.Render(fmt.Sprintf("%d cores │ avg %.1f%% │ max %.1f%%", numCores, avgPct, maxPct)) + "\n\n")

	// Core list
	var coreList strings.Builder
	for _, c := range cores {
		usedPct := c.UserPct + c.SystemPct
		label := fmt.Sprintf("core %d", c.Core)
		lStyle := labelStyle
		if c.Core == -1 {
			label = "aggregate"
			lStyle = highlightLabelStyle
		}
		if len(label) > 12 {
			label = label[:12]
		}
		detail := fmt.Sprintf(" %.1f%%", usedPct)
		if width >= 90 {
			detail = fmt.Sprintf(" %.1f%% (usr:%.1f sys:%.1f io:%.1f)", usedPct, c.UserPct, c.SystemPct, c.IOWaitPct)
		}
		coreList.WriteString(
			lStyle.Render(label) +
				renderBar(usedPct, barWidth) +
				valueStyle.Render(detail) +
				"\n",
		)
	}
	b.WriteString(renderPanel("Cores", coreList.String(), width))

	// History chart (pre-rendered)
	if cachedChart != "" {
		b.WriteString("\n")
		b.WriteString(cachedChart)
	}

	return b.String()
}

var chartColors = []string{
	"84",  // green
	"215", // orange
	"123", // cyan
	"141", // soft purple
	"228", // yellow
	"203", // red
	"75",  // sky blue
	"177", // lavender
	"43",  // mint
	"117", // pale cyan
	"183", // light purple
	"209", // light salmon
	"114", // seafoam
	"220", // golden
	"110", // light sky blue
}

func renderHistoryChart(series []api.TimeSeries, width int, start, end time.Time) string {
	chartWidth := width
	if chartWidth < 20 {
		chartWidth = 20
	}
	chartHeight := 12

	opts := []timeserieslinechart.Option{
		timeserieslinechart.WithTimeRange(start, end),
		timeserieslinechart.WithYRange(0, 100),
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
		points = forwardFillPoints(points, end)
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
		legend.WriteString(style.Render("━ " + s.Label))
	}

	return chart.View() + "\n" + legend.String()
}

// forwardFillPoints extends the last point's value to the given end time,
// preventing a "drop to zero" artifact when the chart time range extends
// beyond the last data point.
func forwardFillPoints(points []timeserieslinechart.TimePoint, end time.Time) []timeserieslinechart.TimePoint {
	if len(points) > 0 && points[len(points)-1].Time.Before(end) {
		points = append(points, timeserieslinechart.TimePoint{
			Time:  end,
			Value: points[len(points)-1].Value,
		})
	}
	return points
}

func historyHelp(rangeLabel string) string {
	return helpStyle.Render(fmt.Sprintf("< >: range [%s]  r: pick dates", rangeLabel))
}

// historyHelpInline returns help text styled for inclusion inside a panel (with spacing above).
func historyHelpInline(rangeLabel string) string {
	return "\n\n" + lipgloss.NewStyle().Foreground(colorDeepPurple).Render(
		fmt.Sprintf("< >:range [%s]  r:pick dates", rangeLabel))
}
