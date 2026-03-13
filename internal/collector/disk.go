package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	smart "github.com/anatol/smart.go"
	"github.com/charmbracelet/log"
	"github.com/prometheus/procfs/blockdevice"
	"golang.org/x/sys/unix"
)

type SMARTInfo struct {
	Available          bool
	Healthy            bool
	Temperature        uint64
	PowerOnHours       uint64
	PowerCycles        uint64
	ReadSectors        uint64
	WrittenSectors     uint64
	ReallocatedSectors uint64
	PendingSectors     uint64
	UncorrectableErrs  uint64
	ReadErrorRate      uint64
	AvailableSpare     uint8 // NVMe
	PercentUsed        uint8 // NVMe
}

type DiskMountSample struct {
	Mount         string
	Device        string
	Transport     string // "nvme", "sata", "usb", "virtio", "mmc", "scsi", ""
	TotalBytes    uint64
	UsedBytes     uint64
	FreeBytes     uint64
	ReadBytesSec  float64
	WriteBytesSec float64
	ReadIOPS      float64
	WriteIOPS     float64
	SMART         *SMARTInfo // nil if unavailable
}

type DiskData struct {
	Mounts []DiskMountSample
}

type DiskCollector struct {
	bfs            blockdevice.FS
	prevIO         map[string]blockdevice.Diskstats
	prevTime       time.Time
	excludeMounts  []string
	smartInterval  time.Duration
	smartCache     map[string]*SMARTInfo // keyed by physical device path
	smartCacheTime time.Time
	smartLoggedErr map[string]bool // suppress repeated open errors
	useSmartctl    bool              // true if smartctl binary was found at startup
	smartctlPath   string            // full path to smartctl binary
	transportCache map[string]string // keyed by physical device path → "nvme", "sata", "usb", etc.
}

func NewDiskCollector(excludeMounts []string, smartInterval time.Duration) (*DiskCollector, error) {
	bfs, err := blockdevice.NewDefaultFS()
	if err != nil {
		return nil, fmt.Errorf("creating blockdevice fs: %w", err)
	}
	stats, err := bfs.ProcDiskstats()
	if err != nil {
		return nil, fmt.Errorf("initial diskstats: %w", err)
	}
	m := make(map[string]blockdevice.Diskstats, len(stats))
	for _, s := range stats {
		m[s.Info.DeviceName] = s
	}
	smartctlPath := detectSmartctl()
	if smartctlPath != "" {
		log.Infof("smart: using smartctl at %s", smartctlPath)
	} else {
		log.Infof("smart: smartctl not found, using library fallback")
	}

	return &DiskCollector{
		bfs:            bfs,
		prevIO:         m,
		prevTime:       time.Now(),
		excludeMounts:  excludeMounts,
		smartInterval:  smartInterval,
		smartCache:     make(map[string]*SMARTInfo),
		smartLoggedErr: make(map[string]bool),
		useSmartctl:    smartctlPath != "",
		smartctlPath:   smartctlPath,
		transportCache: make(map[string]string),
	}, nil
}

func (c *DiskCollector) Name() string { return "disk" }

