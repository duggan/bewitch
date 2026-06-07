package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/duggan/bewitch/internal/api"
)

type alertFormState struct {
	// editID is 0 when creating a new rule, or the rule's id when editing an existing
	// one. In edit mode the category + alert-type selection steps are skipped (type is
	// immutable) and the form opens straight on the rule's parameter group.
	editID  int
	enabled bool
	// origMetric is the rule's metric captured on edit. Because the metric is locked in
	// edit mode, it is written back verbatim so rules round-trip losslessly (including
	// metrics the create form can't produce, e.g. gpu.temperature).
	origMetric string

	// Step 1: category
	category string // cpu, memory, disk, network, temperature, gpu, smart, process

	// Step 2: alert type
	alertType string // threshold, variance, predictive, process_down, process_thrashing

	// Step 3: parameters
	operator     string
	valueStr     string
	durationStr  string
	aggregate    string // threshold value metrics: avg (default), max, min
	severity     string
	mount        string
	ifaceName    string
	sensor       string
	smartMetric  string // for smart category: smart.reallocated, smart.percent_used, ...
	direction    string // rx, tx (for network)
	predictHours string // for predictive
	thresholdPct string // for predictive
	deltaStr     string // for variance: delta threshold %
	countStr     string // for variance: min exceedance count

	// Process alert parameters
	processName      string
	processPattern   string
	minInstances     string
	restartThreshold string
	restartWindow    string
	checkDuration    string

	// Step 4: name
	name string
}

