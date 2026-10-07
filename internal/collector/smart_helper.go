package collector

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/charmbracelet/log"
)

// Privileged NVMe SMART helper.
//
// On recent kernels the NVMe health log (Get Log Page, an admin command) can
// only be read with CAP_SYS_ADMIN. Rather than hand that capability to the
// network-facing daemon, a separate systemd unit (bewitch-smart.service, run
// by bewitch-smart.timer) executes `bewitchd smart-dump`, which takes no input
// beyond its output directory, reads every NVMe namespace, and writes one JSON
// snapshot per device. The daemon reads those snapshots instead of querying
// the device itself — so the code holding CAP_SYS_ADMIN has no attack surface.

// smartSnapshotVersion is bumped if the snapshot layout changes incompatibly.
const smartSnapshotVersion = 1

// helperMinMaxAge is the floor on how old a helper snapshot may be before the
// daemon ignores it. The timer runs every 5m, so this tolerates a missed run
// even when smart_interval is set below the timer cadence.
const helperMinMaxAge = 15 * time.Minute

// smartSnapshot is the on-disk format written by the helper.
type smartSnapshot struct {
	Version int       `json:"version"`
	Device  string    `json:"device"`
	ReadAt  time.Time `json:"read_at"`
	Info    SMARTInfo `json:"info"`
}

// nvmeNamespaceRe matches NVMe namespace block devices (nvme0n1), not
// partitions (nvme0n1p1) or controller char devices (nvme0).
var nvmeNamespaceRe = regexp.MustCompile(`^nvme\d+n\d+$`)

// sysBlockDir is a package var so tests can point enumeration at a fixture.
var sysBlockDir = "/sys/block"

func isNVMeDevice(devPath string) bool {
	return nvmeNamespaceRe.MatchString(deviceBaseName(devPath))
}

// readHelperSMART returns a fresh helper snapshot for devPath, or nil when the
// helper is disabled, the device isn't NVMe, or no usable snapshot exists.
func (c *DiskCollector) readHelperSMART(devPath string) *SMARTInfo {
	if c.helperDir == "" || !isNVMeDevice(devPath) {
		return nil
	}
	maxAge := 2 * c.smartInterval
	if maxAge < helperMinMaxAge {
		maxAge = helperMinMaxAge
	}
	return loadSMARTSnapshot(c.helperDir, devPath, maxAge, time.Now())
}

func loadSMARTSnapshot(dir, devPath string, maxAge time.Duration, now time.Time) *SMARTInfo {
	data, err := os.ReadFile(filepath.Join(dir, deviceBaseName(devPath)+".json"))
	if err != nil {
		return nil
	}
	var snap smartSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		log.Debugf("smart: ignoring malformed helper snapshot for %s: %v", devPath, err)
		return nil
	}
	if snap.Version != smartSnapshotVersion || snap.Device != devPath || !snap.Info.Available {
		return nil
	}
	if age := now.Sub(snap.ReadAt); age > maxAge || age < -time.Minute {
		return nil
	}
	info := snap.Info
	return &info
}

// listNVMeDevices returns /dev paths for every NVMe namespace on the system.
func listNVMeDevices() ([]string, error) {
	entries, err := os.ReadDir(sysBlockDir)
	if err != nil {
		return nil, err
	}
	var devs []string
	for _, e := range entries {
		if nvmeNamespaceRe.MatchString(e.Name()) {
			devs = append(devs, "/dev/"+e.Name())
		}
	}
	return devs, nil
}

// DumpNVMeSMART is the body of `bewitchd smart-dump`: it reads SMART from every
// NVMe namespace and atomically writes <outDir>/<dev>.json for each readable
// one. A device that can't be read has its old snapshot removed so the daemon
// never trusts stale data. Returns an error only if nothing could be written
// for a host that has NVMe devices.
func DumpNVMeSMART(outDir string) error {
	devs, err := listNVMeDevices()
	if err != nil {
		return fmt.Errorf("listing NVMe devices: %w", err)
	}
	if len(devs) == 0 {
		log.Infof("smart-dump: no NVMe devices found")
		return nil
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", outDir, err)
	}

	smartctlPath := detectSmartctl()
	c := &DiskCollector{
		smartLoggedErr: make(map[string]bool),
		useSmartctl:    smartctlPath != "",
		smartctlPath:   smartctlPath,
		dumpMode:       true,
		// helperDir stays empty: the helper must read the device, not itself.
	}

	written := 0
	for _, dev := range devs {
		path := filepath.Join(outDir, deviceBaseName(dev)+".json")
		info := c.readSMARTDevice(dev)
		if !info.Available {
			_ = os.Remove(path)
			continue
		}
		snap := smartSnapshot{Version: smartSnapshotVersion, Device: dev, ReadAt: time.Now().UTC(), Info: *info}
		if err := writeFileAtomic(path, snap); err != nil {
			log.Warnf("smart-dump: writing %s: %v", path, err)
			continue
		}
		written++
	}
	log.Infof("smart-dump: wrote %d of %d NVMe snapshots to %s", written, len(devs), outDir)
	if written == 0 {
		return fmt.Errorf("no NVMe SMART data could be read")
	}
	return nil
}

func writeFileAtomic(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".smart-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
