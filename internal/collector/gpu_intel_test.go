package collector

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// intelFixture is a fake sysfs + /proc tree for the Intel backend.
type intelFixture struct {
	t    *testing.T
	root string
	card string // .../drm/card0
	proc string
}

func newIntelFixture(t *testing.T, driver string) *intelFixture {
	t.Helper()
	root := t.TempDir()
	f := &intelFixture{t: t, root: root, card: filepath.Join(root, "drm", "card0"), proc: filepath.Join(root, "proc")}

	pci := filepath.Join(root, "pci", "0000:00:02.0")
	f.mkdir(filepath.Join(root, "drivers", driver))
	f.mkdir(pci)
	f.mkdir(f.card)
	f.symlink(pci, filepath.Join(f.card, "device"))
	f.symlink(filepath.Join(root, "drivers", driver), filepath.Join(pci, "driver"))
	f.write(filepath.Join(pci, "vendor"), "0x8086")
	f.write(filepath.Join(pci, "device"), "0xa780")
	f.mkdir(filepath.Join(root, "drm", "card0-DP-1")) // a connector, not a card

	old := []*string{&intelDRMGlob, &intelProcDir, &powercapRoot}
	saved := []string{intelDRMGlob, intelProcDir, powercapRoot}
	intelDRMGlob = filepath.Join(root, "drm", "card[0-9]*")
	intelProcDir = f.proc
	powercapRoot = filepath.Join(root, "powercap")
	t.Cleanup(func() {
		for i, p := range old {
			*p = saved[i]
		}
	})
	return f
}

func (f *intelFixture) mkdir(p string) {
	if err := os.MkdirAll(p, 0o755); err != nil {
		f.t.Fatal(err)
	}
}

