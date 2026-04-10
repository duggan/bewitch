package collector

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"
)


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

	// Main zones
	mainZones, _ := filepath.Glob("/sys/class/powercap/*/energy_uj")
	for _, p := range mainZones {
		dir := filepath.Dir(p)
		name := readString(filepath.Join(dir, "name"))
		if name == "" {
			name = filepath.Base(dir)
		}
		c.zones = append(c.zones, zonePath{
			energyPath: p,
			name:       name,
		})
	}

	// Sub-zones (e.g. intel-rapl:0:0)
	subZones, _ := filepath.Glob("/sys/class/powercap/*/intel-rapl:*:*/energy_uj")
	for _, p := range subZones {
		dir := filepath.Dir(p)
		name := readString(filepath.Join(dir, "name"))
		if name == "" {
			name = filepath.Base(dir)
		}
		// Prefix with parent zone name for clarity
		parentDir := filepath.Dir(dir)
		parentName := readString(filepath.Join(parentDir, "name"))
		if parentName != "" {
			name = parentName + "/" + name
		}
		c.zones = append(c.zones, zonePath{
			energyPath: p,
			name:       name,
		})
	}

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
					// Counter wrapped
					continue
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
