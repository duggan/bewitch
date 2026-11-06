package collector

import (
	"os"
	"testing"
	"time"
)

// fakeSmartctlEnv, when set, turns the test binary into a stand-in smartctl:
// it prints the variable's value as smartctl's JSON and exits with status 4
// (bit 2, "SMART or other ATA command failed" — a non-fatal warning). See
// fakeSmartctl.
const fakeSmartctlEnv = "BEWITCH_TEST_FAKE_SMARTCTL_JSON"

// fakeIntelGPUTopEnv, when set, turns the test binary into a stand-in
// intel_gpu_top: "denied" fails the way it does without permission for the
// i915 PMU; "stream" emits JSON samples and keeps running until killed.
const fakeIntelGPUTopEnv = "BEWITCH_TEST_FAKE_INTEL_GPU_TOP"

func TestMain(m *testing.M) {
	if out, ok := os.LookupEnv(fakeSmartctlEnv); ok {
		os.Stdout.WriteString(out + "\n")
		os.Exit(4)
	}
	switch os.Getenv(fakeIntelGPUTopEnv) {
	case "denied":
		os.Stderr.WriteString("Failed to initialize PMU! (Permission denied)\n\nWhen running as a normal user CAP_PERFMON is required\n")
		os.Exit(1)
	case "stream":
		os.Stdout.WriteString("[\n")
		for i := 0; ; i++ {
			os.Stdout.WriteString(`{"period": {"duration": 1000.0, "unit": "ms"}, "frequency": {"requested": 1200.0, "actual": 1100.0, "unit": "MHz"}, "engines": {"Render/3D": {"busy": 42.5, "unit": "%"}}},` + "\n")
			time.Sleep(50 * time.Millisecond)
		}
	}
	os.Exit(m.Run())
}
