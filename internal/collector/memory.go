package collector

import (
	"fmt"
	"os"
	"strconv"
	"strings"
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
	fs, err := newProcFS()
	if err != nil {
		return nil, err
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

	// The ZFS ARC is a cache, but the kernel doesn't count it in Cached or
	// MemAvailable, so on a ZFS host it would read as used memory (often most of
	// RAM). Count it as cache, and the part above its floor (c_min, which the
	// ARC gives back under memory pressure) as available.
	if size, cMin, ok := readARCStats(); ok {
		cached += size
		if size > cMin {
			available += size - cMin
		}
		if available > total {
			available = total
		}
	}

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

// arcstatsPath is a package var so tests can point it at a fixture.
var arcstatsPath = "/proc/spl/kstat/zfs/arcstats"

// readARCStats returns the ZFS ARC's current size and minimum size in bytes;
// ok is false when ZFS isn't loaded.
func readARCStats() (size, cMin uint64, ok bool) {
	data, err := os.ReadFile(arcstatsPath)
	if err != nil {
		return 0, 0, false
	}
	var haveSize bool
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		switch f[0] {
		case "size":
			size, err = strconv.ParseUint(f[2], 10, 64)
			haveSize = err == nil
		case "c_min":
			cMin, _ = strconv.ParseUint(f[2], 10, 64)
		}
	}
	return size, cMin, haveSize
}