func (f *intelFixture) write(p, v string) {
	f.mkdir(filepath.Dir(p))
	if err := os.WriteFile(p, []byte(v+"\n"), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *intelFixture) symlink(target, link string) {
	f.mkdir(filepath.Dir(link))
	if err := os.Symlink(target, link); err != nil {
		f.t.Fatal(err)
	}
}

// openFD gives pid an open fd with the given fdinfo. (Discovery reads fdinfo
// only; the fd symlink target isn't needed.)
func (f *intelFixture) openFD(pid, fd int, fdinfo string) {
	f.write(filepath.Join(f.proc, fmt.Sprint(pid), "fdinfo", fmt.Sprint(fd)), fdinfo)
}

func i915Fdinfo(clientID, renderNS, videoNS uint64) string {
	return fmt.Sprintf("pos:\t0\nflags:\t02100002\ndrm-driver:\ti915\ndrm-pdev:\t0000:00:02.0\ndrm-client-id:\t%d\ndrm-engine-render:\t%d ns\ndrm-engine-video:\t%d ns\ndrm-engine-capacity-video:\t2", clientID, renderNS, videoNS)
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestIntelGPUBackendI915(t *testing.T) {
	f := newIntelFixture(t, "i915")
	f.write(filepath.Join(f.card, "gt_act_freq_mhz"), "900")
	f.write(filepath.Join(f.card, "gt_max_freq_mhz"), "1400")
	f.write(filepath.Join(f.card, "gt", "gt0", "rc6_residency_ms"), "10000")
	uncore := filepath.Join(f.root, "powercap", "intel-rapl:0", "intel-rapl:0:1")
	f.write(filepath.Join(uncore, "name"), "uncore")
	f.write(filepath.Join(uncore, "energy_uj"), "5000000")
	f.write(filepath.Join(uncore, "max_energy_range_uj"), "262143328850")

	// Client 7 is open in two processes (an inherited fd): counted once.
	f.openFD(100, 5, i915Fdinfo(7, 1_000_000_000, 0))
	f.openFD(101, 9, i915Fdinfo(7, 1_000_000_000, 0))
	f.openFD(102, 4, i915Fdinfo(8, 0, 0))
	f.openFD(103, 3, "drm-driver:\tamdgpu\ndrm-pdev:\t0000:03:00.0\ndrm-client-id:\t1\ndrm-engine-gfx:\t1 ns")
	f.openFD(104, 1, "pos:\t0\nflags:\t02\nmnt_id:\t25\nino:\t42")

	b := newIntelGPUBackend()
	if b == nil || len(b.cards) != 1 {
		t.Fatalf("backend = %+v, want one i915 card (connector skipped)", b)
	}
	clock := time.Unix(1_000_000, 0)
	b.now = func() time.Time { return clock }

	if devs, err := b.read(); err != nil || len(devs) != 0 {
		t.Fatalf("first read = %v, %v; want the baseline (no devices)", devs, err)
	}

	// 2 s later: client 7 rendered 1 s (50%), client 8 rendered 0.4 s (20%),
	// video used 1 s across two engines (25%); RC6 1.5 s (75%); 6 J (3 W).
	clock = clock.Add(2 * time.Second)
	f.write(filepath.Join(f.proc, "100", "fdinfo", "5"), i915Fdinfo(7, 2_000_000_000, 0))
	f.write(filepath.Join(f.proc, "101", "fdinfo", "9"), i915Fdinfo(7, 2_000_000_000, 0))
	f.write(filepath.Join(f.proc, "102", "fdinfo", "4"), i915Fdinfo(8, 400_000_000, 1_000_000_000))
	f.write(filepath.Join(f.card, "gt", "gt0", "rc6_residency_ms"), "11500")
	f.write(filepath.Join(uncore, "energy_uj"), "11000000")

	devs, err := b.read()
	if err != nil || len(devs) != 1 {
		t.Fatalf("read = %v, %v", devs, err)
	}
	d := devs[0]
	if !near(d.UtilizationPct, 70) {
		t.Errorf("utilization = %.2f%%, want 70%% (render: 50%% + 20%%, busier than video's 25%%)", d.UtilizationPct)
	}
	if !near(d.ThrottlePct, 75) {
		t.Errorf("RC6 = %.2f%%, want 75%%", d.ThrottlePct)
	}
	if !near(d.PowerWatts, 3) {
		t.Errorf("power = %.2f W, want 3 W", d.PowerWatts)
	}
	if d.FrequencyMHz != 900 || d.FrequencyMaxMHz != 1400 || d.Vendor != "intel" || d.Name != "Intel GPU 0xa780" {
		t.Errorf("device = %+v", d)
	}

	// A client that has gone (fdinfo removed) is dropped, not an error.
	os.RemoveAll(filepath.Join(f.proc, "102"))
	clock = clock.Add(2 * time.Second)
	if _, err := b.read(); err != nil {
		t.Fatal(err)
	}
	if len(b.clients) != 2 {
		t.Errorf("known clients = %d, want 2 (pids 100, 101) after pid 102 exited; the amdgpu and non-DRM fds are never tracked", len(b.clients))
	}
}

func TestIntelGPUBackendXe(t *testing.T) {
	f := newIntelFixture(t, "xe")
	gt := filepath.Join(f.card, "device", "tile0", "gt0")
	f.write(filepath.Join(gt, "freq0", "act_freq"), "2000")
	f.write(filepath.Join(gt, "freq0", "max_freq"), "2400")
	f.write(filepath.Join(gt, "gtidle", "idle_residency_ms"), "0")
	xe := func(cycles, total uint64) string {
		return fmt.Sprintf("drm-driver:\txe\ndrm-pdev:\t0000:00:02.0\ndrm-client-id:\t3\ndrm-cycles-rcs:\t%d\ndrm-total-cycles-rcs:\t%d", cycles, total)
	}
	f.openFD(200, 6, xe(0, 1000))

	b := newIntelGPUBackend()
	clock := time.Unix(1_000_000, 0)
	b.now = func() time.Time { return clock }
	b.read()

	clock = clock.Add(time.Second)
	f.write(filepath.Join(f.proc, "200", "fdinfo", "6"), xe(600, 2000))
	devs, err := b.read()
	if err != nil || len(devs) != 1 {
		t.Fatalf("read = %v, %v", devs, err)
	}
	if !near(devs[0].UtilizationPct, 60) || devs[0].FrequencyMHz != 2000 || devs[0].FrequencyMaxMHz != 2400 {
		t.Errorf("device = %+v, want 60%% busy (600 of 1000 cycles) at 2000/2400 MHz", devs[0])
	}
}

func TestIntelGPUBackendNone(t *testing.T) {
	f := newIntelFixture(t, "amdgpu")
	_ = f
	if b := newIntelGPUBackend(); b != nil {
		t.Errorf("backend for a non-Intel card: %+v", b)
	}
}
