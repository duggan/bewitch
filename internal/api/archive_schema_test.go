package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/db"
)

// Archived Parquet written before a migration added columns (cpu_metrics.steal_pct
// in 000012, process_metrics I/O rates in 000013/000014) must stay readable next
// to newer files and the live table. Without name-based matching the all_* views
// failed to create (positional UNION ALL). History reads over date-sorted files
// happened to work (DuckDB takes the first, oldest file's schema); they're kept
// here as coverage — store.TestArchiveScanFileOrder covers the orderings and
// column selections that plain read_parquet rejects.

func newArchiveSchemaServer(t *testing.T) (*Server, *sql.DB, string) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "a.duckdb"), "", "")
	if err != nil {
		t.Fatalf("opening migrated db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	archive := filepath.Join(t.TempDir(), "archive")
	s := &Server{
		dbFn:             func() *sql.DB { return database },
		historyCache:     map[string]*historyCacheEntry{},
		archivePath:      archive,
		archiveThreshold: 24 * time.Hour,
	}
	return s, database, archive
}

func exec(t *testing.T, d *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := d.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// archiveRows inserts via insertSQL, copies the table to path (dropping the
// excluded columns to mimic a pre-migration file), then empties the table.
func archiveRows(t *testing.T, d *sql.DB, table, path, exclude, insertSQL string, args ...any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	exec(t, d, insertSQL, args...)
	sel := "*"
	if exclude != "" {
		sel = "* EXCLUDE (" + exclude + ")"
	}
	exec(t, d, fmt.Sprintf(`COPY (SELECT %s FROM %s) TO '%s' (FORMAT parquet)`, sel, table, path))
	exec(t, d, "DELETE FROM "+table)
}

func historyPoints(t *testing.T, s *Server, path string, start, end time.Time) int {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", fmt.Sprintf("%s?start=%d&end=%d", path, start.Unix(), end.Unix()), nil)
	switch {
	case strings.HasSuffix(path, "/cpu"):
		s.handleHistoryCPU(rec, req)
	case strings.HasSuffix(path, "/process"):
		s.handleHistoryProcess(rec, req)
	}
	if rec.Code != 200 {
		t.Fatalf("%s: status %d: %s", path, rec.Code, rec.Body.String())
	}
	var resp struct {
		Series []TimeSeries `json:"series"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("%s: decoding %q: %v", path, rec.Body.String(), err)
	}
	n := 0
	for _, ser := range resp.Series {
		n += len(ser.Points)
	}
	return n
}

func TestArchiveSchemaDriftHistoryAndViews(t *testing.T) {
	s, d, archive := newArchiveSchemaServer(t)
	now := time.Now().UTC()
	oldDay := now.AddDate(0, 0, -10).Truncate(24 * time.Hour)
	newDay := now.AddDate(0, 0, -9).Truncate(24 * time.Hour)
	file := func(table string, day time.Time) string {
		return filepath.Join(archive, table, day.Format("2006-01-02")+".parquet")
	}

	cpuIns := `INSERT INTO cpu_metrics (ts, core, user_pct, system_pct, idle_pct, iowait_pct) VALUES (?, -1, 10, 5, 80, 5)`
	archiveRows(t, d, "cpu_metrics", file("cpu_metrics", oldDay), "steal_pct", cpuIns, oldDay.Add(time.Hour))
	archiveRows(t, d, "cpu_metrics", file("cpu_metrics", newDay), "", cpuIns, newDay.Add(time.Hour))

	procIns := `INSERT INTO process_metrics (ts, pid, start_time, cpu_user_pct, cpu_system_pct) VALUES (?, 42, 1, 3, 1)`
	archiveRows(t, d, "process_metrics", file("process_metrics", oldDay), "read_bytes_sec, write_bytes_sec, net_rx_bytes_sec, net_tx_bytes_sec", procIns, oldDay.Add(time.Hour))
	archiveRows(t, d, "process_metrics", file("process_metrics", newDay), "", procIns, newDay.Add(time.Hour))
	archiveRows(t, d, "process_info", filepath.Join(archive, "process_info.parquet"), "",
		`INSERT INTO process_info (pid, start_time, name, first_seen) VALUES (42, 1, 'worker', ?)`, oldDay)

	// A live row too, so the "both" (DuckDB + Parquet) path is exercised.
	exec(t, d, `INSERT INTO cpu_metrics (ts, core, user_pct, system_pct, idle_pct, iowait_pct, steal_pct) VALUES (?, -1, 30, 5, 60, 5, 2)`, now.Add(-time.Hour))

	t.Run("parquet-only cpu history spans old and new files", func(t *testing.T) {
		if n := historyPoints(t, s, "/api/history/cpu", oldDay, newDay.Add(23*time.Hour)); n == 0 {
			t.Error("expected points from both archived days")
		}
	})
	t.Run("both-source cpu history", func(t *testing.T) {
		if n := historyPoints(t, s, "/api/history/cpu", oldDay, now); n == 0 {
			t.Error("expected points")
		}
	})
	t.Run("parquet-only process history over a mixed file list", func(t *testing.T) {
		if n := historyPoints(t, s, "/api/history/process", oldDay, newDay.Add(23*time.Hour)); n == 0 {
			t.Error("expected points for pid 42")
		}
	})
	t.Run("all_* views create and expose new columns", func(t *testing.T) {
		s.CreateArchiveViews()
		var total, withSteal int
		if err := d.QueryRow(`SELECT COUNT(*), COUNT(steal_pct) FROM all_cpu_metrics`).Scan(&total, &withSteal); err != nil {
			t.Fatalf("all_cpu_metrics: %v", err)
		}
		if total != 3 || withSteal != 1 {
			t.Errorf("all_cpu_metrics rows=%d with steal=%d, want 3 and 1", total, withSteal)
		}
		var procRows int
		if err := d.QueryRow(`SELECT COUNT(*) FROM all_process_metrics WHERE read_bytes_sec IS NULL`).Scan(&procRows); err != nil {
			t.Fatalf("all_process_metrics: %v", err)
		}
		if procRows != 2 {
			t.Errorf("all_process_metrics rows=%d, want 2", procRows)
		}
	})
}
