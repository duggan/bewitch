package tui

import (
	"fmt"
	"strings"

	"github.com/duggan/bewitch/internal/api"

	"github.com/NimbleMarkets/ntcharts/sparkline"
	"github.com/charmbracelet/lipgloss"
)

func renderDashboard(dash *api.DashboardData, width int, sparkData map[string][]float64, netSelected, tempSelected, powerSelected map[string]bool) string {
	if dash == nil {
		return dimStyle.Render("loading...")
	}

	// Determine layout mode and widths
	sideBySide := width > 120
	halfWidth := width / 2

	// Layout: side-by-side on wide terminals, stacked on narrow
	if sideBySide {
		// Build panels for side-by-side sections
		cpuPanel := buildCPUPanel(dash, halfWidth, sparkData)
		memPanel := buildMemPanel(dash, halfWidth, sparkData)
		diskPanel := buildDiskPanel(dash, halfWidth)
		netPanel := buildNetPanel(dash, halfWidth, netSelected)

		row1 := lipgloss.JoinHorizontal(lipgloss.Top,
			renderPanel("CPU", cpuPanel, halfWidth),
			renderPanel("Memory", memPanel, halfWidth),
		)
		row2 := lipgloss.JoinHorizontal(lipgloss.Top,
			renderPanel("Disk", diskPanel, halfWidth),
			renderPanel("Network", netPanel, halfWidth),
		)

		// Use halfWidth*2 to match combined width of side-by-side panels
		fullWidth := halfWidth * 2

		var b strings.Builder
		b.WriteString(row1 + "\n" + row2)

		// Build temp/power with same total width as the rows above
		tempPanel := buildTempPanel(dash, fullWidth, tempSelected)
		if tempPanel != "" {
			b.WriteString("\n" + renderPanel("Temperature", tempPanel, fullWidth))
		}
		powerPanel := buildPowerPanel(dash, fullWidth, powerSelected)
		if powerPanel != "" {
			b.WriteString("\n" + renderPanel("Power", powerPanel, fullWidth))
		}
		procPanel := buildProcessPanel(dash, fullWidth)
		if procPanel != "" {
			b.WriteString("\n" + renderPanel("Processes", procPanel, fullWidth))
		}
		return b.String()
	}

	// Stacked layout
	cpuPanel := buildCPUPanel(dash, width, sparkData)
	memPanel := buildMemPanel(dash, width, sparkData)
	diskPanel := buildDiskPanel(dash, width)
	netPanel := buildNetPanel(dash, width, netSelected)
	tempPanel := buildTempPanel(dash, width, tempSelected)
	powerPanel := buildPowerPanel(dash, width, powerSelected)

	var b strings.Builder
	b.WriteString(renderPanel("CPU", cpuPanel, width) + "\n")
	b.WriteString(renderPanel("Memory", memPanel, width) + "\n")
	b.WriteString(renderPanel("Disk", diskPanel, width) + "\n")
	b.WriteString(renderPanel("Network", netPanel, width))
	if tempPanel != "" {
		b.WriteString("\n" + renderPanel("Temperature", tempPanel, width))
	}
	if powerPanel != "" {
		b.WriteString("\n" + renderPanel("Power", powerPanel, width))
	}
	procPanel := buildProcessPanel(dash, width)
	if procPanel != "" {
		b.WriteString("\n" + renderPanel("Processes", procPanel, width))
	}
	return b.String()
}

func buildCPUPanel(dash *api.DashboardData, width int, sparkData map[string][]float64) string {
	var b strings.Builder

	// Summary stats
	var maxPct, totalPct float64
	numCores := 0
	var aggCore *api.CPUCoreMetric
	for i, c := range dash.CPU {
		if c.Core == -1 {
			aggCore = &dash.CPU[i]
			continue
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
	summaryText := fmt.Sprintf("%d cores  avg %.1f%%  max %.1f%%", numCores, avgPct, maxPct)

	// Sparkline inline with summary
	if vals, ok := sparkData["cpu"]; ok && len(vals) > 0 {
		sparkStyle := lipgloss.NewStyle().Foreground(colorLavender)
		sparkW := width - 24 - lipgloss.Width(summaryText) - 2
		if sparkW < 10 {
			sparkW = 10
		}
		if sparkW > 30 {
			sparkW = 30
		}
		sl := sparkline.New(sparkW, 1,
			sparkline.WithData(vals),
			sparkline.WithStyle(sparkStyle),
		)
		sl.Draw()
		sparkView := strings.TrimLeft(sl.View(), " ")
		b.WriteString(summaryStyle.Render(summaryText) + " " + sparkView + "\n")
	} else {
		b.WriteString(summaryStyle.Render(summaryText) + "\n")
	}

	// Usage bar
	cpuPct := 0.0
	if aggCore != nil {
		cpuPct = aggCore.UserPct + aggCore.SystemPct
	}
	barWidth := width - 24
	if barWidth < 10 {
		barWidth = 10
	}
	if barWidth > 40 {
		barWidth = 40
	}
	b.WriteString(labelStyle.Render("usage") + renderBar(cpuPct, barWidth) + valueStyle.Render(fmt.Sprintf(" %.1f%%", cpuPct)) + "\n")

	// Breakdown
	if aggCore != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(
			fmt.Sprintf("usr:%.1f  sys:%.1f  io:%.1f", aggCore.UserPct, aggCore.SystemPct, aggCore.IOWaitPct)))
	}

	return b.String()
}

