package db

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRecoverInterruptedCompaction covers the half-swapped states a compaction
// can leave behind if the process dies mid-swap.
func TestRecoverInterruptedCompaction(t *testing.T) {
	t.Run("restores original when main file is missing", func(t *testing.T) {
		// Crash after renaming the original aside, before moving the compacted
		// file into place: only the backup exists.
		path := filepath.Join(t.TempDir(), "x.duckdb")
		backup := path + PreCompactSuffix
		if err := os.WriteFile(backup, []byte("ORIGINAL"), 0o644); err != nil {
			t.Fatal(err)
		}
		recoverInterruptedCompaction(path)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("main file not restored: %v", err)
		}
		if string(data) != "ORIGINAL" {
			t.Errorf("restored content = %q, want ORIGINAL", data)
		}
		if _, err := os.Stat(backup); !os.IsNotExist(err) {
			t.Error("backup should be gone after restore")
		}
	})

	t.Run("drops stale backup when main file is present", func(t *testing.T) {
		// Crash after the compacted file is in place, before the backup is
		// removed: both exist. The (compacted) main file is authoritative.
		path := filepath.Join(t.TempDir(), "x.duckdb")
		backup := path + PreCompactSuffix
		os.WriteFile(path, []byte("COMPACTED"), 0o644)
		os.WriteFile(backup, []byte("OLD"), 0o644)
		recoverInterruptedCompaction(path)
		data, _ := os.ReadFile(path)
		if string(data) != "COMPACTED" {
			t.Errorf("main file = %q, want COMPACTED (untouched)", data)
		}
		if _, err := os.Stat(backup); !os.IsNotExist(err) {
			t.Error("stale backup should be removed")
		}
	})

	t.Run("no-op without a backup", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "x.duckdb")
		os.WriteFile(path, []byte("MAIN"), 0o644)
		recoverInterruptedCompaction(path)
		data, _ := os.ReadFile(path)
		if string(data) != "MAIN" {
			t.Errorf("main file = %q, want MAIN", data)
		}
	})
}
