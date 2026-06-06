package store

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/db"
)

func newCompactTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "compact.duckdb")
	database, err := db.Open(dbPath, "", "")
	if err != nil {
		t.Fatalf("opening migrated db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return New(database), dbPath
}

// TestCompactPreservesDataAndStaysUsable verifies the file swap: data survives,
// and the store's *sql.DB is the fresh handle and works after compaction.
func TestCompactPreservesDataAndStaysUsable(t *testing.T) {
	s, dbPath := newCompactTestStore(t)

	for i := 0; i < 50; i++ {
		if _, err := s.DB().Exec("INSERT INTO load_metrics (ts, load1, load5, load15) VALUES (?, ?, ?, ?)",
			time.Unix(int64(i), 0), float64(i), 0.0, 0.0); err != nil {
			t.Fatalf("seeding row %d: %v", i, err)
		}
	}

	if err := s.Compact(dbPath); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	var n int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM load_metrics").Scan(&n); err != nil {
		t.Fatalf("query after compaction (store unusable?): %v", err)
	}
	if n != 50 {
		t.Errorf("row count after compaction = %d, want 50", n)
	}
}

// TestCompactConcurrentReaders exercises the swap against concurrent DB()
// readers. Under `go test -race` this catches an unsynchronized s.db reassign
// racing DB()'s read.
func TestCompactConcurrentReaders(t *testing.T) {
	s, dbPath := newCompactTestStore(t)
	if _, err := s.DB().Exec("INSERT INTO load_metrics (ts, load1, load5, load15) VALUES (?, 1, 1, 1)", time.Unix(0, 0)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// Grab the handle the way the API/alert read paths do. Queries may
				// fail transiently during the brief close window — that's fine; the
				// race detector is what we care about here.
				dbh := s.DB()
				var n int
				_ = dbh.QueryRow("SELECT COUNT(*) FROM load_metrics").Scan(&n)
			}
		}()
	}

	for i := 0; i < 3; i++ {
		if err := s.Compact(dbPath); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("Compact iteration %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()
}
