package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mountPoints(ms []mountEntry) string {
	var p []string
	for _, m := range ms {
		p = append(p, m.mountPoint)
	}
	return strings.Join(p, ",")
}

func TestParseMountinfoAndDropBindDuplicates(t *testing.T) {
	cases := []struct {
		name string
		info string
		want string
	}{
		{
			// Real excerpt from bewitchd's private mount namespace on an LVM host
			// (sanitized): ReadWritePaths/StateDirectory and PrivateTmp bind subdirectories
			// of /var back into the tree. They must not show up as extra disks.
			name: "systemd sandbox binds on LVM",
			info: `1036 258 254:0 / / ro,nosuid,relatime shared:353 master:1 - ext4 /dev/mapper/host--vg-root rw,errors=remount-ro
1068 1036 0:46 / /tmp rw,nosuid,nodev shared:736 master:97 - tmpfs tmpfs rw,inode64
1081 1036 259:2 / /boot ro,nosuid,relatime shared:750 master:122 - ext4 /dev/nvme0n1p2 rw
1082 1081 259:1 / /boot/efi ro,nosuid,relatime shared:751 master:134 - vfat /dev/nvme0n1p1 rw
1083 1036 254:1 / /var ro,nosuid,relatime shared:752 master:126 - ext4 /dev/mapper/host--vg-var rw
1089 1036 0:56 / /mnt/nas ro,nosuid,noatime shared:760 master:196 - nfs4 192.0.2.10:/export/nas rw
1093 1036 8:65 / /mnt/backup ro,nosuid,relatime shared:764 master:611 - ext4 /dev/sde1 rw
1108 1083 254:1 /lib/bewitch /var/lib/bewitch rw,nosuid,relatime shared:757 master:126 - ext4 /dev/mapper/host--vg-var rw
1109 1083 254:1 /tmp/systemd-private-1fe2-bewitchd.service-7qou7n/tmp /var/tmp rw,nosuid,relatime shared:758 master:126 - ext4 /dev/mapper/host--vg-var rw`,
			want: "/,/boot,/boot/efi,/var,/mnt/backup",
		},
		{
			name: "btrfs sibling subvolumes are kept",
			info: `30 1 0:29 /@ / rw,relatime shared:1 - btrfs /dev/sda2 rw,subvol=/@
31 30 0:29 /@home /home rw,relatime shared:2 - btrfs /dev/sda2 rw,subvol=/@home`,
			want: "/,/home",
		},
		{
			name: "bind of a directory inside a btrfs subvolume is dropped",
			info: `30 1 0:29 /@ / rw,relatime shared:1 - btrfs /dev/sda2 rw,subvol=/@
40 30 0:29 /@/var/lib/bewitch /var/lib/bewitch rw,relatime shared:3 - btrfs /dev/sda2 rw,subvol=/@`,
			want: "/",
		},
		{
			name: "prefix match respects path boundaries",
			info: `30 1 0:29 /data / rw - btrfs /dev/sda2 rw
31 30 0:29 /data2 /mnt rw - btrfs /dev/sda2 rw`,
			want: "/,/mnt",
		},
		{
			// Proxmox: /etc/pve is FUSE from /dev/fuse. It stays a mount (usage is
			// still reported); SMART is skipped later because it isn't a block device.
			name: "proxmox fuse mount kept",
			info: `25 1 253:1 / / rw - ext4 /dev/mapper/pve-root rw
60 25 0:51 / /etc/pve rw - fuse /dev/fuse rw,user_id=0`,
			want: "/,/etc/pve",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mountPoints(dropBindDuplicates(parseMountinfo(tc.info))); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestParseMountsAppliesExcludes(t *testing.T) {
	f := filepath.Join(t.TempDir(), "mountinfo")
	info := "1 0 8:1 / / rw - ext4 /dev/sda1 rw\n2 1 8:2 / /snap/core rw - squashfs /dev/loop0 ro\n"
	if err := os.WriteFile(f, []byte(info), 0o644); err != nil {
		t.Fatal(err)
	}
	old := mountinfoPath
	mountinfoPath = f
	t.Cleanup(func() { mountinfoPath = old })

	c := &DiskCollector{excludeMounts: []string{"/snap/"}}
	ms, err := c.parseMounts()
	if err != nil {
		t.Fatal(err)
	}
	if got := mountPoints(ms); got != "/" {
		t.Errorf("got %s, want /", got)
	}
	if ms[0].device != "/dev/sda1" || ms[0].devID != "8:1" {
		t.Errorf("entry = %+v", ms[0])
	}
}