func buildAlertForm(state *alertFormState) *huh.Form {
	theme := huh.ThemeCharm()
	theme.Focused.Base = lipgloss.NewStyle().PaddingLeft(1)
	theme.Focused.Title = lipgloss.NewStyle().Foreground(colorPink).Bold(true)
	theme.Focused.Description = lipgloss.NewStyle().Foreground(colorMuted)
	theme.Focused.SelectedOption = lipgloss.NewStyle().Foreground(colorPink).Bold(true)
	theme.Focused.UnselectedOption = lipgloss.NewStyle().Foreground(colorText)
	theme.Focused.FocusedButton = lipgloss.NewStyle().Foreground(colorDarkBg).Background(colorPink).Bold(true).Padding(0, 1)
	theme.Focused.BlurredButton = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1)
	theme.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(colorPink)
	theme.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(colorMagenta)
	theme.Focused.TextInput.Text = lipgloss.NewStyle().Foreground(colorText)
	theme.Blurred = theme.Focused

	// Build groups list - skip category selection if already set
	var groups []*huh.Group

	if state.category == "" {
		groups = append(groups, huh.NewGroup(
			huh.NewSelect[string]().
				Title("Metric Category").
				Options(
					huh.NewOption("CPU", "cpu"),
					huh.NewOption("Memory", "memory"),
					huh.NewOption("Disk", "disk"),
					huh.NewOption("Network", "network"),
					huh.NewOption("Temperature", "temperature"),
					huh.NewOption("GPU", "gpu"),
					huh.NewOption("Disk health (SMART)", "smart"),
					huh.NewOption("Process", "process"),
				).
				Value(&state.category),
		))
	}

	// In edit mode the alert type is locked (changing it would mean moving the rule to a
	// different config table); skip the selection step entirely.
	if state.editID == 0 {
		groups = append(groups, huh.NewGroup(
			huh.NewSelect[string]().
				Title("Alert Type").
				OptionsFunc(func() []huh.Option[string] {
					switch state.category {
					case "cpu":
						return []huh.Option[string]{
							huh.NewOption("Average usage over threshold", "threshold"),
						}
					case "memory":
						return []huh.Option[string]{
							huh.NewOption("Average usage over threshold", "threshold"),
							huh.NewOption("Variance / thrashing detection", "variance"),
						}
					case "disk":
						return []huh.Option[string]{
							huh.NewOption("Usage exceeds percentage", "threshold"),
							huh.NewOption("Fill rate prediction", "predictive"),
						}
					case "network":
						return []huh.Option[string]{
							huh.NewOption("Average throughput over threshold", "threshold"),
						}
					case "temperature":
						return []huh.Option[string]{
							huh.NewOption("Average temperature over threshold", "threshold"),
						}
					case "gpu":
						return []huh.Option[string]{
							huh.NewOption("Average GPU utilization over threshold", "threshold"),
						}
					case "smart":
						return []huh.Option[string]{
							huh.NewOption("SMART attribute over threshold", "threshold"),
						}
					case "process":
						return []huh.Option[string]{
							huh.NewOption("Process went down", "process_down"),
							huh.NewOption("Process restarting (thrashing)", "process_thrashing"),
						}
					}
					return nil
				}, &state.category).
				Value(&state.alertType),
		))
	}

	groups = append(groups,
		// SMART attribute (aggregated across all disks). Asked BEFORE the threshold
		// value so that field's description/placeholder can reflect the chosen
		// attribute (a raw sector count wants "0", NVMe wear wants "0-100 %").
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("SMART Attribute").
				Description("Worst value across all disks trips the alert").
				Options(
					huh.NewOption("Reallocated sectors", "smart.reallocated"),
					huh.NewOption("Pending sectors", "smart.pending"),
					huh.NewOption("Uncorrectable errors", "smart.uncorrectable"),
					huh.NewOption("NVMe wear (% used)", "smart.percent_used"),
					huh.NewOption("Health failures (count)", "smart.unhealthy"),
				).
				Value(&state.smartMetric),
		).WithHideFunc(func() bool {
			return state.category != "smart"
		}),
		// Aggregate function (value metrics only). Its own group because huh only
		// supports hiding at the group level — the SMART metrics have a fixed
		// MAX/COUNT aggregate, so this is hidden for them (and for non-threshold types).
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Aggregate").
				Description("How the metric is reduced over the window before comparing").
				Options(
					huh.NewOption("Average over the window", "avg"),
					huh.NewOption("Maximum in the window (catch a spike)", "max"),
					huh.NewOption("Minimum in the window", "min"),
				).
				Value(&state.aggregate),
		).WithHideFunc(func() bool {
			return state.alertType != "threshold" || state.category == "smart"
		}),
		// Threshold parameters
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Operator").
				Options(
					huh.NewOption("> (greater than)", ">"),
					huh.NewOption(">= (greater or equal)", ">="),
					huh.NewOption("< (less than)", "<"),
					huh.NewOption("<= (less or equal)", "<="),
				).
				Value(&state.operator),
			huh.NewInput().
				Title("Threshold Value").
				// Dynamic: the form is built once (with category="" in create mode),
				// so a static Description/Placeholder could never reflect the metric
				// the user later picks — it just showed blank. DescriptionFunc/
				// PlaceholderFunc are re-evaluated by huh whenever the bound category
				// or SMART attribute changes, so the hint always matches the metric.
				DescriptionFunc(func() string { return thresholdDesc(state) }, thresholdBindings(state)).
				PlaceholderFunc(func() string { return thresholdPlaceholder(state) }, thresholdBindings(state)).
				Value(&state.valueStr).
				Validate(validateFloat),
			huh.NewInput().
				Title("Duration").
				Description("How long the condition must persist (e.g. 5m, 1h)").
				Placeholder("5m").
				Value(&state.durationStr).
				Validate(validateDuration),
		).WithHideFunc(func() bool {
			return state.alertType != "threshold"
		}),
		// Network direction
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Direction").
				Options(
					huh.NewOption("Download (RX)", "rx"),
					huh.NewOption("Upload (TX)", "tx"),
				).
				Value(&state.direction),
			huh.NewInput().
				Title("Interface").
				Description("Network interface name (e.g. eth0, enp0s3)").
				Placeholder("eth0").
				Value(&state.ifaceName).
				Validate(validateNotEmpty),
		).WithHideFunc(func() bool {
			return !(state.category == "network" && state.alertType == "threshold")
		}),
		// Disk mount
		huh.NewGroup(
			huh.NewInput().
				Title("Mount Point").
				Description("Filesystem mount path").
				Placeholder("/").
				Value(&state.mount).
				Validate(validateNotEmpty),
		).WithHideFunc(func() bool {
			return !(state.category == "disk" && state.alertType == "threshold")
		}),
		// Temperature sensor
		huh.NewGroup(
			huh.NewInput().
				Title("Sensor Name").
				Description("Temperature sensor identifier").
				Placeholder("coretemp_0").
				Value(&state.sensor).
				Validate(validateNotEmpty),
		).WithHideFunc(func() bool {
			return !(state.category == "temperature")
		}),
		// GPU device
		huh.NewGroup(
			huh.NewInput().
				Title("GPU Name").
				Description("GPU device name (as shown in hardware view)").
				Placeholder("Intel UHD Graphics 770").
				Value(&state.sensor).
				Validate(validateNotEmpty),
		).WithHideFunc(func() bool {
			return !(state.category == "gpu")
		}),
		// Variance parameters (memory only)
		huh.NewGroup(
			huh.NewInput().
				Title("Delta Threshold (%)").
				Description("Minimum memory change percentage to count as a spike").
				Placeholder("5").
				Value(&state.deltaStr).
				Validate(validateFloat),
			huh.NewInput().
				Title("Minimum Spike Count").
				Description("Number of spikes required to trigger alert").
				Placeholder("10").
				Value(&state.countStr).
				Validate(validateInt),
			huh.NewInput().
				Title("Time Window").
				Description("Period to check for spikes (e.g. 5m, 1h)").
				Placeholder("5m").
				Value(&state.durationStr).
				Validate(validateDuration),
		).WithHideFunc(func() bool {
			return state.alertType != "variance"
		}),
		// Predictive parameters (disk only)
		huh.NewGroup(
			huh.NewInput().
				Title("Mount Point").
				Placeholder("/").
				Value(&state.mount).
				Validate(validateNotEmpty),
			huh.NewSelect[string]().
				Title("Prediction Window").
				Description("Alert if disk fills within this timeframe").
				Options(
					huh.NewOption("< 24 hours", "24"),
					huh.NewOption("< 3 days", "72"),
					huh.NewOption("< 7 days", "168"),
				).
				Value(&state.predictHours),
			huh.NewInput().
				Title("Target Fill %").
				Description("Percentage threshold to predict").
				Placeholder("95").
				Value(&state.thresholdPct).
				Validate(validateFloat),
		).WithHideFunc(func() bool {
			return state.alertType != "predictive"
		}),
		// Process down parameters
		huh.NewGroup(
			huh.NewInput().
				Title("Process Name").
				Description("Exact process name to monitor (from 'comm')").
				Placeholder("nginx").
				Value(&state.processName).
				Validate(validateNotEmpty),
			huh.NewInput().
				Title("Command Line Pattern (optional)").
				Description("Glob pattern to match cmdline (e.g. */myapp*)").
				Placeholder("").
				Value(&state.processPattern),
			huh.NewInput().
				Title("Minimum Instances").
				Description("Alert if fewer than this many instances are running").
				Placeholder("1").
				Value(&state.minInstances).
				Validate(validateInt),
			huh.NewInput().
				Title("Check Duration").
				Description("How long process must be missing before alerting").
				Placeholder("30s").
				Value(&state.checkDuration).
				Validate(validateDuration),
		).WithHideFunc(func() bool {
			return state.alertType != "process_down"
		}),
		// Process thrashing parameters
		huh.NewGroup(
			huh.NewInput().
				Title("Process Name").
				Description("Exact process name to monitor").
				Placeholder("myworker").
				Value(&state.processName).
				Validate(validateNotEmpty),
			huh.NewInput().
				Title("Command Line Pattern (optional)").
				Description("Glob pattern to match cmdline").
				Value(&state.processPattern),
			huh.NewInput().
				Title("Restart Threshold").
				Description("Alert after this many restarts").
				Placeholder("5").
				Value(&state.restartThreshold).
				Validate(validateInt),
			huh.NewInput().
				Title("Time Window").
				Description("Count restarts within this period").
				Placeholder("5m").
				Value(&state.restartWindow).
				Validate(validateDuration),
		).WithHideFunc(func() bool {
			return state.alertType != "process_thrashing"
		}),
		// Severity + Name (always shown last)
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Severity").
				Options(
					huh.NewOption("Warning", "warning"),
					huh.NewOption("Critical", "critical"),
				).
				Value(&state.severity),
			huh.NewInput().
				Title("Rule Name").
				Description("A unique name for this alert rule").
				Placeholder("my_alert").
				Value(&state.name).
				Validate(validateNotEmpty),
			huh.NewConfirm().
				Title(confirmTitle(state)).
				Affirmative(confirmAffirmative(state)).
				Negative("Cancel"),
		),
	)

	return huh.NewForm(groups...).WithTheme(theme).WithWidth(60)
}

