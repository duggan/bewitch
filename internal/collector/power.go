package collector

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"
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

// energyZone is a RAPL domain read as cumulative joules, from either a powercap
// energy_uj file or a perf counter.
type energyZone struct {
	name  string
	wrapJ float64 // the counter wraps back to 0 at this many joules; 0 = never wraps
	read  func() (float64, error)
}

type PowerCollector struct {
	zones    []energyZone
	source   string             // "powercap", "perf", or "" (none readable)
	prev     map[string]float64 // zone name -> cumulative joules
	prevTime time.Time
	sysfsCache

	perf      []*perfCounter // kept open across rediscovery (opened once)
	perfZones []energyZone
	perfTried bool
	warned    bool
}

func NewPowerCollector() *PowerCollector {
	c := &PowerCollector{
		zones: make([]energyZone, 0, 8),
	}
	c.discoverZones()
	return c
}

func (c *PowerCollector) Name() string { return "power" }

// discoverZones picks the energy source: powercap when its energy_uj files are
// readable (root, or a host that relaxed them — keeps mmio/psys coverage), else
// the RAPL perf events (CAP_PERFMON; the packaged unprivileged daemon's path).
func (c *PowerCollector) discoverZones() {
	defer c.markRefreshed()

	var pcErr error
	if pc := discoverPowercapZones(); len(pc) > 0 {
		if _, pcErr = pc[0].read(); pcErr == nil {
			c.useZones("powercap", pc)
			return
		}
	}

	if !c.perfTried {
		c.perfTried = true
		perfErr := c.openPerfZones()
		// Stay quiet on hosts with no RAPL at all (VMs, ARM): no powercap zones and
		// no power PMU. Warn when counters exist but neither path can read them.
		if perfErr != nil && (pcErr != nil || !errors.Is(perfErr, os.ErrNotExist)) && !c.warned {
			c.warned = true
			log.Warnf("power: RAPL energy counters unreadable — powercap: %v; perf: %v. "+
				"powercap energy_uj is root-only since kernel 5.10 (CVE-2020-8694); the perf "+
				"path needs CAP_PERFMON (granted by the packaged unit) or perf_event_paranoid <= 0", pcErr, perfErr)
		}
	}
	if len(c.perfZones) > 0 {
		c.useZones("perf", c.perfZones)
		return
	}
	c.useZones("", nil)
}

func (c *PowerCollector) useZones(source string, zones []energyZone) {
	if source != c.source {
		if source != "" {
			log.Infof("power: reading RAPL energy via %s (%d zones)", source, len(zones))
		}
		c.prev = nil // different counters: don't difference across a switch
		c.source = source
	}
	c.zones = zones
}

// openPerfZones opens one perf counter per RAPL event and package.
func (c *PowerCollector) openPerfZones() error {
	events, err := discoverPerfRAPL()
	if err != nil {
		return err
	}
	for _, ev := range events {
		pc, err := openPerfCounter(ev)
		if err != nil {
			for _, open := range c.perf {
				open.close()
			}
			c.perf, c.perfZones = nil, nil
			return err
		}
		c.perf = append(c.perf, pc)
		c.perfZones = append(c.perfZones, energyZone{name: ev.zone, read: pc.joules})
	}
	return nil
}

// discoverPowercapZones lists the powercap RAPL domains as energy zones.
func discoverPowercapZones() []energyZone {
	var zones []zonePath

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
		zones = append(zones, z)
	}
	// Stable order so dimension IDs and tests are deterministic.
	sort.Slice(zones, func(i, j int) bool { return zones[i].name < zones[j].name })

	out := make([]energyZone, 0, len(zones))
	for _, z := range zones {
		path := z.energyPath
		out = append(out, energyZone{
			name:  z.name,
			wrapJ: float64(z.maxRange) / 1e6,
			read: func() (float64, error) {
				data, err := os.ReadFile(path)
				if err != nil {
					return 0, err
				}
				uj, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
				return float64(uj) / 1e6, err
			},
		})
	}
	return out
}

func (c *PowerCollector) Collect() (Sample, error) {
	now := time.Now()

	if c.needsRefresh(len(c.zones)) {
		c.discoverZones()
	}

	// Read current cumulative energy (joules) per zone.
	cur := make(map[string]float64, len(c.zones))
	for _, z := range c.zones {
		if j, err := z.read(); err == nil {
			cur[z.name] = j
		}
	}

	var zones []PowerZoneSample

	if c.prev != nil {
		dt := now.Sub(c.prevTime).Seconds()
		if dt > 0 {
			for _, z := range c.zones {
				curVal, ok := cur[z.name]
				if !ok {
					continue
				}
				prevVal, ok := c.prev[z.name]
				if !ok {
					continue
				}
				delta := curVal - prevVal
				if delta < 0 {
					// powercap energy_uj is a fixed-width counter that wraps back to 0
					// at max_energy_range_uj (~262 J on many package/core domains —
					// every few seconds at tens of watts). Recover the real delta
					// across the wrap instead of dropping the sample, which made
					// high-draw zones vanish from the chart exactly under load.
					// (perf counters are 64-bit and don't wrap: wrapJ is 0.)
					if z.wrapJ > 0 {
						delta += z.wrapJ
					}
					if delta < 0 {
						continue // no wrap range, or skew beyond one wrap — can't trust it
					}
				}
				zones = append(zones, PowerZoneSample{
					Zone:  z.name,
					Watts: delta / dt,
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
