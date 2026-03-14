package tui

import (
	"fmt"
	"strings"

	"github.com/duggan/bewitch/internal/api"

	"github.com/charmbracelet/lipgloss"
)

func renderHardwareView(
	temps []api.TemperatureMetric,
	zones []api.PowerMetric,
	ecc *api.ECCMetric,
	width int,
	cachedChart string,
	tempSparkData map[string][]float64,
	tempSelected map[string]bool,
	tempCursor int,
	powerSparkData map[string][]float64,
	powerSelected map[string]bool,
	powerCursor int,
	activeSection int,
) string {
	var b strings.Builder

	// Sub-tab bar
	hasTemp := len(temps) > 0
	hasPower := len(zones) > 0
	hasECC := ecc != nil

	sectionCount := 0
	if hasTemp {
		sectionCount++
	}
	if hasPower {
		sectionCount++
	}
	if hasECC {
		sectionCount++
	}

	// Only show sub-tab bar if multiple sections have data
	if sectionCount > 1 {
		b.WriteString(renderHardwareSubTabs(activeSection, hasTemp, hasPower, hasECC, width))
		b.WriteString("\n")
	}

	switch activeSection {
	case hwSectionTemp:
		if !hasTemp {
			b.WriteString(renderPanel("Temperature", dimStyle.Render("No temperature sensors detected."), width))
		} else {
			b.WriteString(renderTempView(temps, width, cachedChart, tempSparkData, tempSelected, tempCursor))
		}
	case hwSectionPower:
		if !hasPower {
			b.WriteString(renderPanel("Power", dimStyle.Render("No power zones detected."), width))
		} else {
			b.WriteString(renderPowerView(zones, width, cachedChart, powerSparkData, powerSelected, powerCursor))
		}
	case hwSectionECC:
		b.WriteString(renderECCView(ecc, width))
	}

	return b.String()
}

func renderHardwareSubTabs(active int, hasTemp, hasPower, hasECC bool, width int) string {
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(colorPink)
	inactiveStyle := lipgloss.NewStyle().Foreground(colorDeepPurple)
	dimmedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	type subTab struct {
		label   string
		section int
		hasData bool
	}
	tabs := []subTab{
		{"Temperature", hwSectionTemp, hasTemp},
		{"Power", hwSectionPower, hasPower},
		{"ECC", hwSectionECC, hasECC},
	}

	var parts []string
	for _, t := range tabs {
		style := inactiveStyle
		if !t.hasData {
			style = dimmedStyle
		}
		if t.section == active {
			style = activeStyle
		}
		parts = append(parts, style.Render(t.label))
	}

	bar := strings.Join(parts, inactiveStyle.Render("  │  "))
	help := lipgloss.NewStyle().Foreground(colorDeepPurple).Render("  tab:switch section")
	return bar + help
}

func renderECCView(ecc *api.ECCMetric, width int) string {
	if ecc == nil {
		return renderPanel("ECC Memory", dimStyle.Render("No ECC memory detected."), width)
	}

	var b strings.Builder
	if ecc.Corrected == 0 && ecc.Uncorrected == 0 {
		b.WriteString(labelStyle.Render("status") + valueStyle.Render("ok"))
	} else {
		b.WriteString(labelStyle.Render("corrected") + valueStyle.Render(fmt.Sprintf("%d", ecc.Corrected)) + "\n")
		if ecc.Uncorrected > 0 {
			b.WriteString(labelStyle.Render("uncorrected") + alertCritStyle.Render(fmt.Sprintf("%d", ecc.Uncorrected)))
		} else {
			b.WriteString(labelStyle.Render("uncorrected") + valueStyle.Render("0"))
		}
	}

	return renderPanel("ECC Memory", b.String(), width)
}
