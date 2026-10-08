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

// TestCompactThenRestart is the restart half of compaction, which nothing
// covered: compaction used to drop schema_version (and migration 000005's
// unique rule-name index), so the daemon's next db.Open replayed migrations
// onto the already-migrated schema and refused to start ("migration
// 000007_network_drops failed: Column with name rx_dropped already exists",
// then "database is dirty at version 7"). Seen in the field after a first
// post-archive compaction.
func TestCompactThenRestart(t *testing.T) {
	s, dbPath := newCompactTestStore(t)
	var before int
	if err := s.DB().QueryRow(`SELECT version FROM schema_version`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := s.Compact(dbPath); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	var after int
	var dirty bool
	if err := s.DB().QueryRow(`SELECT version, dirty FROM schema_version`).Scan(&after, &dirty); err != nil {
		t.Fatalf("schema_version after compaction: %v", err)
	}
	if after != before || dirty {
		t.Errorf("schema_version after compaction = %d dirty=%v, want %d clean", after, dirty, before)
	}
	if _, err := s.DB().Exec(`INSERT INTO alert_rules (name, type, severity) VALUES ('dup', 'threshold', 'warning')`); err != nil {
		t.Fatalf("inserting rule: %v", err)
	}
	if _, err := s.DB().Exec(`INSERT INTO alert_rules (name, type, severity) VALUES ('dup', 'threshold', 'warning')`); err == nil {
		t.Error("duplicate rule name accepted: unique index idx_alert_rules_name missing after compaction")
	}
	var dups int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM alert_rules WHERE name = 'dup'`).Scan(&dups); err != nil {
		t.Fatal(err)
	}
	if dups != 1 {
		t.Errorf("rule-name unique index missing after compaction: %d rows named 'dup'", dups)
	}

	// Restart: a fresh db.Open must run migrations cleanly against the compacted file.
	s.DB().Close()
	reopened, err := db.Open(dbPath, "", "")
	if err != nil {
		t.Fatalf("reopening compacted DB (daemon restart): %v", err)
	}
	reopened.Close()
}

// TestCompactDoesNotLoseConcurrentWrites: a write made directly on DB() (the
// API's rule/alert/preference writes, the alert engine's fire/resolve) while
// compaction is copying used to land in the file being replaced — the table it
// targets was already copied — and vanished at the swap. Writers go through
// AcquireWrite, which compaction holds exclusively until the swap, so the write
// waits and lands in the new database.
func TestCompactDoesNotLoseConcurrentWrites(t *testing.T) {
	s, dbPath := newCompactTestStore(t)
	done := make(chan error, 1)
	var once sync.Once
	compactCopyHook = func(table string) {
		if table != "alerts" {
			return
		}
		once.Do(func() {
			go func() {
				release := s.AcquireWrite()
				defer release()
				_, err := s.DB().Exec(`INSERT INTO alerts (ts, rule_name, severity, message) VALUES (?, 'r', 'warning', 'mid-compaction')`, time.Now())
				done <- err
			}()
			// Give the writer time to attempt its write while alerts is already copied.
			time.Sleep(200 * time.Millisecond)
		})
	}
	t.Cleanup(func() { compactCopyHook = nil })

	if err := s.Compact(dbPath); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("concurrent write: %v", err)
	}
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM alerts WHERE message = 'mid-compaction'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("alert written during compaction: %d rows after the swap, want 1 (lost to the replaced file)", n)
	}
}
