package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/duggan/bewitch/internal/api"
)

var (
	notifyOkStyle   = lipgloss.NewStyle().Foreground(colorGreen)
	notifyErrStyle  = lipgloss.NewStyle().Foreground(colorRed)
	notifyDestStyle = lipgloss.NewStyle().Foreground(colorPurple)
	notifyDimStyle  = lipgloss.NewStyle().Foreground(colorMuted)
)

func renderAlertView(alerts []api.AlertMetric, width int, alertTable *table.Model, rules []api.AlertRuleMetric, ruleCursor int, alertFocus int, notifyLog []notifyLogEntry, notifySending bool, confirmDelete bool, confirmName string, formErr string) string {
	var sections []string

	// --- Rules section ---
	rulesContent := renderRulesSection(rules, ruleCursor, width, alertFocus == 0)
	sections = append(sections, rulesContent)

	// --- Selected rule detail ---
	if ruleCursor >= 0 && ruleCursor < len(rules) {
		sections = append(sections, renderRuleDetail(rules[ruleCursor], width))
	}

	// --- Fired alerts section ---
	if alerts == nil {
		sections = append(sections, renderPanel("Fired Alerts", dimStyle.Render("loading..."), width))
	} else if len(alerts) == 0 {
		sections = append(sections, renderPanel("Fired Alerts", valueStyle.Render("No alerts."), width))
	} else {
		rows := make([]table.Row, len(alerts))
		for i, a := range alerts {
			status := ""
			if a.Acknowledged {
				status = "ack"
			}
			rows[i] = table.Row{
				a.Timestamp.Format("Jan 02 15:04"),
				a.Severity,
				a.RuleName,
				a.Message,
				status,
			}
		}
		alertTable.SetRows(rows)
		sections = append(sections, renderPanel("Fired Alerts", alertTable.View(), width))
	}

	// --- Notification test log ---
	if notifySending || len(notifyLog) > 0 {
		sections = append(sections, renderNotifyLog(notifyLog, notifySending, width))
	}

	// --- Transient form error ---
	if formErr != "" {
		sections = append(sections, notifyErrStyle.Render("  ⚠ "+formErr))
	}

	// --- Delete confirmation prompt (takes the place of the help line) ---
	if confirmDelete {
		prompt := alertCritStyle.Render(fmt.Sprintf("Delete rule %q and its fired alerts?", confirmName)) +
			normalHelpStyle.Render("   ") +
			alertCritStyle.Bold(true).Render("y") + normalHelpStyle.Render(" / ") +
			lipgloss.NewStyle().Foreground(colorPink).Bold(true).Render("N")
		sections = append(sections, lipgloss.NewStyle().MarginTop(1).Render(prompt))
		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}

	// Build help line with active-state highlighting for current focus panel.
	type helpItem struct {
		text   string
		active bool
	}
	helpItems := []helpItem{
		{"n:new", alertFocus == 0},
		{"e:edit", alertFocus == 0},
		{"d:delete", alertFocus == 0},
		{"space:toggle", alertFocus == 0},
		{"enter:ack", alertFocus == 1},
		{"t:test", false},
		{"tab:switch", true},
		{"PgUp/Dn:scroll", false},
	}
	var helpParts []string
	for _, item := range helpItems {
		if item.active {
			helpParts = append(helpParts, activeHelpStyle.Render(item.text))
		} else {
			helpParts = append(helpParts, normalHelpStyle.Render(item.text))
		}
	}
	helpLine := strings.Join(helpParts, normalHelpStyle.Render("  "))
	help := lipgloss.NewStyle().MarginTop(1).Render(helpLine)
	sections = append(sections, help)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

var (
	normalHelpStyle = lipgloss.NewStyle().Foreground(colorDeepPurple)
	activeHelpStyle = lipgloss.NewStyle().Foreground(colorPink).Bold(true)
)

// renderRuleDetail renders the full configuration of the selected rule in a labeled
// key/value block, so every field (exact value, duration, scope, id) is visible without
// opening the edit form. ruleDetail() remains the one-line summary used in the list.
func renderRuleDetail(r api.AlertRuleMetric, width int) string {
	label := labelStyle.Render
	val := valueStyle.Render
	ftoa := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

	var lines []string
	row := func(k, v string) {
		if v != "" {
			lines = append(lines, "  "+label(fmt.Sprintf("%-12s", k))+val(v))
		}
	}

	status := "enabled"
	if !r.Enabled {
		status = "disabled"
	}
	row("name", r.Name)
	row("type", ruleTypeDisplay(r.Type))
	row("metric", r.Metric)

	switch r.Type {
	case "threshold":
		// "over" (across the window), not "for" (sustained for its whole length):
		// the engine compares the windowed average/max, mirroring the fired-alert text.
		row("condition", fmt.Sprintf("%s %s over %s", r.Operator, ftoa(r.Value), r.Duration))
		switch {
		case r.Mount != "":
			row("mount", r.Mount)
		case r.InterfaceName != "":
			row("interface", r.InterfaceName)
		case r.Sensor != "":
			row("sensor", r.Sensor)
		}
	case "predictive":
		row("mount", r.Mount)
		row("predict", fmt.Sprintf("fills to %s%% within %dh", ftoa(r.ThresholdPct), r.PredictHours))
	case "variance":
		row("condition", fmt.Sprintf("Δ > %s%% × %d over %s", ftoa(r.DeltaThreshold), r.MinCount, r.Duration))
	case "process_down":
		row("process", r.ProcessName)
		row("pattern", r.ProcessPattern)
		row("condition", fmt.Sprintf("fewer than %d for %s", r.MinInstances, r.CheckDuration))
	case "process_thrashing":
		row("process", r.ProcessName)
		row("pattern", r.ProcessPattern)
		row("condition", fmt.Sprintf("> %d restarts in %s", r.RestartThreshold, r.RestartWindow))
	}

	row("severity", r.Severity)
	row("status", status)
	row("id", fmt.Sprintf("%d", r.ID))

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return renderPanel("Rule Detail", content, width)
}

func renderNotifyLog(entries []notifyLogEntry, sending bool, width int) string {
	var lines []string

	for _, e := range entries {
		lines = append(lines, formatNotifyEntry(e))
	}

	if sending {
		lines = append(lines, notifyDimStyle.Render("  Sending..."))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return renderPanel("Notification Test Log", content, width)
}

func formatNotifyEntry(e notifyLogEntry) string {
	ts := e.SentAt.Format(time.TimeOnly)

	if e.Dest == "" && e.Error != "" {
		return fmt.Sprintf("  %s  %s",
			notifyDimStyle.Render(ts),
			notifyErrStyle.Render(e.Error),
		)
	}

	latencyStr := formatLatency(e.Latency)
	dest := notifyDestStyle.Render(e.Dest)
	method := strings.ToUpper(e.Method)

	var headline string
	if e.Error != "" {
		statusStr := notifyErrStyle.Render("ERR")
		if e.StatusCode > 0 {
			statusStr = notifyErrStyle.Render(fmt.Sprintf("%d", e.StatusCode))
		}
		headline = fmt.Sprintf("  %s  %s %s  %s  %s  %s",
			notifyDimStyle.Render(ts),
			notifyDimStyle.Render(method),
			dest,
			statusStr,
			notifyDimStyle.Render(latencyStr),
			notifyErrStyle.Render(e.Error),
		)
	} else {
		statusStr := notifyOkStyle.Render("OK")
		if e.StatusCode > 0 {
			statusStr = notifyOkStyle.Render(fmt.Sprintf("%d", e.StatusCode))
		}
		headline = fmt.Sprintf("  %s  %s %s  %s  %s",
			notifyDimStyle.Render(ts),
			notifyDimStyle.Render(method),
			dest,
			statusStr,
			notifyDimStyle.Render(latencyStr),
		)
	}

	if e.Body == "" {
		return headline
	}

	// Indent each line of the body
	var bodyLines []string
	for _, line := range strings.Split(e.Body, "\n") {
		bodyLines = append(bodyLines, "         "+notifyDimStyle.Render(line))
	}
	return headline + "\n" + strings.Join(bodyLines, "\n")
}

func formatLatency(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
}

func renderRulesSection(rules []api.AlertRuleMetric, cursor int, width int, focused bool) string {
	if len(rules) == 0 {
		return renderPanel("Alert Rules", valueStyle.Render("No rules configured. Press 'n' to create one."), width)
	}

	// Scale column widths for narrow terminals
	// Fixed columns: type(12), severity(9), status(9) = 30
	// Flexible: name + metric share remaining space
	fixedCols := 30
	flexible := width - fixedCols - 8 // 8 for padding/borders
	if flexible < 20 {
		flexible = 20
	}
	nameW := flexible * 2 / 5
	metricW := flexible - nameW
	if nameW < 10 {
		nameW = 10
	}
	if metricW < 10 {
		metricW = 10
	}

	colName := lipgloss.NewStyle().Width(nameW)
	colType := lipgloss.NewStyle().Width(12)
	colMetric := lipgloss.NewStyle().Width(metricW)
	colSev := lipgloss.NewStyle().Width(9)
	colStatus := lipgloss.NewStyle().Width(9)

	var rows []string

	headerRow := lipgloss.JoinHorizontal(lipgloss.Left,
		colName.Inherit(headerStyle).Render("Name"),
		colType.Inherit(headerStyle).Render("Type"),
		colMetric.Inherit(headerStyle).Render("Metric"),
		colSev.Inherit(headerStyle).Render("Severity"),
		colStatus.Inherit(headerStyle).Render("Status"),
	)
	rows = append(rows, "  "+headerRow)

	for i, r := range rules {
		status := lipgloss.NewStyle().Foreground(colorGreen).Render("enabled")
		if !r.Enabled {
			status = dimStyle.Render("disabled")
		}

		sevStyle := alertWarnStyle
		if r.Severity == "critical" {
			sevStyle = alertCritStyle
		}

		line := lipgloss.JoinHorizontal(lipgloss.Left,
			colName.Inherit(valueStyle).Render(truncate(r.Name, nameW)),
			colType.Inherit(dimStyle).Render(truncate(ruleTypeDisplay(r.Type), 12)),
			colMetric.Inherit(valueStyle).Render(truncate(ruleDetail(r), metricW)),
			colSev.Inherit(sevStyle).Render(r.Severity),
			colStatus.Render(status),
		)

		if i == cursor && focused {
			line = lipgloss.NewStyle().
				Background(colorDeepPurple).
				Foreground(colorText).
				Bold(true).
				Render(line)
		}
		rows = append(rows, "  "+line)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return renderPanel("Alert Rules", content, width)
}

func ruleTypeDisplay(t string) string {
	switch t {
	case "process_down":
		return "proc_down"
	case "process_thrashing":
		return "proc_thrash"
	default:
		return t
	}
}

func ruleDetail(r api.AlertRuleMetric) string {
	switch r.Type {
	case "threshold":
		return fmt.Sprintf("%s %s%.0f", r.Metric, r.Operator, r.Value)
	case "predictive":
		return fmt.Sprintf("%s <%dh", r.Metric, r.PredictHours)
	case "variance":
		return fmt.Sprintf("mem Δ>%.0f%% ×%d", r.DeltaThreshold, r.MinCount)
	case "process_down":
		name := r.ProcessName
		if r.ProcessPattern != "" {
			name = r.ProcessPattern
		}
		return fmt.Sprintf("%s (min: %d)", name, r.MinInstances)
	case "process_thrashing":
		name := r.ProcessName
		if r.ProcessPattern != "" {
			name = r.ProcessPattern
		}
		return fmt.Sprintf("%s (>%d in %s)", name, r.RestartThreshold, r.RestartWindow)
	}
	return r.Metric
}

// truncate shortens s to at most max runes, appending an ellipsis when cut. It
// slices on rune boundaries so multibyte UTF-8 (process names, cmdlines) is never
// corrupted mid-codepoint.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max-1]) + "…"
}
