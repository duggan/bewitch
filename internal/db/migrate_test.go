package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatalf("opening duckdb: %v", err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestFreshDB(t *testing.T) {
	db := openTestDB(t)

	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	// Verify schema_version is at latest version.
	var version int
	var dirty bool
	if err := db.QueryRow(`SELECT version, dirty FROM schema_version`).Scan(&version, &dirty); err != nil {
		t.Fatalf("reading schema_version: %v", err)
	}
	if version != 5 {
		t.Errorf("version = %d, want 5", version)
	}
	if dirty {
		t.Error("dirty = true, want false")
	}

	// Verify key tables were created.
	for _, table := range []string{"cpu_metrics", "memory_metrics", "disk_metrics", "alert_rules", "preferences"} {
		exists, err := tableExists(db, table)
		if err != nil {
			t.Fatalf("tableExists(%s): %v", table, err)
		}
		if !exists {
			t.Errorf("table %s not created", table)
		}
	}
}

func TestExistingDBDetection(t *testing.T) {
	db := openTestDB(t)

	// Simulate a pre-migration database by creating cpu_metrics directly.
	if _, err := db.Exec(`CREATE TABLE cpu_metrics (ts TIMESTAMP NOT NULL, core TINYINT NOT NULL)`); err != nil {
		t.Fatalf("creating cpu_metrics: %v", err)
	}

	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	var version int
	var dirty bool
	if err := db.QueryRow(`SELECT version, dirty FROM schema_version`).Scan(&version, &dirty); err != nil {
		t.Fatalf("reading schema_version: %v", err)
	}
	// Should be stamped at 1 (skipped initial), then ran migrations 2 and 3.
	if version != 5 {
		t.Errorf("version = %d, want 5", version)
	}
	if dirty {
		t.Error("dirty = true, want false")
	}
}

func TestDirtyDBRefuses(t *testing.T) {
	db := openTestDB(t)

	// Create schema_version in dirty state.
	if _, err := db.Exec(`CREATE TABLE schema_version (
		version INTEGER NOT NULL,
		dirty BOOLEAN NOT NULL DEFAULT false,
		applied_at TIMESTAMP NOT NULL DEFAULT current_timestamp
	)`); err != nil {
		t.Fatalf("creating schema_version: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO schema_version (version, dirty) VALUES (1, true)`); err != nil {
		t.Fatalf("inserting dirty version: %v", err)
	}

	err := runMigrations(db)
	if err == nil {
		t.Fatal("expected error for dirty database, got nil")
	}
}

func TestIdempotentRestart(t *testing.T) {
	db := openTestDB(t)

	if err := runMigrations(db); err != nil {
		t.Fatalf("first runMigrations: %v", err)
	}

	// Run again — should be a no-op.
	if err := runMigrations(db); err != nil {
		t.Fatalf("second runMigrations: %v", err)
	}

	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("reading version: %v", err)
	}
	if version != 5 {
		t.Errorf("version = %d, want 5", version)
	}
}

// TestMigrateUniqueRuleNames covers the dedup-then-unique-index repair for alert rule
// names. It builds a minimal alert_rules table with a duplicate (as a pre-migration DB
// would have), runs the migration directly, and asserts names are deduped, the lowest id
// keeps the original name, the unique index now rejects duplicates, and a re-run is a
// no-op.
func TestMigrateUniqueRuleNames(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec(`CREATE TABLE alert_rules (id INTEGER, name VARCHAR, type VARCHAR, severity VARCHAR)`); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO alert_rules VALUES
		(1,'dup','threshold','warning'),
		(2,'dup','threshold','warning'),
		(3,'uniq','threshold','warning')`); err != nil {
		t.Fatalf("seeding rows: %v", err)
	}

	if err := migrateUniqueRuleNames(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var distinct, total int
	db.QueryRow(`SELECT count(DISTINCT name), count(*) FROM alert_rules`).Scan(&distinct, &total)
	if total != 3 || distinct != 3 {
		t.Errorf("expected 3 distinct names, got distinct=%d total=%d", distinct, total)
	}
	var n1, n2 string
	db.QueryRow(`SELECT name FROM alert_rules WHERE id = 1`).Scan(&n1)
	db.QueryRow(`SELECT name FROM alert_rules WHERE id = 2`).Scan(&n2)
	if n1 != "dup" || n2 == "dup" {
		t.Errorf("expected id1 to keep 'dup' and id2 renamed, got id1=%q id2=%q", n1, n2)
	}

	// The unique index must now reject a duplicate-name insert.
	if _, err := db.Exec(`INSERT INTO alert_rules VALUES (4,'uniq','threshold','warning')`); err == nil {
		t.Error("expected unique index to reject duplicate name insert")
	}

	// Re-running is a no-op (no error, index already present).
	if err := migrateUniqueRuleNames(db); err != nil {
		t.Fatalf("second run should be a no-op: %v", err)
	}
}