// thresholdBindings is what huh watches to know when to re-evaluate the
// Threshold Value field's DescriptionFunc/PlaceholderFunc: the category (set in
// create mode after the form is built) and the SMART attribute (chosen just
// before, in the reordered group). hashstructure dereferences the pointers, so
// the funcs re-run whenever either string changes.
func thresholdBindings(state *alertFormState) []any {
	return []any{&state.category, &state.smartMetric}
}

func thresholdDesc(state *alertFormState) string {
	switch state.category {
	case "cpu":
		return "CPU usage percentage (0-100)"
	case "memory":
		return "Memory usage percentage (0-100)"
	case "disk":
		return "Disk usage percentage (0-100)"
	case "network":
		return "Throughput in bytes/sec"
	case "temperature":
		return "Temperature in °C"
	case "gpu":
		return "GPU utilization percentage (0-100)"
	case "smart":
		return smartThresholdDesc(state.smartMetric)
	}
	return ""
}

// smartThresholdDesc tailors the threshold hint to the chosen SMART attribute —
// a raw sector/error count and an NVMe wear percentage have very different units.
func smartThresholdDesc(metric string) string {
	switch metric {
	case "smart.percent_used":
		return "NVMe wear level, percent (0-100); alert e.g. > 90"
	case "smart.unhealthy":
		return "Count of drives reporting a SMART health failure; alert > 0"
	case "smart.reallocated", "smart.pending", "smart.uncorrectable":
		return "Raw sector/error count, worst across disks; alert > 0"
	}
	return "Threshold for the chosen SMART attribute"
}

