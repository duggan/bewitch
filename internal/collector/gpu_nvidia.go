package collector

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type nvidiaGPUBackend struct {
	toolPath string
}

func newNvidiaGPUBackend() *nvidiaGPUBackend {
	toolPath, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return nil
	}
	return &nvidiaGPUBackend{toolPath: toolPath}
}

func (b *nvidiaGPUBackend) read() ([]GPUDeviceSample, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, b.toolPath,
		"--query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,clocks.gr",
		"--format=csv,noheader,nounits",
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("nvidia-smi: %w", err)
	}

	return parseNvidiaSmiOutput(string(out))
}

func (b *nvidiaGPUBackend) stop() {}

func parseNvidiaSmiOutput(output string) ([]GPUDeviceSample, error) {
	var gpus []GPUDeviceSample

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Split(line, ", ")
		if len(fields) < 8 {
			continue
		}

		index, _ := strconv.Atoi(strings.TrimSpace(fields[0]))
		name := strings.TrimSpace(fields[1])
		utilization := parseFloatField(fields[2])
		memUsedMiB := parseFloatField(fields[3])
		memTotalMiB := parseFloatField(fields[4])
		temp := parseFloatField(fields[5])
		power := parseFloatField(fields[6])
		clockMHz := parseFloatField(fields[7])

		gpus = append(gpus, GPUDeviceSample{
			Name:             name,
			Index:            index,
			Vendor:           "nvidia",
			UtilizationPct:   utilization,
			MemoryUsedBytes:  uint64(memUsedMiB * 1024 * 1024),
			MemoryTotalBytes: uint64(memTotalMiB * 1024 * 1024),
			TempCelsius:      temp,
			PowerWatts:       power,
			FrequencyMHz:     uint32(clockMHz),
		})
	}

	if len(gpus) == 0 {
		return nil, fmt.Errorf("no GPUs parsed from nvidia-smi output")
	}
	return gpus, nil
}

// detectNvidiaGPU checks for NVIDIA GPU hardware via DRM sysfs vendor IDs.
func detectNvidiaGPU() bool {
	cards, _ := filepath.Glob("/sys/class/drm/card[0-9]*/device/vendor")
	for _, vendorFile := range cards {
		if readString(vendorFile) == "0x10de" {
			return true
		}
	}
	return false
}

func parseFloatField(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "[N/A]" || s == "N/A" || s == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
