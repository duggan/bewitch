package collector

import "testing"

func TestPhysicalDevice(t *testing.T) {
	// Note: physicalDevice resolves symlinks and reads /sys/block on Linux.
	// On macOS, the symlink resolution may or may not work, and /sys/block
	// doesn't exist, so only test the suffix-stripping logic with simple paths
	// (no symlinks or sysfs). dm-* and md* tests that rely on /sys are skipped.

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"NVMe with partition", "/dev/nvme0n1p2", "/dev/nvme0n1"},
		{"NVMe no partition", "/dev/nvme0n1", "/dev/nvme0n1"},
		{"NVMe multi-digit partition", "/dev/nvme1n1p15", "/dev/nvme1n1"},
		{"SATA with partition", "/dev/sda1", "/dev/sda"},
		{"SATA no partition", "/dev/sda", "/dev/sda"},
		{"SATA multi-digit partition", "/dev/sda12", "/dev/sda"},
		{"virtio with partition", "/dev/vda1", "/dev/vda"},
		{"virtio no partition", "/dev/vda", "/dev/vda"},
		// NVMe pN suffix where N is not a number shouldn't strip
		{"NVMe no numeric suffix after p", "/dev/nvme0n1p", "/dev/nvme0n1p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := physicalDevice(tt.input)
			if got != tt.want {
				t.Errorf("physicalDevice(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDeviceBaseName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/dev/sda1", "sda1"},
		{"/dev/nvme0n1p2", "nvme0n1p2"},
		{"sda", "sda"},
		{"/a/b/c/d", "d"},
		{"/dev/mapper/ubuntu--vg-root", "ubuntu--vg-root"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := deviceBaseName(tt.input)
			if got != tt.want {
				t.Errorf("deviceBaseName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