func buildMemPanel(dash *api.DashboardData, width int, sparkData map[string][]float64) string {
	if dash.Memory == nil || dash.Memory.TotalBytes == 0 {
		return valueStyle.Render("no data")
	}
	var b strings.Builder
	m := dash.Memory
	barWidth := width - 24
	if barWidth < 10 {
		barWidth = 10
	}
	if barWidth > 40 {
		barWidth = 40
	}

	// RAM bar
	memPct := float64(m.UsedBytes) / float64(m.TotalBytes) * 100
	b.WriteString(labelStyle.Render("ram") + renderBar(memPct, barWidth) + valueStyle.Render(fmt.Sprintf(" %.1f%%  %s/%s", memPct, humanBytes(m.UsedBytes), humanBytes(m.TotalBytes))) + "\n")

	// Swap bar
	if m.SwapTotalBytes > 0 {
		swapPct := float64(m.SwapUsedBytes) / float64(m.SwapTotalBytes) * 100
		b.WriteString(labelStyle.Render("swap") + renderBar(swapPct, barWidth) + valueStyle.Render(fmt.Sprintf(" %.1f%%  %s/%s", swapPct, humanBytes(m.SwapUsedBytes), humanBytes(m.SwapTotalBytes))) + "\n")
	}

	// Buffers + cached inline with sparkline
	bufCacheText := fmt.Sprintf("buffers %s  cached %s", humanBytes(m.BuffersBytes), humanBytes(m.CachedBytes))
	if vals, ok := sparkData["mem"]; ok && len(vals) > 0 {
		sparkStyle := lipgloss.NewStyle().Foreground(colorLavender)
		sparkW := width - 24 - lipgloss.Width(bufCacheText) - 2
		if sparkW < 10 {
			sparkW = 10
		}
		if sparkW > 30 {
			sparkW = 30
		}
		sl := sparkline.New(sparkW, 1,
			sparkline.WithData(vals),
			sparkline.WithStyle(sparkStyle),
		)
		sl.Draw()
		sparkView := strings.TrimLeft(sl.View(), " ")
		b.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(bufCacheText) + " " + sparkView)
	} else {
		b.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(bufCacheText))
	}

	return b.String()
}

