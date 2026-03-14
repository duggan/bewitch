package tui

import (
	"strings"
	"time"

	"github.com/duggan/bewitch/internal/api"

	"github.com/NimbleMarkets/ntcharts/sparkline"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

func renderNetView(ifaces []api.NetworkMetric, width int, cachedChart string, sparkData map[string][]float64, selected map[string]bool, cursor int, ifaceNames []string, displayBits bool) string {
	var b strings.Builder

	if ifaces == nil {
		return renderPanel("Interfaces", dimStyle.Render("loading..."), width)
	}

	if len(ifaces) == 0 {
		return renderPanel("Interfaces", valueStyle.Render("No interfaces detected."), width)
	}

	sparkStyle := lipgloss.NewStyle().Foreground(colorLavender)
	cursorStyle := lipgloss.NewStyle().Foreground(colorPink).Bold(true)
	sparkW := 14
	if width < 100 {
		sparkW = 8
	}

	// Color mapping is no longer needed here - chart is pre-rendered
	// Interface name coloring is based on selection state only
	rxColorMap := make(map[string]string)
	txColorMap := make(map[string]string)

	var rows [][]string
	for i, n := range ifaces {
		// Cursor + checkbox + name in single column to reduce spacing
		prefix := "  "
		if i == cursor {
			prefix = cursorStyle.Render("> ")
		}
		check := "[ ] "
		if selected[n.Interface] {
			check = cursorStyle.Render("[x] ")
		}

		// Determine if interface is idle (all sparkline values zero)
		idle := true
		rxKey := n.Interface + "_rx"
		txKey := n.Interface + "_tx"
		if vals, ok := sparkData[rxKey]; ok {
			for _, v := range vals {
				if v > 0 {
					idle = false
					break
				}
			}
		}
		if idle {
			if vals, ok := sparkData[txKey]; ok {
				for _, v := range vals {
					if v > 0 {
						idle = false
						break
					}
				}
			}
		}

		// RX sparkline
		rxSparkStr := ""
		if vals, ok := sparkData[rxKey]; ok && len(vals) > 0 {
			sl := sparkline.New(sparkW, 1,
				sparkline.WithData(vals),
				sparkline.WithStyle(sparkStyle),
			)
			sl.Draw()
			rxSparkStr = sl.View()
		}

		// TX sparkline
		txSparkStr := ""
		if vals, ok := sparkData[txKey]; ok && len(vals) > 0 {
			sl := sparkline.New(sparkW, 1,
				sparkline.WithData(vals),
				sparkline.WithStyle(sparkStyle),
			)
			sl.Draw()
			txSparkStr = sl.View()
		}

		// Format rates
		var rxRate, txRate string
		if displayBits {
			rxRate = humanBits(uint64(n.RxBytesSec))
			txRate = humanBits(uint64(n.TxBytesSec))
		} else {
			rxRate = humanBytes(uint64(n.RxBytesSec)) + "/s"
			txRate = humanBytes(uint64(n.TxBytesSec)) + "/s"
		}

		// Interface name with colored arrows indicating rx/tx chart colors
		nameStr := n.Interface
		if n.RxErrors > 0 || n.TxErrors > 0 {
			nameStr = alertWarnStyle.Render(nameStr)
		} else {
			nameStr = lipgloss.NewStyle().Foreground(colorMagenta).Render(nameStr)
		}
		// Add colored arrows if interface is selected (has chart colors)
		if rxColor, ok := rxColorMap[n.Interface]; ok {
			rxArrow := lipgloss.NewStyle().Foreground(lipgloss.Color(rxColor)).Render("↓")
			txArrow := ""
			if txColor, ok := txColorMap[n.Interface]; ok {
				txArrow = lipgloss.NewStyle().Foreground(lipgloss.Color(txColor)).Render("↑")
			}
			nameStr = rxArrow + txArrow + " " + nameStr
		}

		rxCol := rxArrowStyle.Render("↓") + " " + rxSparkStr + "  " + rxRate
		txCol := txArrowStyle.Render("↑") + " " + txSparkStr + "  " + txRate

		if idle {
			rxCol = dimStyle.Render("↓ " + rxSparkStr + "  " + rxRate)
			txCol = dimStyle.Render("↑ " + txSparkStr + "  " + txRate)
			if n.RxErrors == 0 && n.TxErrors == 0 {
				nameStr = dimStyle.Render(n.Interface)
			}
		}

		rows = append(rows, []string{prefix + check + nameStr, rxCol, txCol})
	}

	contentWidth := width - 4
	tbl := table.New().
		Rows(rows...).
		Width(contentWidth).
		Border(lipgloss.HiddenBorder())

	// Build help line with active-state highlighting
	normalHelp := lipgloss.NewStyle().Foreground(colorDeepPurple)
	activeHelp := lipgloss.NewStyle().Foreground(colorPink).Bold(true)
	bitsLabel := "b:bytes"
	if displayBits {
		bitsLabel = "b:bits"
	}
	type helpItem struct {
		text   string
		active bool
	}
	helpItems := []helpItem{
		{"↑↓:navigate", false},
		{"space:toggle", false},
		{"a:all", false},
		{bitsLabel, displayBits},
		{"PgUp/Dn:scroll", false},
	}
	var helpParts []string
	for _, item := range helpItems {
		if item.active {
			helpParts = append(helpParts, activeHelp.Render(item.text))
		} else {
			helpParts = append(helpParts, normalHelp.Render(item.text))
		}
	}
	helpLine := strings.Join(helpParts, normalHelp.Render("  "))

	b.WriteString(renderPanel("Interfaces", tbl.Render()+"\n"+helpLine, width))

	// History chart (pre-rendered)
	if cachedChart != "" {
		b.WriteString("\n")
		b.WriteString(cachedChart)
	}

	return b.String()
}

func renderNetHistoryChart(series []api.TimeSeries, width, height int, start, end time.Time, displayBits bool) string {
	yFmt := yFmtNetBytes
	if displayBits {
		yFmt = yFmtNetBits
	}
	return renderBrailleChart(chartConfig{
		series:         series,
		width:          width,
		height:         height,
		start:          start,
		end:            end,
		yMin:           0,
		yMax:           autoMaxY(series, 1024, 0),
		yFormatter:     yFmt,
		labelTransform: netLabelTransform,
	})
}
