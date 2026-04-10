package api

import (
	"testing"
)

func TestArchiveViewTables(t *testing.T) {
	// Verify the view table list matches the expected metric tables.
	expected := map[string]bool{
		"cpu_metrics":         true,
		"memory_metrics":      true,
		"disk_metrics":        true,
		"network_metrics":     true,
		"ecc_metrics":         true,
		"temperature_metrics": true,
		"power_metrics":       true,
		"process_metrics":     true,
		"gpu_metrics":         true,
	}
	if len(archiveViewTables) != len(expected) {
		t.Fatalf("archiveViewTables has %d entries, want %d", len(archiveViewTables), len(expected))
	}
	for _, table := range archiveViewTables {
		if !expected[table] {
			t.Errorf("unexpected table in archiveViewTables: %s", table)
		}
	}
}
