package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasParquetFiles(t *testing.T) {
	dir := t.TempDir()

	t.Run("empty directory", func(t *testing.T) {
		os.MkdirAll(filepath.Join(dir, "cpu_metrics"), 0755)
		if hasParquetFiles(dir, "cpu_metrics") {
			t.Error("expected false for empty directory")
		}
	})

	t.Run("non-existent table directory", func(t *testing.T) {
		if hasParquetFiles(dir, "nonexistent") {
			t.Error("expected false for non-existent directory")
		}
	})

	t.Run("directory with parquet files", func(t *testing.T) {
		tableDir := filepath.Join(dir, "memory_metrics")
		os.MkdirAll(tableDir, 0755)
		os.WriteFile(filepath.Join(tableDir, "2025-01.parquet"), []byte("test"), 0644)
		if !hasParquetFiles(dir, "memory_metrics") {
			t.Error("expected true when parquet files exist")
		}
	})

	t.Run("directory with non-parquet files only", func(t *testing.T) {
		tableDir := filepath.Join(dir, "disk_metrics")
		os.MkdirAll(tableDir, 0755)
		os.WriteFile(filepath.Join(tableDir, "readme.txt"), []byte("test"), 0644)
		if hasParquetFiles(dir, "disk_metrics") {
			t.Error("expected false when only non-parquet files exist")
		}
	})
}

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
