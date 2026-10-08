package collector

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseCPUList(t *testing.T) {
	cases := map[string][]int{
		"0":       {0},
		"0,24":    {0, 24},
		"0-3,8\n": {0, 1, 2, 3, 8},
		"":        nil,
	}
	for in, want := range cases {
		if got := parseCPUList(in); !reflect.DeepEqual(got, want) {
			t.Errorf("parseCPUList(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseEventConfig(t *testing.T) {
	if v, err := parseEventConfig("event=0x02\n"); err != nil || v != 2 {
		t.Errorf("event=0x02 -> %d, %v", v, err)
	}
	if v, err := parseEventConfig("event=0x04,umask=0x1"); err != nil || v != 4 {
		t.Errorf("event=0x04,umask -> %d, %v", v, err)
	}
	if _, err := parseEventConfig("umask=0x1"); err == nil {
		t.Error("missing event= should error")
	}
}

// TestDiscoverPerfRAPL builds a two-socket power PMU fixture. Zone names must
// match powercap's (package-N, package-N/core, package-N/dram) so history and
// alert scopes survive a switch between sources, and the platform-wide psys
// counter is opened once, not per package.
func TestDiscoverPerfRAPL(t *testing.T) {
	root := t.TempDir()
	pmu := filepath.Join(root, "power")
	cpu := filepath.Join(root, "cpu")
	write := func(p, v string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(pmu, "type"), "29")
	write(filepath.Join(pmu, "cpumask"), "0,24")
	for ev, code := range map[string]string{"energy-pkg": "0x02", "energy-cores": "0x01", "energy-ram": "0x03", "energy-psys": "0x05"} {
		write(filepath.Join(pmu, "events", ev), "event="+code)
		write(filepath.Join(pmu, "events", ev+".scale"), "2.3283064365386962890625e-10")
		write(filepath.Join(pmu, "events", ev+".unit"), "Joules")
	}
	write(filepath.Join(cpu, "cpu0", "topology", "physical_package_id"), "0")
	write(filepath.Join(cpu, "cpu24", "topology", "physical_package_id"), "1")

	oldP, oldC := perfPowerRoot, cpuSysRoot
	perfPowerRoot, cpuSysRoot = pmu, cpu
	t.Cleanup(func() { perfPowerRoot, cpuSysRoot = oldP, oldC })

	events, err := discoverPerfRAPL()
	if err != nil {
		t.Fatal(err)
	}
	var zones []string
	for _, e := range events {
		zones = append(zones, e.zone)
		if e.pmu != 29 || e.scale <= 0 {
			t.Errorf("event %+v: bad pmu/scale", e)
		}
		if e.zone == "package-1" && (e.cpu != 24 || e.config != 2) {
			t.Errorf("package-1 = %+v, want cpu 24 event 0x02", e)
		}
	}
	want := "package-0,package-0/core,package-0/dram,package-1,package-1/core,package-1/dram,psys"
	if got := strings.Join(zones, ","); got != want {
		t.Errorf("zones = %s, want %s", got, want)
	}

	perfPowerRoot = filepath.Join(root, "missing")
	if _, err := discoverPerfRAPL(); err == nil || !os.IsNotExist(unwrapAll(err)) {
		t.Errorf("no PMU should report not-exist, got %v", err)
	}
}

func unwrapAll(err error) error {
	for {
		u, ok := err.(interface{ Unwrap() error })
		if !ok || u.Unwrap() == nil {
			return err
		}
		err = u.Unwrap()
	}
}

// TestPowerCollectorRates drives Collect with fake energy zones: watts from the
// joule delta, powercap wrap recovery, perf counters (no wrap), and no bogus
// sample when the source switches between unrelated counters.
func TestPowerCollectorRates(t *testing.T) {
	var pkgJ, wrapJ float64
	c := &PowerCollector{}
	c.useZones("perf", []energyZone{
		{name: "package-0", read: func() (float64, error) { return pkgJ, nil }},
		{name: "package-0/core", wrapJ: 262.0, read: func() (float64, error) { return wrapJ, nil }},
	})
	c.markRefreshed()

	pkgJ, wrapJ = 1000, 250
	if s, _ := c.Collect(); len(s.Data.(PowerData).Zones) != 0 {
		t.Fatal("first sample has no previous reading; expected no zones")
	}
	c.prevTime = time.Now().Add(-2 * time.Second)
	pkgJ, wrapJ = 1100, 10 // +100 J; core wrapped: 250 -> 262 -> 10 = +22 J
	s, _ := c.Collect()
	got := map[string]float64{}
	for _, z := range s.Data.(PowerData).Zones {
		got[z.Zone] = z.Watts
	}
	if math.Abs(got["package-0"]-50) > 1 {
		t.Errorf("package-0 = %.2f W, want ~50", got["package-0"])
	}
	if math.Abs(got["package-0/core"]-11) > 0.5 {
		t.Errorf("package-0/core = %.2f W, want ~11 (wrap recovered)", got["package-0/core"])
	}

	// Switching source must not difference the old counter against the new one.
	c.useZones("powercap", []energyZone{{name: "package-0", read: func() (float64, error) { return 5, nil }}})
	if s, _ := c.Collect(); len(s.Data.(PowerData).Zones) != 0 {
		t.Errorf("sample right after a source switch = %+v, want none", s.Data)
	}
}
