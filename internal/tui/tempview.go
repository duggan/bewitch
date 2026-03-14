package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ross/bewitch/internal/api"

	"github.com/NimbleMarkets/ntcharts/sparkline"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

func renderTempView(temps []api.TemperatureMetric, width int, cachedChart string, sparkData map[string][]float64, selected map[string]bool, cursor int) string {
	var b strings.Builder

	if temps == nil {
		return renderPanel("Temperature", dimStyle.Render("loading..."), width)
	}

	if len(temps) == 0 {
		return renderPanel("Temperature", valueStyle.Render("No sensors detected."), width)
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
	for i, t := range temps {
		// Cursor + checkbox + sensor name in single column
		prefix := "  "
		if i == cursor {
			prefix = cursorStyle.Render("> ")
		}
		check := "[ ] "
		if selected[t.Sensor] {
			check = cursorStyle.Render("[x] ")
		}

		// Color sensor name to match chart legend
		sensorStr := t.Sensor
		if color, ok := colorMap[t.Sensor]; ok {
			sensorStr = lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(t.Sensor)
		} else {
			sensorStr = lipgloss.NewStyle().Foreground(colorMagenta).Render(t.Sensor)
		}

		// Sparkline column
		sparkStr := ""
		if vals, ok := sparkData[t.Sensor]; ok && len(vals) > 0 {
			sl := sparkline.New(sparkW, 1,
				sparkline.WithData(vals),
				sparkline.WithStyle(sparkStyle),
			)
			sl.Draw()
			sparkStr = sl.View()
		}

		// Temperature value
		tempStr := fmt.Sprintf("%.1f°C", t.TempCelsius)
		if t.TempCelsius >= 80 {
			tempStr = alertCritStyle.Render(tempStr)
		} else if t.TempCelsius >= 60 {
			tempStr = alertWarnStyle.Render(tempStr)
		} else {
			tempStr = valueStyle.Render(tempStr)
		}

		rows = append(rows, []string{prefix + check + sensorStr, sparkStr, tempStr})
	}

	contentWidth := width - 4 // panel border padding
	tbl := table.New().
		Rows(rows...).
		Width(contentWidth).
		Border(lipgloss.HiddenBorder())

	b.WriteString(renderPanel("Sensors", tbl.Render()+"\n"+
		lipgloss.NewStyle().Foreground(colorDeepPurple).Render("↑↓:navigate  space:toggle  a:all  PgUp/Dn:scroll"), width))

	// History chart (pre-rendered)
	if cachedChart != "" {
		b.WriteString("\n")
		b.WriteString(cachedChart)
	}

	return b.String()
}

func renderTempHistoryChart(series []api.TimeSeries, width, height int, start, end time.Time) string {
	return renderBrailleChart(chartConfig{
		series:     series,
		width:      width,
		height:     height,
		start:      start,
		end:        end,
		yMin:       0,
		yMax:       autoMaxY(series, 50, 10),
		yFormatter: yFmtCelsius,
	})
}
