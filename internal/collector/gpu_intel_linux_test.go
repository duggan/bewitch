//go:build linux

package collector

import (
	"testing"
	"time"
)

// TestIntelGPULive reads the real Intel GPU when the host has one; skips
// otherwise. Run it with a GPU workload going to see non-zero utilization.
func TestIntelGPULive(t *testing.T) {
	b := newIntelGPUBackend()
	if b == nil {
		t.Skip("no i915/xe GPU")
	}
	if _, err := b.read(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	devs, err := b.read()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("clients=%d", len(b.clients))
	for _, d := range devs {
		t.Logf("%s: busy %.1f%%, %d/%d MHz, RC6 %.1f%%, %.2f W", d.Name, d.UtilizationPct, d.FrequencyMHz, d.FrequencyMaxMHz, d.ThrottlePct, d.PowerWatts)
	}
}
