package collector

import (
	"fmt"
	"time"

	"github.com/prometheus/procfs"
)

// LoadData holds the system load averages over 1, 5, and 15 minutes.
type LoadData struct {
	Load1  float64
	Load5  float64
	Load15 float64
}

// LoadCollector reads /proc/loadavg.
type LoadCollector struct {
	fs procfs.FS
}

func NewLoadCollector() (*LoadCollector, error) {
	fs, err := newProcFS()
	if err != nil {
		return nil, err
	}
	return &LoadCollector{fs: fs}, nil
}

func (c *LoadCollector) Name() string { return "load" }

func (c *LoadCollector) Collect() (Sample, error) {
	avg, err := c.fs.LoadAvg()
	if err != nil {
		return Sample{}, fmt.Errorf("reading loadavg: %w", err)
	}
	return Sample{
		Timestamp: time.Now(),
		Kind:      "load",
		Data: LoadData{
			Load1:  avg.Load1,
			Load5:  avg.Load5,
			Load15: avg.Load15,
		},
	}, nil
}
