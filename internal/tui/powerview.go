package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/duggan/bewitch/internal/api"

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

func renderPowerHistoryChart(series []api.TimeSeries, width, height int, start, end time.Time) string {
	return renderBrailleChart(chartConfig{
		series:     series,
		width:      width,
		height:     height,
		start:      start,
		end:        end,
		yMin:       0,
		yMax:       autoMaxY(series, 50, 10),
		yFormatter: yFmtWatts,
	})
}