func (c *DiskCollector) Collect() (Sample, error) {
	now := time.Now()
	dt := now.Sub(c.prevTime).Seconds()
	if dt == 0 {
		dt = 1
	}

	stats, err := c.bfs.ProcDiskstats()
	if err != nil {
		return Sample{}, fmt.Errorf("reading diskstats: %w", err)
	}
	curIO := make(map[string]blockdevice.Diskstats, len(stats))
	for _, s := range stats {
		curIO[s.Info.DeviceName] = s
	}

	mounts, err := c.parseMounts()
	if err != nil {
		return Sample{}, fmt.Errorf("reading mounts: %w", err)
	}

	// Refresh SMART cache if stale
	if c.smartInterval > 0 && (time.Since(c.smartCacheTime) > c.smartInterval || len(c.smartCache) == 0) {
		c.refreshSMARTCache(mounts)
	}

	samples := make([]DiskMountSample, 0, len(mounts))
	for _, mt := range mounts {
		var stat unix.Statfs_t
		if err := unix.Statfs(mt.mountPoint, &stat); err != nil {
			continue
		}
		total := stat.Blocks * uint64(stat.Bsize)
		free := stat.Bavail * uint64(stat.Bsize)
		used := total - (stat.Bfree * uint64(stat.Bsize))

		devName := deviceBaseName(mt.device)
		var readBps, writeBps, readIOPS, writeIOPS float64
		if cur, ok := curIO[devName]; ok {
			if prev, ok2 := c.prevIO[devName]; ok2 {
				readBps = float64(cur.IOStats.ReadSectors-prev.IOStats.ReadSectors) * 512 / dt
				writeBps = float64(cur.IOStats.WriteSectors-prev.IOStats.WriteSectors) * 512 / dt
				readIOPS = float64(cur.IOStats.ReadIOs-prev.IOStats.ReadIOs) / dt
				writeIOPS = float64(cur.IOStats.WriteIOs-prev.IOStats.WriteIOs) / dt
			}
		}

		physDev := physicalDevice(mt.device)

		s := DiskMountSample{
			Mount:         mt.mountPoint,
			Device:        mt.device,
			Transport:     c.detectTransport(physDev),
			TotalBytes:    total,
			UsedBytes:     used,
			FreeBytes:     free,
			ReadBytesSec:  readBps,
			WriteBytesSec: writeBps,
			ReadIOPS:      readIOPS,
			WriteIOPS:     writeIOPS,
		}

		// Attach SMART data from cache
		if si, ok := c.smartCache[physDev]; ok && si.Available {
			s.SMART = si
		}

		samples = append(samples, s)
	}

	c.prevIO = curIO
	c.prevTime = now

	return Sample{
		Timestamp: now,
		Kind:      "disk",
		Data:      DiskData{Mounts: samples},
	}, nil
}

// refreshSMARTCache reads SMART data from each unique physical device.
func (c *DiskCollector) refreshSMARTCache(mounts []mountEntry) {
	physDevs := make(map[string]bool)
	for _, mt := range mounts {
		physDevs[physicalDevice(mt.device)] = true
	}

	newCache := make(map[string]*SMARTInfo, len(physDevs))
	for devPath := range physDevs {
		info := c.readSMARTDevice(devPath)
		newCache[devPath] = info
	}

	c.smartCache = newCache
	c.smartCacheTime = time.Now()
}

// readSMARTDevice tries to read SMART data from a physical device.
// It tries smartctl first (if available), then the smart.go library, then
// direct SAT passthrough as a last resort.
func (c *DiskCollector) readSMARTDevice(devPath string) *SMARTInfo {
	// Try smartctl first — broadest hardware support.
	if c.useSmartctl {
		info, smartctlErr := readSMARTFromSmartctl(c.smartctlPath, devPath)
		if smartctlErr == nil {
			delete(c.smartLoggedErr, devPath)
			return info
		}
		if !c.smartLoggedErr[devPath] {
			log.Warnf("smart: smartctl failed for %s: %v", devPath, smartctlErr)
		}
	}

	// Try the smart.go library (handles NVMe, well-behaved SATA, SCSI).
	dev, err := openSMART(devPath)
	if err == nil {
		info := readSMARTFromLib(dev)
		if closeErr := dev.Close(); closeErr != nil {
			log.Warnf("smart: error closing %s: %v", devPath, closeErr)
		}
		return info
	}

	// Library failed — try direct SAT passthrough for SATA drives with
	// broken vendor ident detection.
	if info, satErr := satSMARTInfo(devPath); satErr == nil {
		if c.smartLoggedErr[devPath] {
			delete(c.smartLoggedErr, devPath)
		}
		return info
	}

	// All paths failed.
	if !c.smartLoggedErr[devPath] {
		log.Warnf("smart: cannot read %s: %v", devPath, err)
		c.smartLoggedErr[devPath] = true
	}
	return &SMARTInfo{} // Available=false
}

// openSMART tries smart.Open() auto-detection first, then falls back to
// protocol-specific opens (OpenSata, OpenNVMe) which bypass the auto-detect
// logic that fails on some kernels/drivers.
func openSMART(devPath string) (smart.Device, error) {
	dev, err := smart.Open(devPath)
	if err == nil {
		return dev, nil
	}
	// Auto-detection failed. Try protocol-specific opens based on device name.
	base := deviceBaseName(devPath)
	if strings.Contains(base, "nvme") {
		return smart.OpenNVMe(devPath)
	}
	// Try SATA (covers sd*, vd*, hd* — most common case for "unknown drive type")
	if sataDev, sataErr := smart.OpenSata(devPath); sataErr == nil {
		return sataDev, nil
	}
	return nil, err // return original auto-detect error
}

