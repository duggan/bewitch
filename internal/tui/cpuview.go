package tui

import (
	"fmt"
	"strings"

	"github.com/duggan/bewitch/internal/api"
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