// thresholdPlaceholder gives the Threshold Value input a sensible example for the
// chosen metric. The old static "90" was wrong for the SMART sector/error counts
// (where the meaningful alert is > 0) and for raw throughput.
func thresholdPlaceholder(state *alertFormState) string {
	switch state.category {
	case "network":
		return "1000000"
	case "temperature":
		return "80"
	case "smart":
		switch state.smartMetric {
		case "smart.percent_used":
			return "90"
		default:
			return "0"
		}
	}
	return "90"
}

func confirmTitle(state *alertFormState) string {
	if state.editID != 0 {
		return "Update this alert rule?"
	}
	return "Create this alert rule?"
}

func confirmAffirmative(state *alertFormState) string {
	if state.editID != 0 {
		return "Update"
	}
	return "Create"
}

func (s *alertFormState) toAlertRuleMetric() api.AlertRuleMetric {
	rule := api.AlertRuleMetric{
		ID:       s.editID,
		Name:     s.name,
		Type:     s.alertType,
		Severity: s.severity,
		Enabled:  s.enabled,
	}

	switch s.alertType {
	case "threshold":
		rule.Operator = s.operator
		rule.Value, _ = strconv.ParseFloat(s.valueStr, 64)
		rule.Duration = s.durationStr
		// Default empty → "avg" (back-compat). SMART stores 'avg' too but the engine
		// ignores it (SMART has a fixed MAX/COUNT aggregate); the form hides the picker.
		rule.Aggregate = s.aggregate
		if rule.Aggregate == "" {
			rule.Aggregate = "avg"
		}
		switch s.category {
		case "cpu":
			rule.Metric = "cpu.aggregate"
		case "memory":
			rule.Metric = "memory.used_pct"
		case "disk":
			rule.Metric = "disk.used_pct"
			rule.Mount = s.mount
		case "network":
			if s.direction == "rx" {
				rule.Metric = "network.rx"
			} else {
				rule.Metric = "network.tx"
			}
			rule.InterfaceName = s.ifaceName
		case "temperature":
			rule.Metric = "temperature.sensor"
			rule.Sensor = s.sensor
		case "gpu":
			rule.Metric = "gpu.utilization"
			rule.Sensor = s.sensor
		case "smart":
			rule.Metric = s.smartMetric
		}
	case "variance":
		rule.Metric = "memory.variance"
		rule.DeltaThreshold, _ = strconv.ParseFloat(s.deltaStr, 64)
		rule.MinCount, _ = strconv.Atoi(s.countStr)
		rule.Duration = s.durationStr
	case "predictive":
		rule.Metric = "disk.used_pct"
		rule.Mount = s.mount
		rule.PredictHours, _ = strconv.Atoi(s.predictHours)
		rule.ThresholdPct, _ = strconv.ParseFloat(s.thresholdPct, 64)
	case "process_down":
		rule.ProcessName = s.processName
		rule.ProcessPattern = s.processPattern
		rule.MinInstances, _ = strconv.Atoi(s.minInstances)
		if rule.MinInstances == 0 {
			rule.MinInstances = 1
		}
		rule.CheckDuration = s.checkDuration
	case "process_thrashing":
		rule.ProcessName = s.processName
		rule.ProcessPattern = s.processPattern
		rule.RestartThreshold, _ = strconv.Atoi(s.restartThreshold)
		rule.RestartWindow = s.restartWindow
	}

	// On edit the metric is locked: write the captured original back verbatim so it can
	// never be silently changed by the derived mapping above.
	if s.editID != 0 && s.origMetric != "" {
		rule.Metric = s.origMetric
	}

	return rule
}

