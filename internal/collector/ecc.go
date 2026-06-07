package collector

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// edacGlob is the sysfs glob for EDAC memory controllers. A package var so the
// presence-detection test can point it at a fixture tree.
var edacGlob = "/sys/devices/system/edac/mc/mc*"

type ECCData struct {
	Corrected   uint64
	Uncorrected uint64
	// Present is true when the host exposes EDAC memory controllers. Without it the
	// zero sample (corrected=0, uncorrected=0) is indistinguishable from a machine
	// with no ECC RAM, which made the TUI assert "ECC ok" on non-ECC hardware and
	// would let the alert form offer ECC rules that can never fire.
	Present bool
}

type ECCCollector struct{}

func NewECCCollector() *ECCCollector {
	return &ECCCollector{}
}

func (c *ECCCollector) Name() string { return "ecc" }

func (c *ECCCollector) Collect() (Sample, error) {
	var corrected, uncorrected uint64

	// Walk the EDAC memory controllers' ce_count (corrected) and ue_count (uncorrectable).
	mcDirs, _ := filepath.Glob(edacGlob)
	for _, dir := range mcDirs {
		corrected += readUint(filepath.Join(dir, "ce_count"))
		uncorrected += readUint(filepath.Join(dir, "ue_count"))
	}

	return Sample{
		Timestamp: time.Now(),
		Kind:      "ecc",
		Data:      ECCData{Corrected: corrected, Uncorrected: uncorrected, Present: len(mcDirs) > 0},
	}, nil
}

func readUint(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return v
}
