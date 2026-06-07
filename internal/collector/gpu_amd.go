package collector

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// amdDRMGlob locates AMD GPUs via their amdgpu-specific utilization file. Globbing
// on gpu_busy_percent naturally excludes DRM connectors (card0-DP-1 etc.) and
// non-amdgpu cards, which don't have it. Package var so the fixture test can point
// discovery at a temp sysfs tree.
var amdDRMGlob = "/sys/class/drm/card*/device/gpu_busy_percent"

type amdCard struct {
	devPath   string // /sys/class/drm/cardN/device
	hwmonPath string // .../hwmon/hwmonX, or "" if absent
	name      string
	index     int
}

// amdGPUBackend reads AMD GPU metrics straight from amdgpu sysfs — no external tool
// (unlike the Intel intel_gpu_top / NVIDIA nvidia-smi backends), so it's the only
// zero-dependency GPU backend. Stateless: nothing to stop.
type amdGPUBackend struct {
	cards []amdCard
}

// newAmdGPUBackend returns a backend for each amdgpu card found, or nil if none.
func newAmdGPUBackend() *amdGPUBackend {
	matches, _ := filepath.Glob(amdDRMGlob)
	var cards []amdCard
	for i, busyFile := range matches {
		dev := filepath.Dir(busyFile)
		// Defensive vendor check (0x1002 = AMD/ATI); gpu_busy_percent already implies amdgpu.
		if v := readString(filepath.Join(dev, "vendor")); v != "" && v != "0x1002" {
			continue
		}
		cards = append(cards, amdCard{
			devPath:   dev,
			hwmonPath: firstAmdHwmon(dev),
			name:      amdCardName(dev),
			index:     i,
		})
	}
	if len(cards) == 0 {
		return nil
	}
	return &amdGPUBackend{cards: cards}
}

func (b *amdGPUBackend) read() ([]GPUDeviceSample, error) {
	out := make([]GPUDeviceSample, 0, len(b.cards))
	for _, c := range b.cards {
		s := GPUDeviceSample{Name: c.name, Index: c.index, Vendor: "amd"}

		if v, err := strconv.ParseFloat(readString(filepath.Join(c.devPath, "gpu_busy_percent")), 64); err == nil {
			s.UtilizationPct = v
		}
		if v, err := strconv.ParseUint(readString(filepath.Join(c.devPath, "mem_info_vram_used")), 10, 64); err == nil {
			s.MemoryUsedBytes = v
		}
		if v, err := strconv.ParseUint(readString(filepath.Join(c.devPath, "mem_info_vram_total")), 10, 64); err == nil {
			s.MemoryTotalBytes = v
		}
		if c.hwmonPath != "" {
			// temp1_input is millidegrees C; power1_average is microwatts. Both optional.
			if v, err := strconv.ParseInt(readString(filepath.Join(c.hwmonPath, "temp1_input")), 10, 64); err == nil {
				s.TempCelsius = float64(v) / 1000.0
			}
			if v, err := strconv.ParseInt(readString(filepath.Join(c.hwmonPath, "power1_average")), 10, 64); err == nil {
				s.PowerWatts = float64(v) / 1e6
			}
		}
		s.FrequencyMHz = amdCurrentSclkMHz(filepath.Join(c.devPath, "pp_dpm_sclk"))

		out = append(out, s)
	}
	return out, nil
}

func (b *amdGPUBackend) stop() {}

// firstAmdHwmon returns the card's first hwmon directory (where temp/power live), or "".
func firstAmdHwmon(dev string) string {
	matches, _ := filepath.Glob(filepath.Join(dev, "hwmon", "hwmon*"))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

// amdCardName uses the kernel-provided product_name when present (newer amdgpu),
// else falls back to the cardN identifier so multiple cards stay distinguishable.
func amdCardName(dev string) string {
	if n := readString(filepath.Join(dev, "product_name")); n != "" {
		return n
	}
	return "AMD GPU (" + filepath.Base(filepath.Dir(dev)) + ")"
}

// amdCurrentSclkMHz parses pp_dpm_sclk (lines like "1: 1000Mhz *") and returns the
// MHz of the active state (the one marked with '*'), or 0 if unavailable.
func amdCurrentSclkMHz(path string) uint32 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "*") {
			continue
		}
		for _, f := range strings.Fields(line) {
			low := strings.ToLower(f)
			if strings.HasSuffix(low, "mhz") {
				if v, err := strconv.ParseUint(strings.TrimSuffix(low, "mhz"), 10, 32); err == nil {
					return uint32(v)
				}
			}
		}
	}
	return 0
}
