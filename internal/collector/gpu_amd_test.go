package collector

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAmdGPUBackend builds a fake amdgpu sysfs tree and verifies the backend
// discovers the card and parses utilization, VRAM, temperature (millidegrees →
// °C), power (µW → W), and the active sclk state from pp_dpm_sclk.
func TestAmdGPUBackend(t *testing.T) {
	root := t.TempDir()
	dev := filepath.Join(root, "card0", "device")
	hwmon := filepath.Join(dev, "hwmon", "hwmon3")
	if err := os.MkdirAll(hwmon, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p, v string) {
		if err := os.WriteFile(p, []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dev, "vendor"), "0x1002\n")
	write(filepath.Join(dev, "gpu_busy_percent"), "42\n")
	write(filepath.Join(dev, "mem_info_vram_used"), "1073741824\n")
	write(filepath.Join(dev, "mem_info_vram_total"), "8589934592\n")
	write(filepath.Join(dev, "product_name"), "Radeon RX 7900\n")
	write(filepath.Join(dev, "pp_dpm_sclk"), "0: 300Mhz\n1: 1500Mhz *\n2: 2000Mhz\n")
	write(filepath.Join(hwmon, "temp1_input"), "55000\n")        // 55 °C
	write(filepath.Join(hwmon, "power1_average"), "120000000\n") // 120 W

	old := amdDRMGlob
	amdDRMGlob = filepath.Join(root, "card*", "device", "gpu_busy_percent")
	defer func() { amdDRMGlob = old }()

	b := newAmdGPUBackend()
	if b == nil {
		t.Fatal("newAmdGPUBackend() = nil, want a backend for the fixture card")
	}
	devs, err := b.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(devs) != 1 {
		t.Fatalf("got %d devices, want 1", len(devs))
	}
	g := devs[0]
	if g.Vendor != "amd" {
		t.Errorf("Vendor = %q, want amd", g.Vendor)
	}
	if g.Name != "Radeon RX 7900" {
		t.Errorf("Name = %q, want product_name", g.Name)
	}
	if g.UtilizationPct != 42 {
		t.Errorf("UtilizationPct = %v, want 42", g.UtilizationPct)
	}
	if g.MemoryUsedBytes != 1073741824 || g.MemoryTotalBytes != 8589934592 {
		t.Errorf("VRAM = %d/%d, want 1073741824/8589934592", g.MemoryUsedBytes, g.MemoryTotalBytes)
	}
	if g.TempCelsius != 55 {
		t.Errorf("TempCelsius = %v, want 55", g.TempCelsius)
	}
	if g.PowerWatts != 120 {
		t.Errorf("PowerWatts = %v, want 120", g.PowerWatts)
	}
	if g.FrequencyMHz != 1500 {
		t.Errorf("FrequencyMHz = %d, want 1500 (the *-marked state)", g.FrequencyMHz)
	}
}

// TestAmdGPUBackendNameFallback verifies the cardN fallback when product_name is absent.
func TestAmdGPUBackendNameFallback(t *testing.T) {
	root := t.TempDir()
	dev := filepath.Join(root, "card1", "device")
	if err := os.MkdirAll(dev, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dev, "vendor"), []byte("0x1002\n"), 0o644)
	os.WriteFile(filepath.Join(dev, "gpu_busy_percent"), []byte("0\n"), 0o644)

	old := amdDRMGlob
	amdDRMGlob = filepath.Join(root, "card*", "device", "gpu_busy_percent")
	defer func() { amdDRMGlob = old }()

	b := newAmdGPUBackend()
	if b == nil {
		t.Fatal("expected a backend")
	}
	devs, _ := b.read()
	if len(devs) != 1 || devs[0].Name != "AMD GPU (card1)" {
		t.Errorf("name fallback = %q, want \"AMD GPU (card1)\"", devs[0].Name)
	}
}

// TestAmdGPUBackendNone returns nil when no amdgpu cards are present.
func TestAmdGPUBackendNone(t *testing.T) {
	old := amdDRMGlob
	amdDRMGlob = filepath.Join(t.TempDir(), "card*", "device", "gpu_busy_percent")
	defer func() { amdDRMGlob = old }()
	if b := newAmdGPUBackend(); b != nil {
		t.Error("newAmdGPUBackend() should be nil with no amdgpu cards")
	}
}
