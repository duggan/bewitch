package collector

import (
	"testing"

	"github.com/prometheus/procfs"
)

// TestMemoryFromStatus guards against re-introducing the 1024× RSS inflation bug.
// procfs returns VmRSS/VmSize/VmLib/VmSwap already in bytes, so memoryFromStatus
// must pass them through unchanged. The values below are realistic byte counts
// for a ~1 GB process; if any factor (e.g. *1024) creeps back in, the result
// would exceed plausible physical memory and this test fails.
func TestMemoryFromStatus(t *testing.T) {
	status := procfs.ProcStatus{
		VmRSS:  1_048_576_000, // ~1 GiB in bytes (as procfs reports it)
		VmSize: 2_097_152_000,
		VmLib:  4_194_304,
		VmSwap: 524_288,
	}

	rss, vss, shared, swap := memoryFromStatus(status)

	if rss != status.VmRSS {
		t.Errorf("rss = %d, want %d (no unit conversion)", rss, status.VmRSS)
	}
	if vss != status.VmSize {
		t.Errorf("vss = %d, want %d", vss, status.VmSize)
	}
	if shared != status.VmLib {
		t.Errorf("shared = %d, want %d", shared, status.VmLib)
	}
	if swap != status.VmSwap {
		t.Errorf("swap = %d, want %d", swap, status.VmSwap)
	}
}
