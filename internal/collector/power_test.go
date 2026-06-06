package collector

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestDiscoverZonesDedup builds a fake /sys/class/powercap tree that reproduces
// the flat-symlink layout: every RAPL sub-domain appears both as a bare
// top-level entry (symlink) and nested under its package. discoverZones must
// collapse those aliases (keeping the hierarchical name) while preserving
// genuinely-distinct domains (the separate intel-rapl-mmio counter).
func TestDiscoverZonesDedup(t *testing.T) {
	root := t.TempDir()
	mkDomain := func(dir, name string) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, "name"), []byte(name+"\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "energy_uj"), []byte("1000\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "max_energy_range_uj"), []byte("262143328850\n"), 0o644)
	}

	// Top-level package with nested sub-domains (the real dirs).
	pkg := filepath.Join(root, "intel-rapl:0")
	mkDomain(pkg, "package-0")
	mkDomain(filepath.Join(pkg, "intel-rapl:0:0"), "core")
	mkDomain(filepath.Join(pkg, "intel-rapl:0:1"), "uncore")
	// Flat symlinks, as /sys/class/powercap exposes every domain at top level —
	// these are the bare aliases that caused the double-counting.
	if err := os.Symlink(filepath.Join(pkg, "intel-rapl:0:0"), filepath.Join(root, "intel-rapl:0:0")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(pkg, "intel-rapl:0:1"), filepath.Join(root, "intel-rapl:0:1")); err != nil {
		t.Fatal(err)
	}
	// A separate control type (mmio) with a same-named but physically distinct counter.
	mkDomain(filepath.Join(root, "intel-rapl-mmio:0"), "package-0")

	old := powercapRoot
	powercapRoot = root
	defer func() { powercapRoot = old }()

	c := &PowerCollector{}
	c.discoverZones()

	var names []string
	for _, z := range c.zones {
		names = append(names, z.name)
	}
	sort.Strings(names)
	got := strings.Join(names, ",")
	want := "mmio/package-0,package-0,package-0/core,package-0/uncore"
	if got != want {
		t.Errorf("zones = %q, want %q (bare core/uncore aliases must be deduped; mmio kept distinct)", got, want)
	}
}
