package collector

import "time"

// GPUDeviceSample holds metrics for a single GPU device.
type GPUDeviceSample struct {
	Name             string
	Index            int
	Vendor           string  // "intel", "nvidia"
	UtilizationPct   float64 // GPU compute utilization %
	MemoryUsedBytes  uint64  // 0 for Intel iGPU (shared system memory)
	MemoryTotalBytes uint64  // 0 for Intel iGPU
	TempCelsius      float64 // 0 if not available
	PowerWatts       float64
	FrequencyMHz     uint32  // actual GPU clock
	FrequencyMaxMHz  uint32  // max GPU clock
	ThrottlePct      float64 // RC6 residency for Intel; 0 for NVIDIA
}

// GPUData is a batch of GPU metrics from one collection cycle.
type GPUData struct {
	GPUs []GPUDeviceSample
}

// gpuBackend reads GPU metrics from a specific vendor's tooling.
type gpuBackend interface {
	read() ([]GPUDeviceSample, error)
	stop()
}

// GPUCollector gathers metrics from all detected GPU backends.
type GPUCollector struct {
	backends []gpuBackend
}

// NewGPUCollector detects available GPU backends and returns a collector.
func NewGPUCollector() *GPUCollector {
	c := &GPUCollector{}

	if b := newIntelGPUBackend(); b != nil {
		c.backends = append(c.backends, b)
	}
	if b := newNvidiaGPUBackend(); b != nil {
		c.backends = append(c.backends, b)
	}

	return c
}

func (c *GPUCollector) Name() string { return "gpu" }

func (c *GPUCollector) Collect() (Sample, error) {
	var gpus []GPUDeviceSample
	for _, b := range c.backends {
		devs, err := b.read()
		if err != nil {
			continue
		}
		gpus = append(gpus, devs...)
	}

	return Sample{
		Timestamp: time.Now(),
		Kind:      "gpu",
		Data:      GPUData{GPUs: gpus},
	}, nil
}

// Stop shuts down long-lived backend processes (e.g. intel_gpu_top).
func (c *GPUCollector) Stop() {
	for _, b := range c.backends {
		b.stop()
	}
}
