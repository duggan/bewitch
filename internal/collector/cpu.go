package collector

import (
	"fmt"
	"sort"
	"time"

	"github.com/prometheus/procfs"
)

// CPUCollector collects CPU usage metrics via /proc/stat.
type CPUCollector struct {
	fs        procfs.FS
	prev      map[int64]procfs.CPUStat
	prevTotal procfs.CPUStat
}

func NewCPUCollector() (*CPUCollector, error) {
	fs, err := procfs.NewDefaultFS()
	if err != nil {
		return nil, fmt.Errorf("creating procfs: %w", err)
	}
	stat, err := fs.Stat()
	if err != nil {
		return nil, fmt.Errorf("initial stat read: %w", err)
	}
	return &CPUCollector{
		fs:        fs,
		prev:      stat.CPU,
		prevTotal: stat.CPUTotal,
	}, nil
}

func (c *CPUCollector) Name() string { return "cpu" }

func (c *CPUCollector) Collect() (Sample, error) {
	stat, err := c.fs.Stat()
	if err != nil {
		return Sample{}, fmt.Errorf("reading /proc/stat: %w", err)
	}

	now := time.Now()
	cores := make([]CPUCoreSample, 0, len(stat.CPU))

	// Sort keys for deterministic output
	keys := make([]int64, 0, len(stat.CPU))
	for k := range stat.CPU {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	for _, k := range keys {
		cur := stat.CPU[k]
		prev, ok := c.prev[k]
		if !ok {
			continue
		}
		cs := computeCPUPct(prev, cur, int(k))
		cores = append(cores, cs)
	}

	// Compute aggregate from CPUTotal
	agg := computeCPUPct(c.prevTotal, stat.CPUTotal, -1)
	cores = append([]CPUCoreSample{agg}, cores...)

	c.prev = stat.CPU
	c.prevTotal = stat.CPUTotal

	return Sample{
		Timestamp: now,
		Kind:      "cpu",
		Data:      CPUData{Cores: cores},
	}, nil
}

func computeCPUPct(prev, cur procfs.CPUStat, coreIdx int) CPUCoreSample {
	prevTotal := cpuTotal(prev)
	curTotal := cpuTotal(cur)
	delta := curTotal - prevTotal
	if delta == 0 {
		return CPUCoreSample{Core: coreIdx}
	}

	return CPUCoreSample{
		Core:      coreIdx,
		UserPct:   (cur.User - prev.User) / delta * 100,
		SystemPct: (cur.System - prev.System) / delta * 100,
		IdlePct:   (cur.Idle - prev.Idle) / delta * 100,
		IOWaitPct: (cur.Iowait - prev.Iowait) / delta * 100,
	}
}

func cpuTotal(s procfs.CPUStat) float64 {
	return s.User + s.Nice + s.System + s.Idle + s.Iowait + s.IRQ + s.SoftIRQ + s.Steal
}
