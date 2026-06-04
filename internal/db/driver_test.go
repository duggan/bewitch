package db

import (
	"database/sql"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
)

// TestLastInsertIdUnsupported documents (and guards) a sharp edge of the DuckDB driver:
// Result.LastInsertId() returns (0, nil) — no error — rather than the inserted row's id.
// Relying on it silently writes 0 where an id is expected (this is exactly how alert rule
// config rows got orphaned at rule_id=0 and rules never fired). Code that needs the new
// id must use `INSERT ... RETURNING id` with QueryRow/Scan instead. If a future driver
// version starts supporting LastInsertId, this test will fail and the guidance can be
// revisited.
func TestLastInsertIdUnsupported(t *testing.T) {
	database, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("opening duckdb: %v", err)
	}
	defer database.Close()

	if _, err := database.Exec(`CREATE SEQUENCE seq START 1`); err != nil {
		t.Fatalf("create sequence: %v", err)
	}
	if _, err := database.Exec(`CREATE TABLE t (id INTEGER DEFAULT nextval('seq'), name VARCHAR)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	res, err := database.Exec(`INSERT INTO t (name) VALUES ('a')`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId returned an error (driver behavior changed?): %v", err)
	}
	if id != 0 {
		t.Fatalf("LastInsertId returned %d — the driver now supports it; the RETURNING-id workaround can be revisited", id)
	}

	// The supported pattern: RETURNING id yields the real, sequence-assigned id.
	var got int64
	if err := database.QueryRow(`INSERT INTO t (name) VALUES ('b') RETURNING id`).Scan(&got); err != nil {
		t.Fatalf("RETURNING id: %v", err)
	}
	if got == 0 {
		t.Errorf("RETURNING id should yield a nonzero id, got %d", got)
	}
}
