package collector

import (
	"fmt"
	"time"

	"github.com/prometheus/procfs"
)

type MemoryData struct {
	TotalBytes     uint64
	UsedBytes      uint64
	AvailableBytes uint64
	BuffersBytes   uint64
	CachedBytes    uint64
	SwapTotalBytes uint64
	SwapUsedBytes  uint64
}

type MemoryCollector struct {
	fs procfs.FS
}

func NewMemoryCollector() (*MemoryCollector, error) {
	fs, err := procfs.NewDefaultFS()
	if err != nil {
		return nil, fmt.Errorf("creating procfs: %w", err)
	}
	return &MemoryCollector{fs: fs}, nil
}

func (c *MemoryCollector) Name() string { return "memory" }

func (c *MemoryCollector) Collect() (Sample, error) {
	info, err := c.fs.Meminfo()
	if err != nil {
		return Sample{}, fmt.Errorf("reading meminfo: %w", err)
	}

	total := ptrVal(info.MemTotal) * 1024
	available := ptrVal(info.MemAvailable) * 1024
	buffers := ptrVal(info.Buffers) * 1024
	cached := ptrVal(info.Cached) * 1024
	swapTotal := ptrVal(info.SwapTotal) * 1024
	swapFree := ptrVal(info.SwapFree) * 1024

	return Sample{
		Timestamp: time.Now(),
		Kind:      "memory",
		Data: MemoryData{
			TotalBytes:     total,
			UsedBytes:      total - available,
			AvailableBytes: available,
			BuffersBytes:   buffers,
			CachedBytes:    cached,
			SwapTotalBytes: swapTotal,
			SwapUsedBytes:  swapTotal - swapFree,
		},
	}, nil
}

func ptrVal(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}
