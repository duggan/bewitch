package collector

import (
	"math"
	"math/rand"
	"path/filepath"
	"sync"
	"time"
)

// smoothWave returns a value oscillating between min and max using a sine wave
// with the given period (seconds) and phase offset, plus small random jitter.
func smoothWave(min, max, periodSec, phase float64) float64 {
	t := float64(time.Now().UnixNano()) / 1e9
	base := (math.Sin(t*2*math.Pi/periodSec+phase) + 1) / 2 // [0, 1]
	noise := (rand.Float64() - 0.5) * 0.05                  // +/- 2.5%
	val := min + (max-min)*(base+noise)
	return math.Max(min, math.Min(max, val))
}

// The mock collectors read the shared MockSim (mocksim.go), which models the
// whole host from a few underlying activities so every reading agrees with the
// others and with the seeded history.

// --- CPU ---

type MockCPUCollector struct{ sim *MockSim }

func NewMockCPUCollector(sim *MockSim) *MockCPUCollector { return &MockCPUCollector{sim: sim} }
func (c *MockCPUCollector) Name() string                 { return "cpu" }

func (c *MockCPUCollector) Collect() (Sample, error) {
	snap := c.sim.Now()
	return Sample{Timestamp: snap.Time, Kind: "cpu", Data: snap.CPU}, nil
}

// --- Memory ---

type MockMemoryCollector struct{ sim *MockSim }

func NewMockMemoryCollector(sim *MockSim) *MockMemoryCollector { return &MockMemoryCollector{sim: sim} }
func (c *MockMemoryCollector) Name() string                    { return "memory" }

func (c *MockMemoryCollector) Collect() (Sample, error) {
	snap := c.sim.Now()
	return Sample{Timestamp: snap.Time, Kind: "memory", Data: snap.Memory}, nil
}

// --- Load ---

type MockLoadCollector struct{ sim *MockSim }

func NewMockLoadCollector(sim *MockSim) *MockLoadCollector { return &MockLoadCollector{sim: sim} }
func (c *MockLoadCollector) Name() string                  { return "load" }

func (c *MockLoadCollector) Collect() (Sample, error) {
	snap := c.sim.Now()
	return Sample{Timestamp: snap.Time, Kind: "load", Data: snap.Load}, nil
}

// --- Disk ---

// MockDiskCollector reports SMART only on refresh cycles, like the real
// collector (smart_interval, default 5m), so SMART is persisted at that cadence.
type MockDiskCollector struct {
	sim       *MockSim
	lastSMART time.Time
}

func NewMockDiskCollector(sim *MockSim) *MockDiskCollector { return &MockDiskCollector{sim: sim} }
func (c *MockDiskCollector) Name() string                  { return "disk" }

func (c *MockDiskCollector) Collect() (Sample, error) {
	snap := c.sim.Now()
	data := snap.Disk
	if snap.Time.Sub(c.lastSMART) >= 5*time.Minute {
		c.lastSMART = snap.Time
	} else {
		data.SMART = nil
	}
	return Sample{Timestamp: snap.Time, Kind: "disk", Data: data}, nil
}

// --- Network ---

type MockNetworkCollector struct{ sim *MockSim }

func NewMockNetworkCollector(sim *MockSim) *MockNetworkCollector {
	return &MockNetworkCollector{sim: sim}
}
func (c *MockNetworkCollector) Name() string { return "network" }

func (c *MockNetworkCollector) Collect() (Sample, error) {
	snap := c.sim.Now()
	return Sample{Timestamp: snap.Time, Kind: "network", Data: snap.Network}, nil
}

// --- ECC ---

type MockECCCollector struct{ sim *MockSim }

func NewMockECCCollector(sim *MockSim) *MockECCCollector { return &MockECCCollector{sim: sim} }
func (c *MockECCCollector) Name() string                 { return "ecc" }

func (c *MockECCCollector) Collect() (Sample, error) {
	snap := c.sim.Now()
	return Sample{Timestamp: snap.Time, Kind: "ecc", Data: snap.ECC}, nil
}

// --- Temperature ---

type MockTemperatureCollector struct{ sim *MockSim }

