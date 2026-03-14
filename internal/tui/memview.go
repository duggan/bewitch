package tui

import (
	"fmt"
	"strings"

	"github.com/duggan/bewitch/internal/api"
)

func renderMemView(m *api.MemoryMetric, width int, cachedChart string) string {
	var b strings.Builder
	barWidth := width - 44 // label(14) + value text(~26) + panel border+padding(4)
	if barWidth < 10 {
		barWidth = 10
	}

	if m == nil {
		return renderPanel("Memory Details", dimStyle.Render("loading..."), width)
	}

	// RAM panel
	var ram strings.Builder
	if m.TotalBytes > 0 {
		usedPct := float64(m.UsedBytes) / float64(m.TotalBytes) * 100
		ram.WriteString(labelStyle.Render("used") + renderBar(usedPct, barWidth) + valueStyle.Render(fmt.Sprintf(" %s / %s (%.1f%%)", humanBytes(m.UsedBytes), humanBytes(m.TotalBytes), usedPct)) + "\n")
		ram.WriteString(labelStyle.Render("available") + valueStyle.Render(humanBytes(m.AvailableBytes)) + "\n")
		ram.WriteString(labelStyle.Render("buffers") + valueStyle.Render(humanBytes(m.BuffersBytes)) + "\n")
		ram.WriteString(labelStyle.Render("cached") + valueStyle.Render(humanBytes(m.CachedBytes)))
	}
	b.WriteString(renderPanel("RAM", ram.String(), width))

	// Swap panel
	if m.SwapTotalBytes > 0 {
		var swap strings.Builder
		swapPct := float64(m.SwapUsedBytes) / float64(m.SwapTotalBytes) * 100
		swap.WriteString(labelStyle.Render("used") + renderBar(swapPct, barWidth) + valueStyle.Render(fmt.Sprintf(" %s / %s (%.1f%%)", humanBytes(m.SwapUsedBytes), humanBytes(m.SwapTotalBytes), swapPct)))
		b.WriteString("\n" + renderPanel("Swap", swap.String(), width))
	}

	// History chart (pre-rendered)
	if cachedChart != "" {
		b.WriteString("\n")
		b.WriteString(cachedChart)
	}

	return b.String()
}
