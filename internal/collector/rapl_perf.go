package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// RAPL energy via perf events.
//
// Since kernel 5.10 (CVE-2020-8694, the PLATYPUS side channel) powercap's
// energy_uj files are mode 0400 root, so the packaged daemon — running as the
// unprivileged bewitch user — can't read them, and power monitoring silently
// reported nothing. The same counters are exported by the "power" perf PMU,
// which perf_event_open lets a CAP_PERFMON holder read system-wide; the
// packaged unit already grants CAP_PERFMON (for GPU/eBPF). The PMU's sysfs
// metadata (type, cpumask, events/*) is world-readable, and its counters are
// 64-bit (the kernel accumulates the 32-bit MSR), so there is no wrap to undo.

// perfPowerRoot and cpuSysRoot are package vars so tests can use fixture trees.
var (
	perfPowerRoot = "/sys/bus/event_source/devices/power"
	cpuSysRoot    = "/sys/devices/system/cpu"
)

// raplEventZones maps power-PMU event names to the powercap sub-zone name they
// correspond to, so a host switching sources keeps the same zone (dimension)
// names: package-N, package-N/core, package-N/uncore, package-N/dram, psys.
// "" means the package zone itself.
var raplEventZones = map[string]string{
	"energy-pkg":   "",
	"energy-cores": "core",
	"energy-gpu":   "uncore",
	"energy-ram":   "dram",
	"energy-psys":  "psys",
}

// perfRAPLEvent is one counter to open: an event on one package's CPU.
type perfRAPLEvent struct {
	zone   string
	pmu    uint32
	config uint64
	scale  float64 // joules per count
	cpu    int
}

// discoverPerfRAPL lists the RAPL perf events to open, one per (event,
// package). It returns os.ErrNotExist (wrapped) when the host has no power PMU.
func discoverPerfRAPL() ([]perfRAPLEvent, error) {
	typ, err := os.ReadFile(filepath.Join(perfPowerRoot, "type"))
	if err != nil {
		return nil, fmt.Errorf("power PMU: %w", err)
	}
	pmu, err := strconv.ParseUint(strings.TrimSpace(string(typ)), 10, 32)
	if err != nil {
		return nil, fmt.Errorf("power PMU type %q: %w", strings.TrimSpace(string(typ)), err)
	}
	// One CPU per package: RAPL counters are package-scoped, and cpumask names
	// the CPU to open each package's counter on.
	cpus := parseCPUList(readString(filepath.Join(perfPowerRoot, "cpumask")))
	if len(cpus) == 0 {
		return nil, fmt.Errorf("power PMU: empty cpumask")
	}

	var events []perfRAPLEvent
	for name, suffix := range raplEventZones {
		evPath := filepath.Join(perfPowerRoot, "events", name)
		raw := readString(evPath)
		if raw == "" {
			continue // event not supported on this CPU
		}
		cfg, err := parseEventConfig(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		scale, err := strconv.ParseFloat(readString(evPath+".scale"), 64)
		if err != nil || scale <= 0 {
			return nil, fmt.Errorf("%s.scale: unparseable", name)
		}
		for i, cpu := range cpus {
			zone := ""
			if suffix == "psys" {
				// Platform-wide, not per package: open it once.
				if i > 0 {
					break
				}
				zone = "psys"
			} else {
				pkg := readString(filepath.Join(cpuSysRoot, fmt.Sprintf("cpu%d", cpu), "topology", "physical_package_id"))
				if pkg == "" {
					pkg = strconv.Itoa(i)
				}
				zone = "package-" + pkg
				if suffix != "" {
					zone += "/" + suffix
				}
			}
			events = append(events, perfRAPLEvent{zone: zone, pmu: uint32(pmu), config: cfg, scale: scale, cpu: cpu})
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].zone < events[j].zone })
	return events, nil
}

// parseEventConfig parses a perf sysfs event spec such as "event=0x02".
func parseEventConfig(s string) (uint64, error) {
	for _, term := range strings.Split(strings.TrimSpace(s), ",") {
		k, v, ok := strings.Cut(term, "=")
		if ok && strings.TrimSpace(k) == "event" {
			return strconv.ParseUint(strings.TrimSpace(v), 0, 64)
		}
	}
	return 0, fmt.Errorf("no event= term in %q", s)
}

// parseCPUList parses a kernel CPU list ("0", "0,24", "0-3,8").
func parseCPUList(s string) []int {
	var cpus []int
	for _, part := range strings.Split(strings.TrimSpace(s), ",") {
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		a, err := strconv.Atoi(lo)
		if err != nil {
			continue
		}
		b := a
		if isRange {
			if b, err = strconv.Atoi(hi); err != nil {
				continue
			}
		}
		for c := a; c <= b; c++ {
			cpus = append(cpus, c)
		}
	}
	return cpus
}
