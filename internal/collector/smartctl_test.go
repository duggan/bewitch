package collector

import "testing"

func TestParseSmartctlNVMe(t *testing.T) {
	t.Run("healthy NVMe", func(t *testing.T) {
		out := &smartctlOutput{
			NVMEHealth: &smartctlNVMEHealthInfoLog{
				CriticalWarning:  0,
				Temperature:      42,
				AvailableSpare:   90,
				PercentageUsed:   5,
				PowerCycles:      100,
				PowerOnHours:     5000,
				DataUnitsRead:    123,
				DataUnitsWritten: 456,
				MediaErrors:      0,
			},
		}
		info := parseSmartctlNVMe(out)
		if !info.Available {
			t.Error("expected Available = true")
		}
		if !info.Healthy {
			t.Error("expected Healthy = true")
		}
		if info.Temperature != 42 {
			t.Errorf("Temperature = %d, want 42", info.Temperature)
		}
		if info.AvailableSpare != 90 {
			t.Errorf("AvailableSpare = %d, want 90", info.AvailableSpare)
		}
		if info.PercentUsed != 5 {
			t.Errorf("PercentUsed = %d, want 5", info.PercentUsed)
		}
		if info.PowerCycles != 100 {
			t.Errorf("PowerCycles = %d, want 100", info.PowerCycles)
		}
		if info.PowerOnHours != 5000 {
			t.Errorf("PowerOnHours = %d, want 5000", info.PowerOnHours)
		}
		// DataUnitsRead * 1000
		if info.ReadSectors != 123000 {
			t.Errorf("ReadSectors = %d, want 123000", info.ReadSectors)
		}
		if info.WrittenSectors != 456000 {
			t.Errorf("WrittenSectors = %d, want 456000", info.WrittenSectors)
		}
		if info.UncorrectableErrs != 0 {
			t.Errorf("UncorrectableErrs = %d, want 0", info.UncorrectableErrs)
		}
	})

	t.Run("unhealthy NVMe with critical warning", func(t *testing.T) {
		out := &smartctlOutput{
			NVMEHealth: &smartctlNVMEHealthInfoLog{
				CriticalWarning: 1,
				Temperature:     85,
				MediaErrors:     3,
			},
		}
		info := parseSmartctlNVMe(out)
		if info.Healthy {
			t.Error("expected Healthy = false with CriticalWarning = 1")
		}
		if info.UncorrectableErrs != 3 {
			t.Errorf("UncorrectableErrs = %d, want 3", info.UncorrectableErrs)
		}
	})

	t.Run("temperature fallback from top-level", func(t *testing.T) {
		out := &smartctlOutput{
			Temperature: &smartctlTemperature{Current: 38},
			NVMEHealth: &smartctlNVMEHealthInfoLog{
				Temperature: 0, // zero -> falls back
			},
		}
		info := parseSmartctlNVMe(out)
		if info.Temperature != 38 {
			t.Errorf("Temperature = %d, want 38 (from fallback)", info.Temperature)
		}
	})

	t.Run("nil NVMEHealth still available", func(t *testing.T) {
		out := &smartctlOutput{}
		info := parseSmartctlNVMe(out)
		if !info.Available {
			t.Error("expected Available = true")
		}
		if !info.Healthy {
			t.Error("expected Healthy = true (default)")
		}
	})
}