// fromAlertRuleMetric builds form state pre-populated for editing an existing rule. The
// category, alert type, and (for network) direction are derived from the rule's type and
// metric so the form opens straight on the locked rule's parameter group. Numeric values
// are stringified at full precision so they round-trip without loss (the list cell's
// %.0f formatting is display-only).
func fromAlertRuleMetric(rule api.AlertRuleMetric) *alertFormState {
	s := &alertFormState{
		editID:     rule.ID,
		enabled:    rule.Enabled,
		origMetric: rule.Metric,
		alertType:  rule.Type,
		severity:   rule.Severity,
		name:       rule.Name,
	}

	ftoa := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

	switch rule.Type {
	case "threshold":
		s.operator = rule.Operator
		s.valueStr = ftoa(rule.Value)
		s.durationStr = rule.Duration
		s.aggregate = rule.Aggregate
		if s.aggregate == "" {
			s.aggregate = "avg" // old rules created before the aggregate column
		}
		switch rule.Metric {
		case "cpu.aggregate":
			s.category = "cpu"
		case "memory.used_pct":
			s.category = "memory"
		case "disk.used_pct":
			s.category = "disk"
			s.mount = rule.Mount
		case "network.rx", "network.tx":
			s.category = "network"
			s.ifaceName = rule.InterfaceName
			if rule.Metric == "network.tx" {
				s.direction = "tx"
			} else {
				s.direction = "rx"
			}
		case "temperature.sensor":
			s.category = "temperature"
			s.sensor = rule.Sensor
		default:
			if strings.HasPrefix(rule.Metric, "gpu.") {
				s.category = "gpu"
				s.sensor = rule.Sensor
			} else if strings.HasPrefix(rule.Metric, "smart.") {
				s.category = "smart"
				s.smartMetric = rule.Metric
			}
		}
	case "variance":
		s.category = "memory"
		s.deltaStr = ftoa(rule.DeltaThreshold)
		s.countStr = strconv.Itoa(rule.MinCount)
		s.durationStr = rule.Duration
	case "predictive":
		s.category = "disk"
		s.mount = rule.Mount
		s.predictHours = strconv.Itoa(rule.PredictHours)
		s.thresholdPct = ftoa(rule.ThresholdPct)
	case "process_down":
		s.category = "process"
		s.processName = rule.ProcessName
		s.processPattern = rule.ProcessPattern
		s.minInstances = strconv.Itoa(rule.MinInstances)
		s.checkDuration = rule.CheckDuration
	case "process_thrashing":
		s.category = "process"
		s.processName = rule.ProcessName
		s.processPattern = rule.ProcessPattern
		s.restartThreshold = strconv.Itoa(rule.RestartThreshold)
		s.restartWindow = rule.RestartWindow
	}

	return s
}

func validateFloat(s string) error {
	if s == "" {
		return fmt.Errorf("value required")
	}
	if _, err := strconv.ParseFloat(s, 64); err != nil {
		return fmt.Errorf("must be a number")
	}
	return nil
}

func validateInt(s string) error {
	if s == "" {
		return fmt.Errorf("value required")
	}
	if _, err := strconv.Atoi(s); err != nil {
		return fmt.Errorf("must be a whole number")
	}
	return nil
}

func validateNotEmpty(s string) error {
	if s == "" {
		return fmt.Errorf("value required")
	}
	return nil
}

func validateDuration(s string) error {
	if s == "" {
		return fmt.Errorf("value required")
	}
	// Simple validation: must end with s, m, or h
	last := s[len(s)-1]
	if last != 's' && last != 'm' && last != 'h' {
		return fmt.Errorf("must be a duration (e.g. 5m, 1h, 30s)")
	}
	if _, err := strconv.ParseFloat(s[:len(s)-1], 64); err != nil {
		return fmt.Errorf("must be a duration (e.g. 5m, 1h, 30s)")
	}
	return nil
}
