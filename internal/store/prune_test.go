package store

import (
	"path/filepath"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/duggan/bewitch/internal/db"
)

func newPruneTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "prune.duckdb")
	database, err := db.Open(dbPath, "", "")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return New(database)
}

// TestPruneShrinksProcInfoCache guards against the unbounded growth of
// procInfoCache: Prune deletes orphaned process_info rows from the DB, and the
// in-memory cache must be resynced to match so it doesn't grow with uptime.
func TestPruneShrinksProcInfoCache(t *testing.T) {
	s := newPruneTestStore(t)

	now := time.Now()
	recent := now.Add(-1 * time.Minute) // survives a 1h retention
	old := now.Add(-2 * time.Hour)      // pruned by a 1h retention

	// Four processes have process_info rows.
	for pid := 1; pid <= 4; pid++ {
		if _, err := s.db.Exec(
			`INSERT INTO process_info (pid, start_time, name, first_seen) VALUES (?, ?, ?, ?)`,
			pid, int64(pid*1000), "proc", old); err != nil {
			t.Fatalf("insert process_info: %v", err)
		}
	}

	// pids 1,2 have recent metrics (kept); pids 3,4 only have old metrics (pruned).
	insertMetric := func(pid int, ts time.Time) {
		if _, err := s.db.Exec(
			`INSERT INTO process_metrics (ts, pid, start_time, cpu_user_pct) VALUES (?, ?, ?, ?)`,
			ts, pid, int64(pid*1000), 1.0); err != nil {
			t.Fatalf("insert process_metrics: %v", err)
		}
	}
	insertMetric(1, recent)
	insertMetric(2, recent)
	insertMetric(3, old)
	insertMetric(4, old)

	// Populate the cache from the current DB state (simulates accumulation).
	s.procInfoCacheMu.Lock()
	s.loadProcessInfoCache()
	s.procInfoCacheMu.Unlock()

	if got := len(s.procInfoCache); got != 4 {
		t.Fatalf("precondition: cache size = %d, want 4", got)
	}

	if err := s.Prune(time.Hour); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	// pids 3,4 were orphaned and deleted from process_info, so the cache must
	// have shrunk to match the surviving rows (pids 1,2).
	s.procInfoCacheMu.RLock()
	defer s.procInfoCacheMu.RUnlock()
	if got := len(s.procInfoCache); got != 2 {
		t.Errorf("cache size after prune = %d, want 2 (orphaned entries should be evicted)", got)
	}
	for pid := 1; pid <= 2; pid++ {
		if _, ok := s.procInfoCache[processKey{pid: int32(pid), startTime: int64(pid * 1000)}]; !ok {
			t.Errorf("pid %d missing from cache after prune", pid)
		}
	}
	for pid := 3; pid <= 4; pid++ {
		if _, ok := s.procInfoCache[processKey{pid: int32(pid), startTime: int64(pid * 1000)}]; ok {
			t.Errorf("pid %d should have been evicted from cache after prune", pid)
		}
	}
}
