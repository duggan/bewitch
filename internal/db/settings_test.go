package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// assertSettings checks threads and memory_limit on every pooled connection at
// once (holding them open forces distinct connections), since a SET that only
// reached the connection it ran on would pass a single-connection check.
func assertSettings(t *testing.T, database *sql.DB, wantThreads int64, wantMemLimit string) {
	t.Helper()
	ctx := context.Background()
	var conns []*sql.Conn
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for i := 0; i < 4; i++ {
		c, err := database.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		var threads int64
		var memLimit string
		if err := c.QueryRowContext(ctx, `SELECT current_setting('threads'), current_setting('memory_limit')`).Scan(&threads, &memLimit); err != nil {
			t.Fatal(err)
		}
		if threads != wantThreads {
			t.Errorf("connection %d: threads = %d, want %d", i, threads, wantThreads)
		}
		if memLimit != wantMemLimit {
			t.Errorf("connection %d: memory_limit = %q, want %q", i, memLimit, wantMemLimit)
		}
	}
}

func TestOpenAndReopenApplySettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.duckdb")
	opts := Options{MemoryLimit: "256MiB", Threads: 2}

	database, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	assertSettings(t, database, 2, "256.0 MiB")
	database.Close()

	// Reopen (used after compaction swaps the file) must re-apply them too.
	database, err = Reopen(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	assertSettings(t, database, 2, "256.0 MiB")
}
