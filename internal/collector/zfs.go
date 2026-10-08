package collector

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ZFS support for the disk collector. A ZFS dataset is mounted from its name
// (rpool/ROOT/pve-1), not from a /dev node, so /proc/diskstats has no entry for
// it and its physical disks can't be found by resolving the mount source.
// Instead, I/O comes from OpenZFS's per-dataset kstats, and pool members are
// found through udev's filesystem probe (each member partition is labelled
// zfs_member with the pool name). Both are world-readable, so the unprivileged
// daemon needs nothing extra, and nothing here execs zpool/zfs.

// Package vars so tests can point them at fixture trees.
var (
	zfsKstatRoot  = "/proc/spl/kstat/zfs"
	sysClassBlock = "/sys/class/block"
	udevDataDir   = "/run/udev/data"
)

// zfsIO holds a dataset's cumulative I/O counters (objset kstat).
type zfsIO struct {
	reads, writes, nread, nwritten uint64
}

// zfsPool returns the pool a dataset belongs to ("rpool/ROOT/pve-1" → "rpool").
func zfsPool(dataset string) string {
	pool, _, _ := strings.Cut(dataset, "/")
	return pool
}

// readZFSObjsetIO returns cumulative I/O counters keyed by dataset name for the
// given pools, from /proc/spl/kstat/zfs/<pool>/objset-0x<id>.
func readZFSObjsetIO(pools map[string]bool) map[string]zfsIO {
	out := make(map[string]zfsIO)
	for pool := range pools {
		files, _ := filepath.Glob(filepath.Join(zfsKstatRoot, pool, "objset-*"))
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			if name, io, ok := parseObjsetKstat(string(data)); ok {
				out[name] = io
			}
		}
	}
	return out
}

// parseObjsetKstat parses an objset kstat file:
//
//	36 1 0x01 7 2160 5214237902 1004461298394
//	name                            type data
//	dataset_name                    7    rpool/ROOT/pve-1
//	writes                          4    26883
//	nwritten                        4    478516413
//	reads                           4    101264
//	nread                           4    1938466432
func parseObjsetKstat(data string) (string, zfsIO, bool) {
	var name string
	var io zfsIO
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		v, _ := strconv.ParseUint(f[2], 10, 64)
		switch f[0] {
		case "dataset_name":
			name = f[2]
		case "reads":
			io.reads = v
		case "writes":
			io.writes = v
		case "nread":
			io.nread = v
		case "nwritten":
			io.nwritten = v
		}
	}
	return name, io, name != ""
}

// zfsPoolDevices maps each imported pool's name to its member disks (physical
// devices, sorted), using the zfs_member label udev records for each partition.
func zfsPoolDevices() map[string][]string {
	entries, err := os.ReadDir(sysClassBlock)
	if err != nil {
		return nil
	}
	seen := make(map[string]map[string]bool)
	for _, e := range entries {
		devNum, err := os.ReadFile(filepath.Join(sysClassBlock, e.Name(), "dev"))
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(udevDataDir, "b"+strings.TrimSpace(string(devNum))))
		if err != nil {
			continue
		}
		fsType, label := parseUdevFS(string(data))
		if fsType != "zfs_member" || label == "" {
			continue
		}
		if seen[label] == nil {
			seen[label] = make(map[string]bool)
		}
		seen[label][physicalDevice("/dev/"+e.Name())] = true
	}
	out := make(map[string][]string, len(seen))
	for pool, devs := range seen {
		for d := range devs {
			out[pool] = append(out[pool], d)
		}
		sort.Strings(out[pool])
	}
	return out
}

// parseUdevFS returns ID_FS_TYPE and ID_FS_LABEL from a udev database entry
// (/run/udev/data/b<major>:<minor>, lines like "E:ID_FS_TYPE=zfs_member").
func parseUdevFS(data string) (fsType, label string) {
	for _, line := range strings.Split(data, "\n") {
		if v, ok := strings.CutPrefix(line, "E:ID_FS_TYPE="); ok {
			fsType = v
		} else if v, ok := strings.CutPrefix(line, "E:ID_FS_LABEL="); ok {
			label = v
		}
	}
	return fsType, label
}

// zfsMountSMART picks the SMART snapshot to show on a ZFS mount whose pool spans
// several disks: a failing disk first, so a dying mirror member isn't hidden
// behind a healthy one, otherwise the first available. Every member is still
// persisted and alertable individually via DiskData.SMART.
func zfsMountSMART(members []string, cache map[string]*SMARTInfo) *SMARTInfo {
	var first *SMARTInfo
	for _, d := range members {
		si, ok := cache[d]
		if !ok || si == nil || !si.Available {
			continue
		}
		if !si.Healthy {
			return si
		}
		if first == nil {
			first = si
		}
	}
	return first
}
