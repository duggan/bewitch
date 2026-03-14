package db

import (
	"database/sql"
	"fmt"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
)

func TestCreateTablesIn(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Run migrations to create the main schema
	if err := runMigrations(db); err != nil {
		t.Fatalf("running migrations: %v", err)
	}

	// Attach a second database
	if _, err := db.Exec("ATTACH ':memory:' AS test_db"); err != nil {
		t.Fatalf("attach: %v", err)
	}

	// Create sequences first (needed for DEFAULT expressions)
	if err := CreateSequencesIn(db, "test_db"); err != nil {
		t.Fatalf("CreateSequencesIn: %v", err)
	}

	// Create all tables
	tables := AllTables()
	if err := CreateTablesIn(db, "test_db", tables); err != nil {
		t.Fatalf("CreateTablesIn: %v", err)
	}

	// Verify all tables exist and column counts match.
	for _, table := range tables {
		var mainCols, testCols int
		if err := db.QueryRow(`SELECT COUNT(*) FROM duckdb_columns()
			WHERE database_name = 'memory' AND schema_name = 'main' AND table_name = $1`, table).Scan(&mainCols); err != nil {
			t.Fatalf("counting main.%s columns: %v", table, err)
		}
		if err := db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM duckdb_columns()
			WHERE database_name = 'test_db' AND table_name = '%s'`, table)).Scan(&testCols); err != nil {
			t.Fatalf("counting test_db.%s columns: %v", table, err)
		}
		if mainCols != testCols {
			t.Errorf("%s: main has %d columns, test_db has %d", table, mainCols, testCols)
		}
	}
}

func TestCreateTablesInSequenceDefaults(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := runMigrations(db); err != nil {
		t.Fatalf("running migrations: %v", err)
	}

	if _, err := db.Exec("ATTACH ':memory:' AS test_db"); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := CreateSequencesIn(db, "test_db"); err != nil {
		t.Fatalf("CreateSequencesIn: %v", err)
	}
	if err := CreateTablesIn(db, "test_db", []string{"alerts"}); err != nil {
		t.Fatalf("CreateTablesIn: %v", err)
	}

	// Insert a row — the DEFAULT nextval should resolve to test_db.alert_id_seq
	if _, err := db.Exec(`INSERT INTO test_db.alerts (ts, rule_name, severity, message)
		VALUES (current_timestamp, 'test', 'info', 'hello')`); err != nil {
		t.Fatalf("insert into test_db.alerts: %v", err)
	}

	var id int
	if err := db.QueryRow("SELECT id FROM test_db.alerts").Scan(&id); err != nil {
		t.Fatalf("reading id: %v", err)
	}
	if id < 1 {
		t.Errorf("expected auto-generated id >= 1, got %d", id)
	}
}
