package main

import (
	"os"
	"strings"
	"testing"
)

// TestDaemonUnitHasNoDeviceAllowlist guards the shipped systemd units. Any
// directive that installs a device allowlist blocks the raw block-device opens
// SMART needs (EPERM, "Operation not permitted"). ProtectClock did exactly
// that on older systemd (249, Ubuntu 22.04) by implying DeviceAllow=char-rtc,
// and SMART was silently dead on such hosts for months.
func TestDaemonUnitHasNoDeviceAllowlist(t *testing.T) {
	forbidden := []string{"ProtectClock=", "ProtectKernelLogs=", "PrivateDevices=", "DevicePolicy=", "DeviceAllow="}
	for _, path := range []string{"../../debian/bewitch.bewitchd.service", "../../bewitchd.service"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				continue
			}
			for _, f := range forbidden {
				if strings.HasPrefix(line, f) {
					t.Errorf("%s:%d: %q installs a device allowlist that blocks SMART disk access", path, i+1, line)
				}
			}
		}
	}
}
