package collector

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ECCData struct {
	Corrected   uint64
	Uncorrected uint64
}

type ECCCollector struct{}

func NewECCCollector() *ECCCollector {
	return &ECCCollector{}
}

func (c *ECCCollector) Name() string { return "ecc" }

func (c *ECCCollector) Collect() (Sample, error) {
	var corrected, uncorrected uint64

	// Walk /sys/devices/system/edac/mc/*/ce_count and ue_count
	mcDirs, _ := filepath.Glob("/sys/devices/system/edac/mc/mc*")
	for _, dir := range mcDirs {
		corrected += readUint(filepath.Join(dir, "ce_count"))
		uncorrected += readUint(filepath.Join(dir, "ue_count"))
	}

	return Sample{
		Timestamp: time.Now(),
		Kind:      "ecc",
		Data:      ECCData{Corrected: corrected, Uncorrected: uncorrected},
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
