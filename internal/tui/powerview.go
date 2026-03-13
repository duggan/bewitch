package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ross/bewitch/internal/api"

	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/NimbleMarkets/ntcharts/sparkline"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

func renderPowerView(zones []api.PowerMetric, width int, cachedChart string, sparkData map[string][]float64, selected map[string]bool, cursor int) string {
	var b strings.Builder

	if zones == nil {
		return renderPanel("Power", dimStyle.Render("loading..."), width)
	}

	if len(zones) == 0 {
		return renderPanel("Power", valueStyle.Render("No power zones detected."), width)
	}

	sparkStyle := lipgloss.NewStyle().Foreground(colorLavender)
	cursorStyle := lipgloss.NewStyle().Foreground(colorPink).Bold(true)
	sparkW := 20
	if width < 100 {
		sparkW = 10
	}

	// Color mapping is no longer needed here - chart is pre-rendered
	colorMap := make(map[string]string)

	var rows [][]string
	for i, z := range zones {
		// Cursor + checkbox + zone name in single column
		prefix := "  "
		if i == cursor {
			prefix = cursorStyle.Render("> ")
		}
		check := "[ ] "
		if selected[z.Zone] {
			check = cursorStyle.Render("[x] ")
		}

		// Color zone name to match chart legend
		zoneStr := z.Zone
		if color, ok := colorMap[z.Zone]; ok {
			zoneStr = lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(z.Zone)
		} else {
			zoneStr = lipgloss.NewStyle().Foreground(colorMagenta).Render(z.Zone)
		}

		sparkStr := ""
		if vals, ok := sparkData[z.Zone]; ok && len(vals) > 0 {
			sl := sparkline.New(sparkW, 1,
				sparkline.WithData(vals),
				sparkline.WithStyle(sparkStyle),
			)
			sl.Draw()
			sparkStr = sl.View()
		}

		wattsStr := fmt.Sprintf("%.1fW", z.Watts)
		if z.Watts >= 300 {
			wattsStr = alertCritStyle.Render(wattsStr)
		} else if z.Watts >= 200 {
			wattsStr = alertWarnStyle.Render(wattsStr)
		} else {
			wattsStr = valueStyle.Render(wattsStr)
		}

		rows = append(rows, []string{prefix + check + zoneStr, sparkStr, wattsStr})
	}

	contentWidth := width - 4
	tbl := table.New().
		Rows(rows...).
		Width(contentWidth).
		Border(lipgloss.HiddenBorder())

	b.WriteString(renderPanel("Zones", tbl.Render()+"\n"+
		lipgloss.NewStyle().Foreground(colorDeepPurple).Render("↑↓:navigate  space:toggle  a:all  PgUp/Dn:scroll"), width))

	// History chart (pre-rendered)
	if cachedChart != "" {
		b.WriteString("\n")
		b.WriteString(cachedChart)
	}

	return b.String()
}

func renderPowerHistoryChart(series []api.TimeSeries, width int, start, end time.Time) string {
	maxVal := 0.0
	for _, s := range series {
		for _, p := range s.Points {
			if p.Value > maxVal {
				maxVal = p.Value
			}
		}
	}
	if maxVal < 50 {
		maxVal = 50
	}
	maxVal = float64(int(maxVal/10)+1) * 10

	chartWidth := width
	if chartWidth < 20 {
		chartWidth = 20
	}
	chartHeight := 12

	opts := []timeserieslinechart.Option{
		timeserieslinechart.WithTimeRange(start, end),
		timeserieslinechart.WithYRange(0, maxVal),
		timeserieslinechart.WithXLabelFormatter(xLabelFormatter(end.Sub(start))),
		timeserieslinechart.WithYLabelFormatter(func(_ int, v float64) string {
			if v >= 1000 {
				return fmt.Sprintf("%.1fkW", v/1000)
			}
			return fmt.Sprintf("%.0fW", v)
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

	var legend strings.Builder
	for i, s := range series {
		if len(s.Points) == 0 {
			continue
		}
		color := chartColors[i%len(chartColors)]
		lstyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		if i > 0 {
			legend.WriteString("  ")
		}
		legend.WriteString(lstyle.Render("━ " + s.Label))
	}

	return chart.View() + "\n" + legend.String()
}
