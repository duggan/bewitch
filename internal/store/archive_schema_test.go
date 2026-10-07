package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Archives outlive schema migrations: Parquet written before migration 000012
// added cpu_metrics.steal_pct has one column fewer than the live table. These
// tests write such "pre-migration" files and check every archive path still
// works, matching columns by name (see ArchiveScan). Before the fix, the daily
// merge failed with "Set operations can only apply to expressions with the same
// number of result columns" on every run, so archiving silently stalled.

// writeOldSchemaCPU archives one cpu_metrics row at ts into path WITHOUT the
// steal_pct column, as a pre-000012 daemon would have, leaving the table empty.
func writeOldSchemaCPU(t *testing.T, s *Store, path string, ts time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	mustExec(t, s.db, `INSERT INTO cpu_metrics (ts, core, user_pct, system_pct, idle_pct, iowait_pct) VALUES (?, -1, 10, 5, 80, 5)`, ts)
	mustExec(t, s.db, `COPY (SELECT * EXCLUDE (steal_pct) FROM cpu_metrics) TO '`+quoteLiteral(path)+`' (FORMAT parquet)`)
	mustExec(t, s.db, `DELETE FROM cpu_metrics`)
}

// writeNewSchemaCPU archives one row (steal_pct = 1.5) with the current schema.
func writeNewSchemaCPU(t *testing.T, s *Store, path string, ts time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	mustExec(t, s.db, `INSERT INTO cpu_metrics (ts, core, user_pct, system_pct, idle_pct, iowait_pct, steal_pct) VALUES (?, -1, 20, 5, 70, 5, 1.5)`, ts)
	mustExec(t, s.db, `COPY (SELECT * FROM cpu_metrics) TO '`+quoteLiteral(path)+`' (FORMAT parquet)`)
	mustExec(t, s.db, `DELETE FROM cpu_metrics`)
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func TestArchiveMergeIntoPreMigrationFile(t *testing.T) {
	s := newPruneTestStore(t)
	dir := t.TempDir()
	day := time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(dir, "cpu_metrics", "2026-06-06.parquet")

	writeOldSchemaCPU(t, s, path, day.Add(1*time.Hour))
	// New rows for the same day arrive after the upgrade and must merge in.
	mustExec(t, s.db, `INSERT INTO cpu_metrics (ts, core, user_pct, system_pct, idle_pct, iowait_pct, steal_pct) VALUES (?, -1, 20, 5, 70, 5, 1.5)`, day.Add(2*time.Hour))

	if err := s.exportToParquet("cpu_metrics", path, day, day.Add(24*time.Hour)); err != nil {
		t.Fatalf("merge into pre-migration file: %v", err)
	}

	var total, withSteal int
	if err := s.db.QueryRow(`SELECT COUNT(*), COUNT(steal_pct) FROM read_parquet('`+quoteLiteral(path)+`')`).Scan(&total, &withSteal); err != nil {
		t.Fatalf("reading merged file (should now carry steal_pct): %v", err)
	}
	if total != 2 || withSteal != 1 {
		t.Errorf("merged file: rows=%d with steal=%d, want 2 and 1 (old row NULL)", total, withSteal)
	}
}

func TestUnarchiveMixedSchemaFiles(t *testing.T) {
	s := newPruneTestStore(t)
	dir := t.TempDir()
	writeOldSchemaCPU(t, s, filepath.Join(dir, "cpu_metrics", "2026-06-05.parquet"), time.Date(2026, 6, 5, 1, 0, 0, 0, time.UTC))
	writeNewSchemaCPU(t, s, filepath.Join(dir, "cpu_metrics", "2026-06-06.parquet"), time.Date(2026, 6, 6, 1, 0, 0, 0, time.UTC))

	if err := s.unarchiveTable("cpu_metrics", dir); err != nil {
		t.Fatalf("unarchive mixed-schema files: %v", err)
	}
	var total, withSteal int
	if err := s.db.QueryRow(`SELECT COUNT(*), COUNT(steal_pct) FROM cpu_metrics`).Scan(&total, &withSteal); err != nil {
		t.Fatal(err)
	}
	if total != 2 || withSteal != 1 {
		t.Errorf("unarchived rows=%d with steal=%d, want 2 and 1", total, withSteal)
	}
}

func TestSnapshotMixedSchemaArchive(t *testing.T) {
	s := newPruneTestStore(t)
	dir := t.TempDir()
	writeOldSchemaCPU(t, s, filepath.Join(dir, "cpu_metrics", "2026-06-05.parquet"), time.Date(2026, 6, 5, 1, 0, 0, 0, time.UTC))
	writeNewSchemaCPU(t, s, filepath.Join(dir, "cpu_metrics", "2026-06-06.parquet"), time.Date(2026, 6, 6, 1, 0, 0, 0, time.UTC))
	// One live row as well.
	mustExec(t, s.db, `INSERT INTO cpu_metrics (ts, core, user_pct, system_pct, idle_pct, iowait_pct, steal_pct) VALUES (?, -1, 30, 5, 60, 5, 2.5)`, time.Now())

	snap := filepath.Join(t.TempDir(), "snap.duckdb")
	if err := s.Snapshot(snap, dir, false); err != nil {
		t.Fatalf("snapshot over mixed-schema archive: %v", err)
	}
	mustExec(t, s.db, `ATTACH '`+quoteLiteral(snap)+`' AS chk (READ_ONLY)`)
	defer s.db.Exec(`DETACH chk`)
	var total, withSteal int
	if err := s.db.QueryRow(`SELECT COUNT(*), COUNT(steal_pct) FROM chk.cpu_metrics`).Scan(&total, &withSteal); err != nil {
		t.Fatal(err)
	}
	if total != 3 || withSteal != 2 {
		t.Errorf("snapshot rows=%d with steal=%d, want 3 and 2", total, withSteal)
	}
}

// TestArchiveScanFileOrder covers the case plain read_parquet rejects: DuckDB
// takes the schema from the first file, so a list where a newer (wider) file
// precedes an older one fails with "schema mismatch in glob". Selecting a column
// that no file in range has (charting steal_pct over pre-000012 days) also needs
// ArchiveScan, which borrows the live table's full schema.
func TestArchiveScanFileOrder(t *testing.T) {
	s := newPruneTestStore(t)
	dir := t.TempDir()
	oldF := filepath.Join(dir, "cpu_metrics", "2026-06-05.parquet")
	newF := filepath.Join(dir, "cpu_metrics", "2026-06-06.parquet")
	writeOldSchemaCPU(t, s, oldF, time.Date(2026, 6, 5, 1, 0, 0, 0, time.UTC))
	writeNewSchemaCPU(t, s, newF, time.Date(2026, 6, 6, 1, 0, 0, 0, time.UTC))
	newFirst := "['" + quoteLiteral(newF) + "', '" + quoteLiteral(oldF) + "']"

	if _, err := s.db.Exec(`SELECT * FROM read_parquet(` + newFirst + `)`); err == nil {
		t.Log("note: plain read_parquet accepted newer-first order; DuckDB behaviour changed")
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + ArchiveScan("cpu_metrics", newFirst)).Scan(&n); err != nil || n != 2 {
		t.Errorf("ArchiveScan newer-first: n=%d err=%v, want 2 rows", n, err)
	}
	var steal sql.NullFloat64
	if err := s.db.QueryRow(`SELECT MAX(steal_pct) FROM ` + ArchiveScan("cpu_metrics", ParquetLiteral(oldF))).Scan(&steal); err != nil {
		t.Errorf("selecting steal_pct over pre-migration-only files: %v", err)
	} else if steal.Valid {
		t.Errorf("steal_pct over pre-migration file = %v, want NULL", steal.Float64)
	}
}