// readSMARTFromLib extracts SMARTInfo from a smart.Device opened by the library.
func readSMARTFromLib(dev smart.Device) *SMARTInfo {
	info := &SMARTInfo{Available: true, Healthy: true}
	attrs, err := dev.ReadGenericAttributes()
	if err == nil {
		info.Temperature = attrs.Temperature
		info.PowerOnHours = attrs.PowerOnHours
		info.PowerCycles = attrs.PowerCycles
		info.ReadSectors = attrs.Read
		info.WrittenSectors = attrs.Written
	}

	switch sd := dev.(type) {
	case *smart.SataDevice:
		if page, err := sd.ReadSMARTData(); err == nil {
			if attr, ok := page.Attrs[5]; ok { // Reallocated_Sector_Ct
				info.ReallocatedSectors = attr.ValueRaw
			}
			if attr, ok := page.Attrs[197]; ok { // Current_Pending_Sector
				info.PendingSectors = attr.ValueRaw
			}
			if attr, ok := page.Attrs[198]; ok { // Offline_Uncorrectable
				info.UncorrectableErrs = attr.ValueRaw
			}
			if attr, ok := page.Attrs[1]; ok { // Raw_Read_Error_Rate
				info.ReadErrorRate = attr.ValueRaw
			}
			info.Healthy = info.ReallocatedSectors == 0 && info.PendingSectors == 0 && info.UncorrectableErrs == 0
		}
	case *smart.NVMeDevice:
		if smartLog, err := sd.ReadSMART(); err == nil {
			info.AvailableSpare = smartLog.AvailSpare
			info.PercentUsed = smartLog.PercentUsed
			info.Healthy = smartLog.CritWarning == 0
		}
	default:
		// Unknown device type (e.g., SCSI/USB mass storage) — the library
		// could open it but can't read protocol-specific health data.
		// If we got no useful generic attributes either, don't claim availability.
		if err != nil {
			return &SMARTInfo{} // Available=false
		}
	}
	return info
}

// physicalDevice resolves a device path to the underlying physical block device.
// It handles symlinks (/dev/mapper/*, /dev/disk/by-*), device-mapper (dm-*),
// and partition suffixes (NVMe pN, SATA trailing digits).
func physicalDevice(devPath string) string {
	// Resolve symlinks: /dev/mapper/ubuntu--vg-root → /dev/dm-0,
	// /dev/disk/by-id/... → /dev/sda1, etc.
	resolved, err := filepath.EvalSymlinks(devPath)
	if err != nil {
		resolved = devPath
	}

	base := deviceBaseName(resolved)

	// Device-mapper (dm-*): walk /sys/block/dm-N/slaves/ to find the
	// underlying physical device. LVM, LUKS, and multipath all use dm.
	if strings.HasPrefix(base, "dm-") {
		if phys := blockSlaveDevice(base); phys != "" {
			return phys
		}
		return resolved // can't resolve further; smart.Open will fail gracefully
	}

	// MD RAID (md0, md127, md0p1): walk /sys/block/mdN/slaves/ to find
	// the underlying physical drives. Strip partition suffix first (md0p1 → md0).
	if strings.HasPrefix(base, "md") {
		mdBase := base
		if idx := strings.Index(base, "p"); idx > 2 { // "md0p1" → "md0"
			if _, err := strconv.Atoi(base[idx+1:]); err == nil {
				mdBase = base[:idx]
			}
		}
		if phys := blockSlaveDevice(mdBase); phys != "" {
			return phys
		}
		return "/dev/" + mdBase
	}

	// NVMe: nvme0n1p2 → nvme0n1 (strip pN suffix)
	if strings.Contains(base, "nvme") {
		if idx := strings.LastIndex(base, "p"); idx > 0 {
			suffix := base[idx+1:]
			if _, err := strconv.Atoi(suffix); err == nil {
				return "/dev/" + base[:idx]
			}
		}
		return "/dev/" + base
	}
	// SATA/SCSI/virtio: sda1 → sda, vda1 → vda (strip trailing digits)
	i := len(base) - 1
	for i >= 0 && base[i] >= '0' && base[i] <= '9' {
		i--
	}
	if i < len(base)-1 && i >= 0 {
		return "/dev/" + base[:i+1]
	}
	return "/dev/" + base
}

