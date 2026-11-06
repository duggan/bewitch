package store

import (
	"path/filepath"
	"testing"
	"time"
)

// TestArchiveChunkedExportAndDelete verifies the per-day archive behaviour:
// data older than the threshold is exported to one Parquet file per day, deleted
// from DuckDB, and archive_state is advanced. Recent data (newer than the
// threshold) must be left untouched. This guards the rewrite that made archive
// incremental/resumable instead of one giant paused operation.
func TestArchiveChunkedExportAndDelete(t *testing.T) {
	s := newPruneTestStore(t)
	archiveDir := filepath.Join(t.TempDir(), "archive")

	now := time.Now().UTC()
	// Insert memory_metrics across three distinct days that are all older than the
	// archive threshold, plus one recent row that must NOT be archived.
	day := func(d int) time.Time {
		return now.AddDate(0, 0, -d).Truncate(time.Hour)
	}
	insert := func(ts time.Time) {
		if _, err := s.db.Exec(
			`INSERT INTO memory_metrics (ts, total_bytes, used_bytes) VALUES (?, ?, ?)`,
			ts, 2_000_000_000, 1_000_000_000); err != nil {
			t.Fatalf("insert memory_metrics: %v", err)
		}
	}
	// Three rows on three separate old days.
	insert(day(10))
	insert(day(9))
	insert(day(8))
	// A recent row (1 hour ago) that is newer than a 7d threshold.
	recent := now.Add(-1 * time.Hour)
	insert(recent)

	// Archive everything older than 7 days.
	if err := s.Archive(archiveDir, 7*24*time.Hour); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// The recent row must remain; the three old rows must be gone.
	var remaining int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM memory_metrics`).Scan(&remaining); err != nil {
		t.Fatalf("count memory_metrics: %v", err)
	}
	if remaining != 1 {
		t.Errorf("rows remaining in DB = %d, want 1 (only the recent row)", remaining)
	}

	// One Parquet file per old day should exist.
	files, err := filepath.Glob(filepath.Join(archiveDir, "memory_metrics", "*.parquet"))
	if err != nil {
		t.Fatalf("glob parquet: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("parquet files = %d, want 3 (one per archived day)", len(files))
	}

	// archive_state must have advanced for memory_metrics.
	var archived time.Time
	err = s.db.QueryRow(`SELECT last_archived_ts FROM archive_state WHERE table_name = 'memory_metrics'`).Scan(&archived)
	if err != nil {
		t.Fatalf("read archive_state: %v", err)
	}
	if archived.IsZero() {
		t.Error("archive_state.last_archived_ts not advanced")
	}

	// The archived rows must be readable back from Parquet (data not lost).
	var total int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM read_parquet(?)`,
		filepath.Join(archiveDir, "memory_metrics", "*.parquet")).Scan(&total); err != nil {
		t.Fatalf("read_parquet count: %v", err)
	}
	if total != 3 {
		t.Errorf("rows in parquet = %d, want 3", total)
	}
}

// TestArchiveMidnightRows: archive days are [midnight, next midnight). With the
// old (start, end] ranges a row exactly at midnight of the oldest day was never
// exported, and later midnight rows were filed under the previous day. It only
// showed when TestArchiveChunkedExportAndDelete ran between 00:00 and 01:00 UTC
// (its hour-truncated timestamps landed on midnight); this test always does.
func TestArchiveMidnightRows(t *testing.T) {
	s := newPruneTestStore(t)
	archiveDir := filepath.Join(t.TempDir(), "archive")

	today := time.Now().UTC().Truncate(24 * time.Hour)
	var dates []string
	for _, d := range []int{10, 9, 8} {
		ts := today.AddDate(0, 0, -d)
		dates = append(dates, ts.Format("2006-01-02"))
		if _, err := s.db.Exec(`INSERT INTO memory_metrics (ts, total_bytes, used_bytes) VALUES (?, ?, ?)`,
			ts, 2_000_000_000, 1_000_000_000); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Archive(archiveDir, 7*24*time.Hour); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	var remaining int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM memory_metrics`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Errorf("%d midnight rows left in DuckDB, want 0", remaining)
	}
	for _, date := range dates {
		var n int
		path := filepath.Join(archiveDir, "memory_metrics", date+".parquet")
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM read_parquet(?)`, path).Scan(&n); err != nil {
			t.Errorf("%s: %v", date, err)
			continue
		}
		if n != 1 {
			t.Errorf("%s.parquet has %d rows, want its own midnight row", date, n)
		}
	}
}
