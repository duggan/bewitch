package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIoDeviceNameResolvesSymlinks(t *testing.T) {
	// /dev/mapper/pve-root → /dev/dm-1: diskstats only knows "dm-1".
	dir := t.TempDir()
	target := filepath.Join(dir, "dm-1")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "pve-root")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if got := ioDeviceName(link); got != "dm-1" {
		t.Errorf("ioDeviceName(symlink) = %q, want dm-1", got)
	}
	// Unresolvable paths fall back to the plain base name.
	if got := ioDeviceName("/dev/does-not-exist-sda1"); got != "does-not-exist-sda1" {
		t.Errorf("ioDeviceName(missing) = %q", got)
	}
}

func TestIsBlockDevice(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{f, "/dev/null", "/dev/does-not-exist"} {
		if isBlockDevice(p) {
			t.Errorf("isBlockDevice(%q) = true, want false", p)
		}
	}
}

func TestIsNVMeDevice(t *testing.T) {
	cases := map[string]bool{
		"/dev/nvme0n1":   true,
		"/dev/nvme12n3":  true,
		"/dev/nvme0n1p1": false,
		"/dev/nvme0":     false,
		"/dev/sda":       false,
	}
	for dev, want := range cases {
		if got := isNVMeDevice(dev); got != want {
			t.Errorf("isNVMeDevice(%q) = %v, want %v", dev, got, want)
		}
	}
}

func TestSMARTSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	info := SMARTInfo{Available: true, Healthy: true, Temperature: 53, AvailableSpare: 100, PercentUsed: 10, PowerOnHours: 1442}
	write := func(s smartSnapshot) {
		t.Helper()
		if err := writeFileAtomic(filepath.Join(dir, "nvme0n1.json"), s); err != nil {
			t.Fatal(err)
		}
	}
	good := smartSnapshot{Version: smartSnapshotVersion, Device: "/dev/nvme0n1", ReadAt: now.Add(-time.Minute), Info: info}

	write(good)
	got := loadSMARTSnapshot(dir, "/dev/nvme0n1", 15*time.Minute, now)
	if got == nil || *got != info {
		t.Fatalf("fresh snapshot: got %+v, want %+v", got, info)
	}

	reject := map[string]func(s *smartSnapshot){
		"stale":           func(s *smartSnapshot) { s.ReadAt = now.Add(-time.Hour) },
		"future":          func(s *smartSnapshot) { s.ReadAt = now.Add(time.Hour) },
		"device mismatch": func(s *smartSnapshot) { s.Device = "/dev/nvme1n1" },
		"wrong version":   func(s *smartSnapshot) { s.Version = 99 },
		"unavailable":     func(s *smartSnapshot) { s.Info.Available = false },
	}
	for name, mutate := range reject {
		s := good
		mutate(&s)
		write(s)
		if got := loadSMARTSnapshot(dir, "/dev/nvme0n1", 15*time.Minute, now); got != nil {
			t.Errorf("%s: expected snapshot to be ignored, got %+v", name, got)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "nvme0n1.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadSMARTSnapshot(dir, "/dev/nvme0n1", 15*time.Minute, now); got != nil {
		t.Errorf("malformed: expected nil, got %+v", got)
	}
	if got := loadSMARTSnapshot(dir, "/dev/nvme9n1", 15*time.Minute, now); got != nil {
		t.Errorf("missing file: expected nil, got %+v", got)
	}
}

func TestReadHelperSMARTGating(t *testing.T) {
	dir := t.TempDir()
	snap := smartSnapshot{Version: smartSnapshotVersion, Device: "/dev/nvme0n1", ReadAt: time.Now(), Info: SMARTInfo{Available: true, Healthy: true}}
	if err := writeFileAtomic(filepath.Join(dir, "nvme0n1.json"), snap); err != nil {
		t.Fatal(err)
	}
	c := &DiskCollector{smartInterval: 5 * time.Minute}
	if c.readHelperSMART("/dev/nvme0n1") != nil {
		t.Error("helper disabled: expected nil")
	}
	c.SetSMARTHelperDir(dir)
	if c.readHelperSMART("/dev/nvme0n1") == nil {
		t.Error("helper enabled: expected snapshot")
	}
	if c.readHelperSMART("/dev/sda") != nil {
		t.Error("non-NVMe device: expected nil")
	}
}

func TestListNVMeDevices(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"nvme0n1", "nvme1n1", "sda", "dm-0", "loop0"} {
		if err := os.Mkdir(filepath.Join(dir, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := sysBlockDir
	sysBlockDir = dir
	t.Cleanup(func() { sysBlockDir = old })

	devs, err := listNVMeDevices()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(devs, ","); got != "/dev/nvme0n1,/dev/nvme1n1" {
		t.Errorf("listNVMeDevices = %s", got)
	}
}

// fakeSmartctl writes a script that prints the given JSON, standing in for smartctl.
func fakeSmartctl(t *testing.T, json string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "smartctl")
	script := "#!/bin/sh\ncat <<'JSON'\n" + json + "\nJSON\nexit 4\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunSmartctlNVMeWithoutHealthLog(t *testing.T) {
	// Unprivileged smartctl on a 6.x kernel: device identified as NVMe, but the
	// health log read is denied. Must be an error, not a "healthy" all-zero reading.
	path := fakeSmartctl(t, `{"smartctl":{"exit_status":4,"messages":[{"string":"Read NVMe SMART/Health Information failed: Permission denied","severity":"error"}]},"device":{"type":"nvme"}}`)
	info, err := runSmartctl(path, "/dev/nvme0n1", "")
	if err == nil {
		t.Fatalf("expected error, got %+v", info)
	}
	if !strings.Contains(err.Error(), "Permission denied") {
		t.Errorf("error should carry smartctl's message, got %v", err)
	}

	path = fakeSmartctl(t, `{"smartctl":{"exit_status":4},"device":{"type":"nvme"},"nvme_smart_health_information_log":{"temperature":53,"available_spare":100,"percentage_used":10,"power_on_hours":1442}}`)
	info, err = runSmartctl(path, "/dev/nvme0n1", "")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.Temperature != 53 || info.PercentUsed != 10 || info.PowerOnHours != 1442 {
		t.Errorf("unexpected info %+v", info)
	}
}

// TestHasSMARTData: an all-zero fallback reading (USB flash stick behind an
// unknown bridge) must not count as SMART data, so it shows as unavailable
// rather than "healthy". Any single real reading is enough.
func TestHasSMARTData(t *testing.T) {
	if hasSMARTData(nil) {
		t.Error("nil reading counted as data")
	}
	if hasSMARTData(&SMARTInfo{Available: true, Healthy: true}) {
		t.Error("empty Available/Healthy reading counted as data")
	}
	for name, info := range map[string]SMARTInfo{
		"power-on hours": {PowerOnHours: 1},
		"power cycles":   {PowerCycles: 3},
		"temperature":    {Temperature: 41},
		"reallocated":    {ReallocatedSectors: 2},
		"nvme wear":      {PercentUsed: 4},
	} {
		if !hasSMARTData(&info) {
			t.Errorf("%s alone should count as SMART data", name)
		}
	}
}
