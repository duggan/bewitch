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

func renderGPUView(gpus []api.GPUMetric, width int, cachedChart string, sparkData map[string][]float64, selected map[string]bool, cursor int) string {
	var b strings.Builder

	if gpus == nil {
		return renderPanel("GPU", dimStyle.Render("loading..."), width)
	}

	if len(gpus) == 0 {
		return renderPanel("GPU", valueStyle.Render("No GPUs detected."), width)
	}

	sparkStyle := lipgloss.NewStyle().Foreground(colorLavender)
	cursorStyle := lipgloss.NewStyle().Foreground(colorPink).Bold(true)
	sparkW := 20
	if width < 100 {
		sparkW = 10
	}

	var rows [][]string
	for i, g := range gpus {
		// Cursor + checkbox + device name in single column
		prefix := "  "
		if i == cursor {
			prefix = cursorStyle.Render("> ")
		}
		check := "[ ] "
		if selected[g.Name] {
			check = cursorStyle.Render("[x] ")
		}

		nameStr := lipgloss.NewStyle().Foreground(colorMagenta).Render(g.Name)

		// Sparkline column
		sparkStr := ""
		if vals, ok := sparkData[g.Name]; ok && len(vals) > 0 {
			sl := sparkline.New(sparkW, 1,
				sparkline.WithData(vals),
				sparkline.WithStyle(sparkStyle),
			)
			sl.Draw()
			sparkStr = sl.View()
		}

		// Utilization
		utilStr := fmt.Sprintf("%.1f%%", g.UtilizationPct)
		if g.UtilizationPct >= 95 {
			utilStr = alertCritStyle.Render(utilStr)
		} else if g.UtilizationPct >= 80 {
			utilStr = alertWarnStyle.Render(utilStr)
		} else {
			utilStr = valueStyle.Render(utilStr)
		}

		// Frequency
		freqStr := fmt.Sprintf("%dMHz", g.FrequencyMHz)
		if g.FrequencyMaxMHz > 0 {
			freqStr = fmt.Sprintf("%d/%dMHz", g.FrequencyMHz, g.FrequencyMaxMHz)
		}
		freqStr = valueStyle.Render(freqStr)

		// Build detail parts
		var details []string
		details = append(details, utilStr, freqStr)

		if g.PowerWatts > 0 {
			details = append(details, valueStyle.Render(fmt.Sprintf("%.1fW", g.PowerWatts)))
		}
		if g.MemoryTotalBytes > 0 {
			memPct := float64(g.MemoryUsedBytes) / float64(g.MemoryTotalBytes) * 100
			details = append(details, valueStyle.Render(fmt.Sprintf("%s/%s (%.0f%%)",
				humanBytes(g.MemoryUsedBytes), humanBytes(g.MemoryTotalBytes), memPct)))
		}
		if g.TempCelsius > 0 {
			tempStr := fmt.Sprintf("%.0f°C", g.TempCelsius)
			if g.TempCelsius >= 90 {
				tempStr = alertCritStyle.Render(tempStr)
			} else if g.TempCelsius >= 75 {
				tempStr = alertWarnStyle.Render(tempStr)
			} else {
				tempStr = valueStyle.Render(tempStr)
			}
			details = append(details, tempStr)
		}

		rows = append(rows, []string{prefix + check + nameStr, sparkStr, strings.Join(details, "  ")})
	}

	contentWidth := width - 4
	tbl := table.New().
		Rows(rows...).
		Width(contentWidth).
		Border(lipgloss.HiddenBorder())

	b.WriteString(renderPanel("Devices", tbl.Render()+"\n"+
		lipgloss.NewStyle().Foreground(colorDeepPurple).Render("↑↓:navigate  space:toggle  a:all  PgUp/Dn:scroll"), width))

	// History chart (pre-rendered)
	if cachedChart != "" {
		b.WriteString("\n")
		b.WriteString(cachedChart)
	}

	return b.String()
}

func renderGPUHistoryChart(series []api.TimeSeries, width, height int, start, end time.Time) string {
	return renderBrailleChart(chartConfig{
		series:     series,
		width:      width,
		height:     height,
		start:      start,
		end:        end,
		yMin:       0,
		yMax:       autoMaxY(series, 100, 10),
		yFormatter: yFmtPercent,
	})
}
