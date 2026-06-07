package tui

import "testing"

// TestThresholdDescByMetric verifies the Threshold Value field's description is
// metric-appropriate for every category — the bug was that the static binding
// showed a blank (create mode) or stale ("CPU usage percentage") hint for SMART
// and everything else. These are exactly the strings the field's DescriptionFunc
// returns at render time.
func TestThresholdDescByMetric(t *testing.T) {
	cases := []struct {
		category, smartMetric string
		want                  string
	}{
		{"cpu", "", "CPU usage percentage (0-100)"},
		{"memory", "", "Memory usage percentage (0-100)"},
		{"disk", "", "Disk usage percentage (0-100)"},
		{"network", "", "Throughput in bytes/sec"},
		{"temperature", "", "Temperature in °C"},
		{"gpu", "", "GPU utilization percentage (0-100)"},
		{"smart", "smart.reallocated", "Raw sector/error count, worst across disks; alert > 0"},
		{"smart", "smart.pending", "Raw sector/error count, worst across disks; alert > 0"},
		{"smart", "smart.uncorrectable", "Raw sector/error count, worst across disks; alert > 0"},
		{"smart", "smart.percent_used", "NVMe wear level, percent (0-100); alert e.g. > 90"},
		{"smart", "smart.unhealthy", "Count of drives reporting a SMART health failure; alert > 0"},
		{"smart", "", "Threshold for the chosen SMART attribute"}, // attribute not yet picked
		{"", "", ""}, // create mode, before a category is chosen
	}
	for _, c := range cases {
		state := &alertFormState{category: c.category, smartMetric: c.smartMetric}
		if got := thresholdDesc(state); got != c.want {
			t.Errorf("thresholdDesc(category=%q smart=%q) = %q, want %q", c.category, c.smartMetric, got, c.want)
		}
	}
	// The SMART description must never be a CPU/percentage hint — the reported bug.
	smart := &alertFormState{category: "smart", smartMetric: "smart.reallocated"}
	if got := thresholdDesc(smart); got == "CPU usage percentage (0-100)" {
		t.Errorf("SMART rule must not show the CPU hint, got %q", got)
	}
}

// TestThresholdPlaceholderByMetric verifies the example value matches the metric:
// the meaningful alert for SMART sector/error counts is > 0, not the old "90".
func TestThresholdPlaceholderByMetric(t *testing.T) {
	cases := []struct {
		category, smartMetric string
		want                  string
	}{
		{"cpu", "", "90"},
		{"disk", "", "90"},
		{"network", "", "1000000"},
		{"temperature", "", "80"},
		{"smart", "smart.reallocated", "0"},
		{"smart", "smart.pending", "0"},
		{"smart", "smart.uncorrectable", "0"},
		{"smart", "smart.unhealthy", "0"},
		{"smart", "smart.percent_used", "90"},
	}
	for _, c := range cases {
		state := &alertFormState{category: c.category, smartMetric: c.smartMetric}
		if got := thresholdPlaceholder(state); got != c.want {
			t.Errorf("thresholdPlaceholder(category=%q smart=%q) = %q, want %q", c.category, c.smartMetric, got, c.want)
		}
	}
}
