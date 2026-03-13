package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ross/bewitch/internal/api"
)

func renderDiskView(disks []api.DiskMetric, width int, cachedChart string) string {
	var b strings.Builder
	barWidth := width - 44 // label(14) + value text(~26) + panel border+padding(4)
	if barWidth < 10 {
		barWidth = 10
	}

	if disks == nil {
		return renderPanel("Disk Details", dimStyle.Render("loading..."), width)
	}

	for i, d := range disks {
		pct := 0.0
		if d.TotalBytes > 0 {
			pct = float64(d.UsedBytes) / float64(d.TotalBytes) * 100
		}

		var disk strings.Builder
		disk.WriteString(labelStyle.Render("space") + renderBar(pct, barWidth) + valueStyle.Render(fmt.Sprintf(" %.1f%% (%s / %s)", pct, humanBytes(d.UsedBytes), humanBytes(d.TotalBytes))) + "\n")
		disk.WriteString(labelStyle.Render("read") + valueStyle.Render(fmt.Sprintf("%s/s  (%.0f IOPS)", humanBytes(uint64(d.ReadBytesSec)), d.ReadIOPS)) + "\n")
		disk.WriteString(labelStyle.Render("write") + valueStyle.Render(fmt.Sprintf("%s/s  (%.0f IOPS)", humanBytes(uint64(d.WriteBytesSec)), d.WriteIOPS)))

		if d.SMARTAvailable {
			disk.WriteString("\n")
			disk.WriteString(labelStyle.Render("health") + renderSMARTHealth(d))
			disk.WriteString("\n")
			disk.WriteString(labelStyle.Render("lifetime") + renderSMARTLifetime(d))
			if errLine := renderSMARTErrors(d); errLine != "" {
				disk.WriteString("\n")
				disk.WriteString(labelStyle.Render("errors") + errLine)
			}
		}

		if i > 0 {
			b.WriteString("\n")
		}
		var title string
		if d.Transport != "" {
			title = fmt.Sprintf("%s (%s, %s)", d.Mount, d.Device, d.Transport)
		} else {
			title = fmt.Sprintf("%s (%s)", d.Mount, d.Device)
		}
		b.WriteString(renderPanel(title, disk.String(), width))
	}

	// History chart (pre-rendered)
	if cachedChart != "" {
		b.WriteString("\n")
		b.WriteString(cachedChart)
	}

	return b.String()
}

var (
	smartOKStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorGreen)
	smartFailStyle = lipgloss.NewStyle().Bold(true).Foreground(colorRed)
)

func renderSMARTHealth(d api.DiskMetric) string {
	var parts []string
	if d.SMARTHealthy {
		parts = append(parts, smartOKStyle.Render("OK"))
	} else {
		parts = append(parts, smartFailStyle.Render("FAILING"))
	}
	if d.SMARTTemperature > 0 {
		parts = append(parts, valueStyle.Render(fmt.Sprintf("temp %d°C", d.SMARTTemperature)))
	}
	if d.SMARTAvailableSpare > 0 {
		parts = append(parts, valueStyle.Render(fmt.Sprintf("spare %d%%", d.SMARTAvailableSpare)))
	}
	if d.SMARTPercentUsed > 0 {
		parts = append(parts, valueStyle.Render(fmt.Sprintf("used %d%%", d.SMARTPercentUsed)))
	}
	return strings.Join(parts, "  ")
}

func renderSMARTLifetime(d api.DiskMetric) string {
	var parts []string
	parts = append(parts, valueStyle.Render(fmt.Sprintf("%s hrs", humanCount(d.SMARTPowerOnHours))))
	if d.SMARTPowerCycles > 0 {
		parts = append(parts, valueStyle.Render(fmt.Sprintf("cycles %s", humanCount(d.SMARTPowerCycles))))
	}
	return strings.Join(parts, "  ")
}

func renderSMARTErrors(d api.DiskMetric) string {
	// Only show error line when SATA-like attributes are present
	if d.SMARTReallocated == 0 && d.SMARTPending == 0 && d.SMARTUncorrectable == 0 && d.SMARTReadErrorRate == 0 {
		return ""
	}
	var parts []string
	parts = append(parts, renderSmartCounter("realloc", d.SMARTReallocated))
	parts = append(parts, renderSmartCounter("pending", d.SMARTPending))
	parts = append(parts, renderSmartCounter("uncorrect", d.SMARTUncorrectable))
	return strings.Join(parts, "  ")
}

func renderSmartCounter(label string, count uint64) string {
	s := fmt.Sprintf("%s %d", label, count)
	if count > 0 {
		return alertWarnStyle.Render(s)
	}
	return valueStyle.Render(s)
}

func humanCount(n uint64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
