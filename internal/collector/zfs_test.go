package collector

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseObjsetKstat(t *testing.T) {
	data := `36 1 0x01 7 2160 5214237902 1004461298394
name                            type data
dataset_name                    7    rpool/ROOT/pve-1
writes                          4    26883
nwritten                        4    478516413
reads                           4    101264
nread                           4    1938466432
nunlinks                        4    1200
nunlinked                       4    1200
`
	name, io, ok := parseObjsetKstat(data)
	if !ok || name != "rpool/ROOT/pve-1" {
		t.Fatalf("name = %q, ok = %v", name, ok)
	}
	want := zfsIO{reads: 101264, writes: 26883, nread: 1938466432, nwritten: 478516413}
	if io != want {
		t.Errorf("io = %+v, want %+v", io, want)
	}
	if _, _, ok := parseObjsetKstat("name type data\n"); ok {
		t.Error("kstat without dataset_name should not parse")
	}
}

func TestReadZFSObjsetIO(t *testing.T) {
	root := t.TempDir()
	write := func(rel, data string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("rpool/objset-0x36", "dataset_name 7 rpool/ROOT/pve-1\nnread 4 100\nnwritten 4 200\n")
	write("rpool/objset-0x101", "dataset_name 7 rpool/data\nnread 4 5\n")
	write("tank/objset-0x40", "dataset_name 7 tank\nnread 4 9\n")
	old := zfsKstatRoot
	zfsKstatRoot = root
	t.Cleanup(func() { zfsKstatRoot = old })

	got := readZFSObjsetIO(map[string]bool{"rpool": true})
	if len(got) != 2 || got["rpool/ROOT/pve-1"].nwritten != 200 || got["rpool/data"].nread != 5 {
		t.Errorf("got %+v", got)
	}
	if _, ok := got["tank"]; ok {
		t.Error("read a pool that wasn't asked for")
	}
}

func TestZFSPoolDevices(t *testing.T) {
	sys, udev := t.TempDir(), t.TempDir()
	dev := func(name, majmin, udevData string) {
		if err := os.MkdirAll(filepath.Join(sys, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sys, name, "dev"), []byte(majmin+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if udevData != "" {
			if err := os.WriteFile(filepath.Join(udev, "b"+majmin), []byte(udevData), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A two-disk mirror (Proxmox installer layout: ZFS on partition 3), a
	// whole-disk pool (ZFS on partition 1, plus the reserved partition 9 that
	// isn't a zfs_member), an ext4 disk, and a device udev knows nothing about.
	dev("sda", "8:0", "E:ID_TYPE=disk\n")
	dev("sda3", "8:3", "E:ID_FS_TYPE=zfs_member\nE:ID_FS_LABEL=rpool\nE:ID_FS_LABEL_ENC=rpool\n")
	dev("sdb3", "8:19", "E:ID_FS_TYPE=zfs_member\nE:ID_FS_LABEL=rpool\n")
	dev("nvme0n1p1", "259:1", "E:ID_FS_TYPE=zfs_member\nE:ID_FS_LABEL=tank\n")
	dev("nvme0n1p9", "259:9", "E:ID_PART_ENTRY_NUMBER=9\n")
	dev("sdc1", "8:33", "E:ID_FS_TYPE=ext4\nE:ID_FS_LABEL=backup\n")
	dev("loop0", "7:0", "")

	oldSys, oldUdev := sysClassBlock, udevDataDir
	sysClassBlock, udevDataDir = sys, udev
	t.Cleanup(func() { sysClassBlock, udevDataDir = oldSys, oldUdev })

	want := map[string][]string{
		"rpool": {"/dev/sda", "/dev/sdb"},
		"tank":  {"/dev/nvme0n1"},
	}
	if got := zfsPoolDevices(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestZFSMountSMART(t *testing.T) {
	healthy := &SMARTInfo{Available: true, Healthy: true, Temperature: 30}
	failing := &SMARTInfo{Available: true, Healthy: false, ReallocatedSectors: 8}
	cache := map[string]*SMARTInfo{
		"/dev/sda": healthy,
		"/dev/sdb": failing,
		"/dev/sdc": {Available: false},
	}
	if got := zfsMountSMART([]string{"/dev/sda", "/dev/sdb"}, cache); got != failing {
		t.Errorf("mirror with a failing member: got %+v, want the failing disk", got)
	}
	if got := zfsMountSMART([]string{"/dev/sdc", "/dev/sda"}, cache); got != healthy {
		t.Errorf("got %+v, want the first available disk", got)
	}
	if got := zfsMountSMART([]string{"/dev/sdc"}, cache); got != nil {
		t.Errorf("no available member: got %+v, want nil", got)
	}
	if got := zfsMountSMART(nil, cache); got != nil {
		t.Errorf("no members: got %+v, want nil", got)
	}
}

func TestReadARCStats(t *testing.T) {
	f := filepath.Join(t.TempDir(), "arcstats")
	data := `13 1 0x01 147 39984 5146838493 1004613785822
name                            type data
hits                            4    1234
c_min                           4    262144000
c_max                           4    4194304000
size                            4    3221225472
`
	if err := os.WriteFile(f, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	old := arcstatsPath
	arcstatsPath = f
	t.Cleanup(func() { arcstatsPath = old })

	size, cMin, ok := readARCStats()
	if !ok || size != 3221225472 || cMin != 262144000 {
		t.Errorf("got size=%d c_min=%d ok=%v", size, cMin, ok)
	}

	arcstatsPath = filepath.Join(t.TempDir(), "missing")
	if _, _, ok := readARCStats(); ok {
		t.Error("ok without ZFS loaded")
	}
}
