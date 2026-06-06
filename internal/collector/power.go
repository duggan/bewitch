package collector

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// powercapRoot is the sysfs powercap directory. A package var so tests can point
// discovery at a fixture tree.
var powercapRoot = "/sys/class/powercap"

type PowerZoneSample struct {
	Zone  string
	Watts float64
}

type PowerData struct {
	Zones []PowerZoneSample
}

type zonePath struct {
	energyPath string
	name       string
	maxRange   int64 // max_energy_range_uj: energy_uj wraps back to 0 at this value
}

type PowerCollector struct {
	zones    []zonePath
	prev     map[string]int64 // zone path -> energy_uj
	prevTime time.Time
	sysfsCache
}

func NewPowerCollector() *PowerCollector {
	c := &PowerCollector{
		zones: make([]zonePath, 0, 8),
	}
	c.discoverZones()
	return c
}

func (c *PowerCollector) Name() string { return "power" }

func (c *PowerCollector) discoverZones() {
	c.zones = c.zones[:0]

	// /sys/class/powercap is a flat directory of symlinks to every RAPL domain,
	// so a sub-domain (intel-rapl:0:0) appears BOTH as a bare top-level entry
	// (matched by the main glob, named e.g. "core") AND under its parent package
	// (matched by the sub glob, named "package-0/core") — the same physical
	// energy_uj counter twice. Dedupe by the resolved real path, keeping the more
	// qualified (hierarchical) name. Genuinely-distinct counters (multi-socket
	// package-1, the separate intel-rapl-mmio control type) have distinct real
	// paths and are preserved.
	byReal := make(map[string]zonePath)
	add := func(z zonePath) {
		key := z.energyPath
		if real, err := filepath.EvalSymlinks(z.energyPath); err == nil {
			key = real
		}
		if existing, ok := byReal[key]; ok {
			// Same counter seen twice: prefer the hierarchical name.
			if !strings.Contains(existing.name, "/") && strings.Contains(z.name, "/") {
				byReal[key] = z
			}
			return
		}
		byReal[key] = z
	}

	// Main zones (top-level domains, plus the bare sub-domain aliases).
	mainZones, _ := filepath.Glob(filepath.Join(powercapRoot, "*/energy_uj"))
	for _, p := range mainZones {
		dir := filepath.Dir(p)
		name := readString(filepath.Join(dir, "name"))
		if name == "" {
			name = filepath.Base(dir)
		}
		// The intel-rapl-mmio control type exposes a same-named (e.g. "package-0")
		// but physically distinct counter; disambiguate so it doesn't collide.
		if strings.HasPrefix(filepath.Base(dir), "intel-rapl-mmio") {
			name = "mmio/" + name
		}
		maxRange, _ := strconv.ParseInt(readString(filepath.Join(dir, "max_energy_range_uj")), 10, 64)
		add(zonePath{energyPath: p, name: name, maxRange: maxRange})
	}

	// Sub-zones (e.g. intel-rapl:0:0) — named "parent/child" for clarity.
	subZones, _ := filepath.Glob(filepath.Join(powercapRoot, "*/intel-rapl:*:*/energy_uj"))
	for _, p := range subZones {
		dir := filepath.Dir(p)
		name := readString(filepath.Join(dir, "name"))
		if name == "" {
			name = filepath.Base(dir)
		}
		parentDir := filepath.Dir(dir)
		parentName := readString(filepath.Join(parentDir, "name"))
		if parentName != "" {
			name = parentName + "/" + name
		}
		maxRange, _ := strconv.ParseInt(readString(filepath.Join(dir, "max_energy_range_uj")), 10, 64)
		add(zonePath{energyPath: p, name: name, maxRange: maxRange})
	}

	for _, z := range byReal {
		c.zones = append(c.zones, z)
	}
	// Stable order so dimension IDs and tests are deterministic.
	sort.Slice(c.zones, func(i, j int) bool { return c.zones[i].name < c.zones[j].name })

	c.markRefreshed()
}

func (c *PowerCollector) Collect() (Sample, error) {
	now := time.Now()

	if c.needsRefresh(len(c.zones)) {
		c.discoverZones()
	}

	// Read current energy values
	cur := make(map[string]int64, len(c.zones))
	for _, z := range c.zones {
		val, err := strconv.ParseInt(strings.TrimSpace(readStringFile(z.energyPath)), 10, 64)
		if err != nil {
			continue
		}
		cur[z.energyPath] = val
	}

	var zones []PowerZoneSample

	if c.prev != nil {
		dt := now.Sub(c.prevTime).Seconds()
		if dt > 0 {
			for _, z := range c.zones {
				curVal, ok := cur[z.energyPath]
				if !ok {
					continue
				}
				prevVal, ok := c.prev[z.energyPath]
				if !ok {
					continue
				}
				delta := curVal - prevVal
				if delta < 0 {
					// energy_uj is a fixed-width counter that wraps back to 0 at
					// max_energy_range_uj (~262 J on many package/core domains —
					// every few seconds at tens of watts). Recover the real delta
					// across the wrap instead of dropping the sample, which made
					// high-draw zones vanish from the chart exactly under load.
					if z.maxRange > 0 {
						delta += z.maxRange
					}
					if delta < 0 {
						continue // no wrap range, or skew beyond one wrap — can't trust it
					}
				}
				watts := float64(delta) / dt / 1e6
				zones = append(zones, PowerZoneSample{
					Zone:  z.name,
					Watts: watts,
				})
			}
		}
	}

	c.prev = cur
	c.prevTime = now

	return Sample{
		Timestamp: now,
		Kind:      "power",
		Data:      PowerData{Zones: zones},
	}, nil
}
