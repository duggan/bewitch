package tui

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/ross/bewitch/internal/api"
)

type alertFormState struct {
	// Step 1: category
	category string // cpu, memory, disk, network, temperature, process

	// Step 2: alert type
	alertType string // threshold, variance, predictive, process_down, process_thrashing

	// Step 3: parameters
	operator     string
	valueStr     string
	durationStr  string
	severity     string
	mount        string
	ifaceName    string
	sensor       string
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
					huh.NewOption("Process", "process"),
				).
				Value(&state.category),
		))
	}

	groups = append(groups, huh.NewGroup(
		huh.NewSelect[string]().
			Title("Alert Type").
			OptionsFunc(func() []huh.Option[string] {
				switch state.category {
				case "cpu":
					return []huh.Option[string]{
						huh.NewOption("Sustained usage over threshold", "threshold"),
					}
				case "memory":
					return []huh.Option[string]{
						huh.NewOption("Sustained usage over threshold", "threshold"),
						huh.NewOption("Variance / thrashing detection", "variance"),
					}
				case "disk":
					return []huh.Option[string]{
						huh.NewOption("Usage exceeds percentage", "threshold"),
						huh.NewOption("Fill rate prediction", "predictive"),
					}
				case "network":
					return []huh.Option[string]{
						huh.NewOption("Sustained throughput", "threshold"),
					}
				case "temperature":
					return []huh.Option[string]{
						huh.NewOption("Sustained high temperature", "threshold"),
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

	groups = append(groups,
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
				Description(thresholdDesc(state)).
				Placeholder("90").
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
				Title("Create this alert rule?").
				Affirmative("Create").
				Negative("Cancel"),
		),
	)

	return huh.NewForm(groups...).WithTheme(theme).WithWidth(60)
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
	}
	return ""
}

func (s *alertFormState) toAlertRuleMetric() api.AlertRuleMetric {
	rule := api.AlertRuleMetric{
		Name:     s.name,
		Type:     s.alertType,
		Severity: s.severity,
		Enabled:  true,
	}

	switch s.alertType {
	case "threshold":
		rule.Operator = s.operator
		rule.Value, _ = strconv.ParseFloat(s.valueStr, 64)
		rule.Duration = s.durationStr
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

	return rule
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