func buildDiskPanel(dash *api.DashboardData, width int) string {
	if len(dash.Disks) == 0 {
		return valueStyle.Render("no data")
	}
	barWidth := width - 24
	if barWidth < 10 {
		barWidth = 10
	}
	if barWidth > 40 {
		barWidth = 40
	}

	var b strings.Builder
	for i, d := range dash.Disks {
		pct := 0.0
		if d.TotalBytes > 0 {
			pct = float64(d.UsedBytes) / float64(d.TotalBytes) * 100
		}
		label := d.Mount
		if len(label) > 12 {
			label = label[:12]
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(labelStyle.Render(label) + renderBar(pct, barWidth) + valueStyle.Render(fmt.Sprintf(" %.1f%%  %s", pct, humanBytes(d.TotalBytes))))
		// I/O rates on second line (only if nonzero)
		if d.ReadBytesSec > 0 || d.WriteBytesSec > 0 {
			b.WriteString("\n" + lipgloss.NewStyle().Foreground(colorMuted).PaddingLeft(14).Render(
				fmt.Sprintf("r:%s/s  w:%s/s", humanBytes(uint64(d.ReadBytesSec)), humanBytes(uint64(d.WriteBytesSec)))))
		}
	}
	return b.String()
}

func buildNetPanel(dash *api.DashboardData, width int, selected map[string]bool) string {
	if len(dash.Network) == 0 {
		return valueStyle.Render("no data")
	}
	// Filter to selected interfaces (or all if no selection)
	var interfaces []api.NetworkMetric
	for _, n := range dash.Network {
		if len(selected) == 0 || selected[n.Interface] {
			interfaces = append(interfaces, n)
		}
	}
	if len(interfaces) == 0 {
		return dimStyle.Render("no interfaces selected")
	}

	var b strings.Builder
	for i, n := range interfaces {
		if i > 0 {
			b.WriteString("\n")
		}
		name := n.Interface
		if len(name) > 14 {
			name = name[:14]
		}
		isIdle := n.RxBytesSec == 0 && n.TxBytesSec == 0
		if isIdle {
			b.WriteString(dimStyle.Render(fmt.Sprintf("%-14s ↓ 0/s  ↑ 0/s", name)))
		} else {
			b.WriteString(labelStyle.Render(name) +
				rxArrowStyle.Render("↓") + valueStyle.Render(fmt.Sprintf(" %s/s  ", humanBytes(uint64(n.RxBytesSec)))) +
				txArrowStyle.Render("↑") + valueStyle.Render(fmt.Sprintf(" %s/s", humanBytes(uint64(n.TxBytesSec)))))
		}
	}
	return b.String()
}

func buildTempPanel(dash *api.DashboardData, width int, selected map[string]bool) string {
	if len(dash.Temperature) == 0 {
		return ""
	}
	// Horizontal flow layout with color-coded temperatures
	var parts []string
	for _, t := range dash.Temperature {
		if len(selected) > 0 && !selected[t.Sensor] {
			continue
		}
		name := t.Sensor
		if len(name) > 16 {
			name = name[:16]
		}
		tempStr := fmt.Sprintf("%.0f°C", t.TempCelsius)
		if t.TempCelsius >= 80 {
			tempStr = alertCritStyle.Render(tempStr)
		} else if t.TempCelsius >= 60 {
			tempStr = alertWarnStyle.Render(tempStr)
		} else {
			tempStr = valueStyle.Render(tempStr)
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(colorMagenta).Render(name)+" "+tempStr)
	}
	// Join with spacing, wrapping based on width
	contentWidth := width - 6
	var lines []string
	line := ""
	for _, part := range parts {
		candidate := line
		if candidate != "" {
			candidate += "  "
		}
		candidate += part
		if lipgloss.Width(candidate) > contentWidth && line != "" {
			lines = append(lines, line)
			line = part
		} else {
			line = candidate
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func buildPowerPanel(dash *api.DashboardData, width int, selected map[string]bool) string {
	if len(dash.Power) == 0 {
		return ""
	}
	var parts []string
	for _, p := range dash.Power {
		if len(selected) > 0 && !selected[p.Zone] {
			continue
		}
		name := p.Zone
		if len(name) > 16 {
			name = name[:16]
		}
		wattsStr := fmt.Sprintf("%.0fW", p.Watts)
		if p.Watts >= 300 {
			wattsStr = alertCritStyle.Render(wattsStr)
		} else if p.Watts >= 200 {
			wattsStr = alertWarnStyle.Render(wattsStr)
		} else {
			wattsStr = valueStyle.Render(wattsStr)
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(colorMagenta).Render(name)+" "+wattsStr)
	}
	contentWidth := width - 6
	var lines []string
	line := ""
	for _, part := range parts {
		candidate := line
		if candidate != "" {
			candidate += "  "
		}
		candidate += part
		if lipgloss.Width(candidate) > contentWidth && line != "" {
			lines = append(lines, line)
			line = part
		} else {
			line = candidate
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func buildProcessPanel(dash *api.DashboardData, width int) string {
	if dash.Processes == nil || len(dash.Processes.Processes) == 0 {
		return ""
	}
	var b strings.Builder

	// Summary line (matches processview.go style)
	b.WriteString(summaryStyle.Render(fmt.Sprintf(
		"%d procs | %d active | %d running | CPU: %.1f%% | Mem: %s",
		dash.Processes.TotalProcs, dash.Processes.ActiveProcs, dash.Processes.RunningProcs,
		dash.Processes.TotalCPUPct, humanBytes(dash.Processes.TotalRSSBytes))) + "\n")

	// Compact table - name, CPU%, memory
	nameWidth := 16
	if width >= 100 {
		nameWidth = 20
	}

	for _, p := range dash.Processes.Processes {
		name := p.Name
		if len(name) > nameWidth {
			name = name[:nameWidth-1] + "…"
		}
		cpuPct := p.CPUUserPct + p.CPUSystemPct
		row := fmt.Sprintf("%-*s %6.1f%% %8s", nameWidth, name, cpuPct, humanBytes(p.RSSBytes))

		// Highlight running processes in purple
		if p.State == "R" {
			b.WriteString(lipgloss.NewStyle().Foreground(colorPurple).Render(row) + "\n")
		} else {
			b.WriteString(valueStyle.Render(row) + "\n")
		}
	}

	return strings.TrimSuffix(b.String(), "\n")
}