func TestParseSmartctlATA(t *testing.T) {
	boolPtr := func(v bool) *bool { return &v }

	t.Run("healthy ATA with all attributes", func(t *testing.T) {
		out := &smartctlOutput{
			SmartStatus: smartctlSmartStatus{Passed: boolPtr(true)},
			Temperature: &smartctlTemperature{Current: 35},
			ATASmart: &smartctlATASmartAttrs{
				Table: []smartctlATAAttr{
					{ID: 1, Raw: smartctlATARawVal{Value: 100}},   // ReadErrorRate
					{ID: 5, Raw: smartctlATARawVal{Value: 0}},     // Reallocated
					{ID: 9, Raw: smartctlATARawVal{Value: 12000}}, // PowerOnHours
					{ID: 12, Raw: smartctlATARawVal{Value: 50}},   // PowerCycles
					{ID: 197, Raw: smartctlATARawVal{Value: 0}},   // Pending
					{ID: 198, Raw: smartctlATARawVal{Value: 0}},   // Uncorrectable
					{ID: 241, Raw: smartctlATARawVal{Value: 1000}}, // Written
					{ID: 242, Raw: smartctlATARawVal{Value: 2000}}, // Read
				},
			},
		}
		info := parseSmartctlATA(out)
		if !info.Available {
			t.Error("expected Available")
		}
		if !info.Healthy {
			t.Error("expected Healthy")
		}
		if info.Temperature != 35 {
			t.Errorf("Temperature = %d, want 35", info.Temperature)
		}
		if info.ReadErrorRate != 100 {
			t.Errorf("ReadErrorRate = %d, want 100", info.ReadErrorRate)
		}
		if info.PowerOnHours != 12000 {
			t.Errorf("PowerOnHours = %d, want 12000", info.PowerOnHours)
		}
		if info.PowerCycles != 50 {
			t.Errorf("PowerCycles = %d, want 50", info.PowerCycles)
		}
		if info.WrittenSectors != 1000 {
			t.Errorf("WrittenSectors = %d, want 1000", info.WrittenSectors)
		}
		if info.ReadSectors != 2000 {
			t.Errorf("ReadSectors = %d, want 2000", info.ReadSectors)
		}
	})

	t.Run("failed smart status", func(t *testing.T) {
		out := &smartctlOutput{
			SmartStatus: smartctlSmartStatus{Passed: boolPtr(false)},
		}
		info := parseSmartctlATA(out)
		if info.Healthy {
			t.Error("expected Healthy = false when smart status failed")
		}
	})

	t.Run("nil smart status derives from counters - healthy", func(t *testing.T) {
		out := &smartctlOutput{
			SmartStatus: smartctlSmartStatus{Passed: nil},
			ATASmart: &smartctlATASmartAttrs{
				Table: []smartctlATAAttr{
					{ID: 5, Raw: smartctlATARawVal{Value: 0}},
					{ID: 197, Raw: smartctlATARawVal{Value: 0}},
					{ID: 198, Raw: smartctlATARawVal{Value: 0}},
				},
			},
		}
		info := parseSmartctlATA(out)
		if !info.Healthy {
			t.Error("expected Healthy = true when no errors and status nil")
		}
	})

	t.Run("nil smart status derives from counters - unhealthy", func(t *testing.T) {
		out := &smartctlOutput{
			SmartStatus: smartctlSmartStatus{Passed: nil},
			ATASmart: &smartctlATASmartAttrs{
				Table: []smartctlATAAttr{
					{ID: 5, Raw: smartctlATARawVal{Value: 10}}, // Reallocated > 0
					{ID: 197, Raw: smartctlATARawVal{Value: 0}},
					{ID: 198, Raw: smartctlATARawVal{Value: 0}},
				},
			},
		}
		info := parseSmartctlATA(out)
		if info.Healthy {
			t.Error("expected Healthy = false when reallocated > 0")
		}
	})

	t.Run("temperature from attr 194 with bitmask", func(t *testing.T) {
		out := &smartctlOutput{
			// No top-level temperature
			ATASmart: &smartctlATASmartAttrs{
				Table: []smartctlATAAttr{
					// Raw value might have min/max packed in higher bytes
					{ID: 194, Raw: smartctlATARawVal{Value: 0x002D002D002D}}, // 45 in low byte
				},
			},
			SmartStatus: smartctlSmartStatus{Passed: boolPtr(true)},
		}
		info := parseSmartctlATA(out)
		if info.Temperature != 0x2D { // 45
			t.Errorf("Temperature = %d, want 45", info.Temperature)
		}
	})
}

func TestSmartctlErrorMsg(t *testing.T) {
	tests := []struct {
		name string
		msgs []smartctlMsg
		want string
	}{
		{"no messages", nil, ""},
		{"no errors", []smartctlMsg{{String: "info", Severity: "information"}}, ""},
		{"first error", []smartctlMsg{
			{String: "info", Severity: "information"},
			{String: "device open failed", Severity: "error"},
			{String: "another error", Severity: "error"},
		}, "device open failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := smartctlErrorMsg(tt.msgs)
			if got != tt.want {
				t.Errorf("smartctlErrorMsg() = %q, want %q", got, tt.want)
			}
		})
	}
}