// blockSlaveDevice walks /sys/block/<dm>/slaves/ to find the underlying physical
// device. For simple LVM/LUKS, there's typically one slave (e.g., sda2).
// For stacked mappers (LVM on LUKS), it recurses until a real device is found.
func blockSlaveDevice(dmName string) string {
	slavesDir := "/sys/block/" + dmName + "/slaves"
	entries, err := os.ReadDir(slavesDir)
	if err != nil || len(entries) == 0 {
		return ""
	}
	// Take the first slave — for SMART purposes any slave leads to the
	// same physical disk (multipath has identical drives, RAID doesn't
	// support per-device SMART via dm anyway).
	slave := entries[0].Name()
	if strings.HasPrefix(slave, "dm-") {
		return blockSlaveDevice(slave) // recurse through stacked mappers
	}
	// Got a real block device name (e.g., sda2, nvme0n1p3) — strip partition
	return physicalDevice("/dev/" + slave)
}

// detectTransport returns the bus/transport type for a physical block device.
// Results are cached since transport type never changes at runtime.
// Returns "nvme", "sata", "usb", "virtio", "mmc", "scsi", or "" if unknown.
func (c *DiskCollector) detectTransport(physDev string) string {
	if t, ok := c.transportCache[physDev]; ok {
		return t
	}
	t := detectTransportSysfs(deviceBaseName(physDev))
	c.transportCache[physDev] = t
	return t
}

// detectTransportSysfs walks the sysfs device tree for a block device to
// determine its transport type. It reads the symlink at
// /sys/block/<dev>/device to find the device's bus hierarchy, then checks
// for known subsystem indicators (usb, ata, nvme, virtio, mmc).
func detectTransportSysfs(devName string) string {
	// NVMe is identifiable from the device name alone and always correct.
	if strings.HasPrefix(devName, "nvme") {
		return "nvme"
	}
	if strings.HasPrefix(devName, "vd") {
		return "virtio"
	}
	if strings.HasPrefix(devName, "mmcblk") {
		return "mmc"
	}

	// For SATA vs USB vs SCSI, walk the sysfs device ancestry.
	// /sys/block/sda/device is a symlink into the device tree.
	devLink, err := filepath.EvalSymlinks("/sys/block/" + devName + "/device")
	if err != nil {
		return ""
	}

	// Walk up the path looking for bus indicators in ancestor directory names.
	// e.g., /sys/devices/pci0000:00/0000:00:1f.2/ata1/host0/target0:0:0/0:0:0:0
	//   → contains "ata" → SATA
	// e.g., /sys/devices/pci0000:00/0000:00:14.0/usb2/2-1/2-1:1.0/host3/target3:0:0/3:0:0:0
	//   → contains "usb" → USB
	parts := strings.Split(devLink, "/")
	for _, p := range parts {
		// Check for USB first — USB-attached SATA drives have both "usb"
		// and sometimes "ata" in the path, but USB is the actual transport.
		if strings.HasPrefix(p, "usb") {
			return "usb"
		}
	}
	for _, p := range parts {
		if strings.HasPrefix(p, "ata") {
			return "sata"
		}
	}

	return "scsi"
}

type mountEntry struct {
	device     string
	mountPoint string
}

func (c *DiskCollector) parseMounts() ([]mountEntry, error) {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil, err
	}
	var mounts []mountEntry
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if !strings.HasPrefix(fields[0], "/dev/") {
			continue
		}
		// Check mount path exclusions
		mountPoint := fields[1]
		excluded := false
		for _, prefix := range c.excludeMounts {
			if strings.HasPrefix(mountPoint, prefix) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		mounts = append(mounts, mountEntry{device: fields[0], mountPoint: mountPoint})
	}
	return mounts, nil
}

func deviceBaseName(dev string) string {
	parts := strings.Split(dev, "/")
	return parts[len(parts)-1]
}
