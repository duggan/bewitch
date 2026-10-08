//go:build linux

package collector

import (
	"testing"
	"time"
)

// TestPerfRAPLLive opens the real power PMU when the host has one and the test
// may use it (CAP_PERFMON or perf_event_paranoid <= 0); skips otherwise (VMs,
// ARM, CI runners).
func TestPerfRAPLLive(t *testing.T) {
	events, err := discoverPerfRAPL()
	if err != nil || len(events) == 0 {
		t.Skipf("no power PMU: %v", err)
	}
	pc, err := openPerfCounter(events[0])
	if err != nil {
		t.Skipf("cannot open %s (needs CAP_PERFMON or perf_event_paranoid <= 0): %v", events[0].zone, err)
	}
	defer pc.close()
	a, err := pc.joules()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	b, err := pc.joules()
	if err != nil {
		t.Fatal(err)
	}
	if b < a {
		t.Errorf("%s went backwards: %v -> %v", events[0].zone, a, b)
	}
	t.Logf("%s: %.3f W", events[0].zone, (b-a)/0.2)
}

// TestPowerCollectorLive runs the real collector: as root it should pick
// powercap; as the unprivileged packaged user (energy_uj is 0400 root) it must
// fall back to perf and still report watts.
func TestPowerCollectorLive(t *testing.T) {
	c := NewPowerCollector()
	if len(c.zones) == 0 {
		t.Skip("no readable RAPL source on this host")
	}
	c.Collect()
	time.Sleep(500 * time.Millisecond)
	s, err := c.Collect()
	if err != nil {
		t.Fatal(err)
	}
	zones := s.Data.(PowerData).Zones
	if len(zones) == 0 {
		t.Fatalf("source %s: no zone readings", c.source)
	}
	for _, z := range zones {
		t.Logf("source=%s %s: %.2f W", c.source, z.Zone, z.Watts)
	}
}
