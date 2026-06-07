package tui

import "testing"

// TestCategoryOptionsGating verifies the create form only offers hardware-dependent
// categories the host actually reports: the always-present ones are unconditional,
// while SMART/GPU/temperature appear only when available — so a box with no SMART
// disks isn't offered SMART alerts that could never fire.
func TestCategoryOptionsGating(t *testing.T) {
	values := func(caps formCapabilities) map[string]bool {
		m := map[string]bool{}
		for _, o := range categoryOptions(caps) {
			m[o.Value] = true
		}
		return m
	}
	always := []string{"cpu", "memory", "disk", "network", "process"}
	gated := []string{"smart", "gpu", "temperature", "ecc"}

	t.Run("no hardware: only always-present categories", func(t *testing.T) {
		got := values(formCapabilities{})
		for _, c := range always {
			if !got[c] {
				t.Errorf("category %q must always be offered", c)
			}
		}
		for _, c := range gated {
			if got[c] {
				t.Errorf("category %q must be hidden when unavailable", c)
			}
		}
	})

	t.Run("all hardware present: every category offered", func(t *testing.T) {
		got := values(formCapabilities{smart: true, gpu: true, temperature: true, ecc: true})
		for _, c := range append(append([]string{}, always...), gated...) {
			if !got[c] {
				t.Errorf("category %q should be offered when available", c)
			}
		}
	})

	t.Run("only SMART present: gates independently", func(t *testing.T) {
		got := values(formCapabilities{smart: true})
		if !got["smart"] {
			t.Error("smart should be offered when available")
		}
		if got["gpu"] || got["temperature"] {
			t.Error("gpu/temperature must stay hidden when only smart is available")
		}
	})
}

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
