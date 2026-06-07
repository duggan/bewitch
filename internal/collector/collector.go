package collector

import "time"

// Sample is one batch of metric readings from a collector.
type Sample struct {
	Timestamp time.Time
	Kind      string // "cpu", "memory", "disk", "network", "ecc", "temperature", "power", "gpu", "process"
	Data      any
}

// Collector gathers one category of system metrics.
type Collector interface {
	Name() string
	Collect() (Sample, error)
}

// ProcessCollectorI extends Collector with process-specific methods needed by the daemon.
type ProcessCollectorI interface {
	Collector
	AllProcessSnapshot() []ProcessBasicInfo
	SetRuntimePinsFunc(fn func() []string)
	SetNetIOReader(r NetIOReader)
}

// NetIOCounters holds cumulative per-process network bytes (rx/tx) observed since
// the reader started. The process collector deltas them into bytes/sec rates.
type NetIOCounters struct {
	RxBytes uint64
	TxBytes uint64
}

// NetIOReader supplies per-PID cumulative network byte counters. Implemented by the
// eBPF backend on Linux (NewNetIOReader); nil elsewhere or when eBPF can't load, in
// which case per-process network rates are simply absent (graceful no-op). Snapshot
// is read once per collection cycle and must be safe to call concurrently with the
// reader's own kernel-map updates.
type NetIOReader interface {
	Snapshot() map[int32]NetIOCounters
	Close() error
}

// CPU data types

type CPUCoreSample struct {
	Core      int // -1 for aggregate
	UserPct   float64
	SystemPct float64
	IdlePct   float64
	IOWaitPct float64
	StealPct  float64 // hypervisor-stolen time (VPS noisy-neighbour contention); live-only
}

type CPUData struct {
	Cores []CPUCoreSample
}
