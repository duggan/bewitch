package collector

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// GrantPowercapRead lets group gid read the powercap RAPL energy counters:
// each energy_uj under powercapRoot is set to group gid, mode 0440 (owner
// unchanged). It returns how many counters it changed.
//
// energy_uj has been root-only (0400) since kernel 5.10 (CVE-2020-8694). The
// daemon's other route to RAPL, perf events, needs only CAP_PERFMON on most
// kernels, but Debian's default perf_event_paranoid of 3 demands CAP_SYS_ADMIN,
// which the daemon deliberately doesn't hold. `bewitchd rapl-access` runs this
// as root before the daemon starts, exposing the counters to the bewitch group
// only. sysfs permissions don't survive a reboot or a reload of the RAPL
// driver, so it runs on every start.
//
// Symlinks are resolved first and anything that doesn't resolve to a file named
// energy_uj is left alone, so only the counters themselves are touched.
func GrantPowercapRead(gid int) (int, error) {
	paths, _ := filepath.Glob(filepath.Join(powercapRoot, "*/energy_uj"))
	nested, _ := filepath.Glob(filepath.Join(powercapRoot, "*/*/energy_uj"))
	paths = append(paths, nested...)

	seen := make(map[string]bool)
	var errs []error
	for _, p := range paths {
		real, err := filepath.EvalSymlinks(p)
		if err != nil || seen[real] || filepath.Base(real) != "energy_uj" {
			continue
		}
		seen[real] = true
		info, err := os.Stat(real)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if err := os.Chown(real, -1, gid); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", real, err))
			continue
		}
		if err := os.Chmod(real, 0o440); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", real, err))
			continue
		}
	}
	return len(seen) - len(errs), errors.Join(errs...)
}