func NewMockTemperatureCollector(sim *MockSim) *MockTemperatureCollector {
	return &MockTemperatureCollector{sim: sim}
}
func (c *MockTemperatureCollector) Name() string { return "temperature" }

func (c *MockTemperatureCollector) Collect() (Sample, error) {
	snap := c.sim.Now()
	return Sample{Timestamp: snap.Time, Kind: "temperature", Data: snap.Temperature}, nil
}

// --- Power ---

type MockPowerCollector struct{ sim *MockSim }

func NewMockPowerCollector(sim *MockSim) *MockPowerCollector { return &MockPowerCollector{sim: sim} }
func (c *MockPowerCollector) Name() string                   { return "power" }

func (c *MockPowerCollector) Collect() (Sample, error) {
	snap := c.sim.Now()
	return Sample{Timestamp: snap.Time, Kind: "power", Data: snap.Power}, nil
}

// --- GPU ---

type MockGPUCollector struct{ sim *MockSim }

func NewMockGPUCollector(sim *MockSim) *MockGPUCollector { return &MockGPUCollector{sim: sim} }
func (c *MockGPUCollector) Name() string                 { return "gpu" }
func (c *MockGPUCollector) Stop()                        {}

func (c *MockGPUCollector) Collect() (Sample, error) {
	snap := c.sim.Now()
	return Sample{Timestamp: snap.Time, Kind: "gpu", Data: snap.GPU}, nil
}

// --- Process ---

// MockProcessCollector reports the simulated process table: every process in
// the lightweight snapshot, and the top N by CPU plus pinned ones enriched.
type MockProcessCollector struct {
	sim           *MockSim
	maxProcs      int
	configPins    []string
	runtimePinsFn func() []string

	mu       sync.RWMutex
	allProcs []ProcessBasicInfo
}

var _ ProcessCollectorI = (*MockProcessCollector)(nil)

func NewMockProcessCollector(sim *MockSim, maxProcs int, configPins []string) *MockProcessCollector {
	return &MockProcessCollector{sim: sim, maxProcs: maxProcs, configPins: configPins}
}

func (c *MockProcessCollector) Name() string { return "process" }

func (c *MockProcessCollector) SetRuntimePinsFunc(fn func() []string) {
	c.runtimePinsFn = fn
}

func (c *MockProcessCollector) SetNetIOReader(NetIOReader) {} // no-op: mock has no eBPF backend

func (c *MockProcessCollector) AllProcessSnapshot() []ProcessBasicInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]ProcessBasicInfo, len(c.allProcs))
	copy(out, c.allProcs)
	return out
}

func (c *MockProcessCollector) Collect() (Sample, error) {
	pins := append([]string{}, c.configPins...)
	if c.runtimePinsFn != nil {
		pins = append(pins, c.runtimePinsFn()...)
	}
	snap := c.sim.Now()
	all, full := snap.Processes()
	data := MockProcessData(all, full, c.maxProcs, pins)

	c.mu.Lock()
	c.allProcs = all
	c.mu.Unlock()
	return Sample{Timestamp: snap.Time, Kind: "process", Data: data}, nil
}

// MockProcessData builds the collector's ProcessData from a snapshot's process
// table (busiest first): the top maxProcs plus any matching a pin pattern are
// enriched, in CPU order, as the real collector does. Shared with the history
// seeder.
func MockProcessData(all []ProcessBasicInfo, full []ProcessSample, maxProcs int, pins []string) ProcessData {
	data := ProcessData{TotalProcs: int32(len(all))}
	count := 0
	for i, p := range full {
		pinned := false
		for _, pattern := range pins {
			if matched, _ := filepath.Match(pattern, p.Name); matched {
				pinned = true
				break
			}
		}
		if pinned || count < maxProcs {
			data.Processes = append(data.Processes, p)
			if !pinned {
				count++
			}
		}
		b := all[i]
		data.TotalCPUPct += b.CPUPct
		data.TotalRSSBytes += b.RSSBytes
		if b.State == "R" {
			data.RunningProcs++
		}
		if b.CPUPct > 0.5 {
			data.ActiveProcs++
		}
	}
	return data
}
