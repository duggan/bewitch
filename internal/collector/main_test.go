package collector

import (
	"os"
	"testing"
)

// fakeSmartctlEnv, when set, turns the test binary into a stand-in smartctl:
// it prints the variable's value as smartctl's JSON and exits with status 4
// (bit 2, "SMART or other ATA command failed" — a non-fatal warning). See
// fakeSmartctl.
const fakeSmartctlEnv = "BEWITCH_TEST_FAKE_SMARTCTL_JSON"

func TestMain(m *testing.M) {
	if out, ok := os.LookupEnv(fakeSmartctlEnv); ok {
		os.Stdout.WriteString(out + "\n")
		os.Exit(4)
	}
	os.Exit(m.Run())
}
