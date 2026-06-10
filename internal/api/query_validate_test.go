package api

import (
	"database/sql"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("opening in-memory DuckDB: %v", err)
	}
	// Create a test table so table-referencing queries can parse
	if _, err := db.Exec("CREATE TABLE test_table (id INT, name VARCHAR)"); err != nil {
		t.Fatalf("creating test table: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCheckReadOnly(t *testing.T) {
	db := testDB(t)

	tests := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		// Allowed
		{"select literal", "SELECT 1", false},
		{"select from table", "SELECT * FROM test_table", false},
		{"select with where", "SELECT * FROM test_table WHERE id > 0", false},
		{"with cte", "WITH cte AS (SELECT 1) SELECT * FROM cte", false},
		{"explain", "EXPLAIN SELECT * FROM test_table", false},
		{"pragma", "PRAGMA version", false},
		{"select case insensitive", "select 1", false},
		{"select with comments", "-- comment\nSELECT 1", false},
		{"select with block comment", "/* block */ SELECT 1", false},
		{"trailing semicolon", "SELECT 1;", false},
		{"trailing semicolon whitespace", "SELECT * FROM test_table; ", false},

		// Rejected
		{"insert", "INSERT INTO test_table VALUES (1, 'a')", true},
		{"delete", "DELETE FROM test_table", true},
		{"update", "UPDATE test_table SET name = 'b'", true},
		{"drop", "DROP TABLE test_table", true},
		{"create", "CREATE TABLE new_table (id INT)", true},
		{"alter", "ALTER TABLE test_table ADD COLUMN x INT", true},
		{"copy to", "COPY test_table TO '/tmp/out.csv'", true},
		{"attach", "ATTACH ':memory:' AS mem", true},
		{"set", "SET memory_limit = '2GB'", true},

		// File/network functions: classified as SELECT by DuckDB but rejected so a
		// read-only query can't read arbitrary files or perform SSRF.
		{"read_text file", "SELECT content FROM read_text('/etc/passwd')", true},
		{"read_blob device", "SELECT * FROM read_blob('/dev/sda')", true},
		{"read_csv ssrf", "SELECT * FROM read_csv('http://169.254.169.254/latest/meta-data/')", true},
		{"read_parquet path", "SELECT * FROM read_parquet('/var/lib/bewitch/tls-key.pem')", true},
		{"glob fs", "SELECT * FROM glob('/etc/*')", true},
		{"read_text case/space", "select * from READ_TEXT ('/etc/hosts')", true},
		{"read_json", "SELECT * FROM read_json_auto('/etc/passwd')", true},
		{"sniff_csv", "SELECT * FROM sniff_csv('/etc/passwd')", true},

		// Comment-based bypass attempts (the key advantage over keyword matching)
		{"comment then insert", "-- harmless\nINSERT INTO test_table VALUES (1, 'a')", true},
		{"cte wrapping insert", "WITH cte AS (SELECT 1) INSERT INTO test_table SELECT * FROM cte", true},

		// Multi-statement bypass attempts: a destructive leading statement with a
		// trailing SELECT used to pass (the driver reported only the last type).
		{"destructive then select", "DELETE FROM test_table; SELECT 1", true},
		{"drop then select", "DROP TABLE test_table; SELECT 1", true},
		{"select then destructive", "SELECT 1; DELETE FROM test_table", true},
		{"two selects", "SELECT 1; SELECT 2", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkReadOnly(db, tt.sql)
			if tt.wantErr && err == nil {
				t.Errorf("checkReadOnly(%q) = nil, want error", tt.sql)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("checkReadOnly(%q) = %v, want nil", tt.sql, err)
			}
		})
	}
}

// TestCheckReadOnlyDoesNotExecute is the security regression for the multi-statement
// bypass: validating "DELETE …; SELECT 1" must reject it WITHOUT running the DELETE.
// (The driver's PrepareContext path executes all but the last statement during the
// check itself, so the row would vanish before the query was ever rejected.)
func TestCheckReadOnlyDoesNotExecute(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec("INSERT INTO test_table VALUES (1, 'keep')"); err != nil {
		t.Fatalf("seeding row: %v", err)
	}

	if err := checkReadOnly(db, "DELETE FROM test_table; SELECT 1"); err == nil {
		t.Fatal("checkReadOnly accepted a multi-statement query, want rejection")
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM test_table").Scan(&count); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count = %d after rejected DELETE, want 1 (the DELETE executed during validation)", count)
	}
}
