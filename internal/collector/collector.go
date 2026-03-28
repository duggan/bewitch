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
}

// CPU data types

type CPUCoreSample struct {
	Core      int // -1 for aggregate
	UserPct   float64
	SystemPct float64
	IdlePct   float64
	IOWaitPct float64
}

type CPUData struct {
	Cores []CPUCoreSample
}
