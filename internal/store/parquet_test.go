package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasParquetFiles(t *testing.T) {
	dir := t.TempDir()

	t.Run("empty directory", func(t *testing.T) {
		os.MkdirAll(filepath.Join(dir, "cpu_metrics"), 0755)
		if HasParquetFiles(dir, "cpu_metrics") {
			t.Error("expected false for empty directory")
		}
	})

	t.Run("non-existent table directory", func(t *testing.T) {
		if HasParquetFiles(dir, "nonexistent") {
			t.Error("expected false for non-existent directory")
		}
	})

	t.Run("directory with parquet files", func(t *testing.T) {
		tableDir := filepath.Join(dir, "memory_metrics")
		os.MkdirAll(tableDir, 0755)
		os.WriteFile(filepath.Join(tableDir, "2025-01.parquet"), []byte("test"), 0644)
		if !HasParquetFiles(dir, "memory_metrics") {
			t.Error("expected true when parquet files exist")
		}
	})

	t.Run("directory with non-parquet files only", func(t *testing.T) {
		tableDir := filepath.Join(dir, "disk_metrics")
		os.MkdirAll(tableDir, 0755)
		os.WriteFile(filepath.Join(tableDir, "readme.txt"), []byte("test"), 0644)
		if HasParquetFiles(dir, "disk_metrics") {
			t.Error("expected false when only non-parquet files exist")
		}
	})
}
