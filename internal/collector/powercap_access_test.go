package collector

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestGrantPowercapRead(t *testing.T) {
	root := t.TempDir()
	mkDomain := func(dir string) string {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "energy_uj")
		if err := os.WriteFile(p, []byte("1000\n"), 0o400); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// The flat-symlink layout of /sys/class/powercap (see TestDiscoverZonesDedup).
	pkg := filepath.Join(root, "intel-rapl:0")
	counters := []string{
		mkDomain(pkg),
		mkDomain(filepath.Join(pkg, "intel-rapl:0:0")),
		mkDomain(filepath.Join(root, "intel-rapl-mmio:0")),
	}
	if err := os.Symlink(filepath.Join(pkg, "intel-rapl:0:0"), filepath.Join(root, "intel-rapl:0:0")); err != nil {
		t.Fatal(err)
	}
	// A zone whose energy_uj is a symlink to some other file must not be touched.
	other := filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(other, []byte("x"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "decoy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(root, "decoy", "energy_uj")); err != nil {
		t.Fatal(err)
	}

	old := powercapRoot
	powercapRoot = root
	defer func() { powercapRoot = old }()

	// Our own group: a non-root test can chgrp to it.
	gid := os.Getgid()
	n, err := GrantPowercapRead(gid)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(counters) {
		t.Errorf("changed %d counters, want %d (each counter once, despite the aliases)", n, len(counters))
	}
	for _, p := range counters {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o440 {
			t.Errorf("%s: mode %v, want 0440", p, info.Mode().Perm())
		}
		if g := int(info.Sys().(*syscall.Stat_t).Gid); g != gid {
			t.Errorf("%s: gid %d, want %d", p, g, gid)
		}
	}
	if info, _ := os.Stat(other); info.Mode().Perm() != 0o400 {
		t.Errorf("symlink target that isn't energy_uj was changed to %v", info.Mode().Perm())
	}
}
