package collector

import (
	"fmt"
	"sort"
	"time"

	"github.com/prometheus/procfs"
)

type NetIfaceSample struct {
	Interface    string
	RxBytesSec  float64
	TxBytesSec  float64
	RxPacketsSec float64
	TxPacketsSec float64
	RxErrors     uint64
	TxErrors     uint64
}

type NetworkData struct {
	Interfaces []NetIfaceSample
}

type NetworkCollector struct {
	fs       procfs.FS
	prev     map[string]procfs.NetDevLine
	prevTime time.Time
}

func NewNetworkCollector() (*NetworkCollector, error) {
	fs, err := procfs.NewDefaultFS()
	if err != nil {
		return nil, fmt.Errorf("creating procfs: %w", err)
	}
	netDev, err := fs.NetDev()
	if err != nil {
		return nil, fmt.Errorf("initial netdev read: %w", err)
	}
	prev := make(map[string]procfs.NetDevLine, len(netDev))
	for name, line := range netDev {
		prev[name] = line
	}
	return &NetworkCollector{fs: fs, prev: prev, prevTime: time.Now()}, nil
}

func (c *NetworkCollector) Name() string { return "network" }

func (c *NetworkCollector) Collect() (Sample, error) {
	now := time.Now()
	dt := now.Sub(c.prevTime).Seconds()
	if dt == 0 {
		dt = 1
	}

	netDev, err := c.fs.NetDev()
	if err != nil {
		return Sample{}, fmt.Errorf("reading netdev: %w", err)
	}

	ifaces := make([]NetIfaceSample, 0, len(netDev))
	for name, cur := range netDev {
		if name == "lo" {
			continue
		}
		prev, ok := c.prev[name]
		if !ok {
			continue
		}
		ifaces = append(ifaces, NetIfaceSample{
			Interface:    name,
			RxBytesSec:  float64(cur.RxBytes-prev.RxBytes) / dt,
			TxBytesSec:  float64(cur.TxBytes-prev.TxBytes) / dt,
			RxPacketsSec: float64(cur.RxPackets-prev.RxPackets) / dt,
			TxPacketsSec: float64(cur.TxPackets-prev.TxPackets) / dt,
			RxErrors:     cur.RxErrors,
			TxErrors:     cur.TxErrors,
		})
	}

	sort.Slice(ifaces, func(i, j int) bool {
		return ifaces[i].Interface < ifaces[j].Interface
	})

	cur := make(map[string]procfs.NetDevLine, len(netDev))
	for name, line := range netDev {
		cur[name] = line
	}
	c.prev = cur
	c.prevTime = now

	return Sample{
		Timestamp: now,
		Kind:      "network",
		Data:      NetworkData{Interfaces: ifaces},
	}, nil
}
