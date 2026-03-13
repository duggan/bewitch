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

		// Comment-based bypass attempts (the key advantage over keyword matching)
		{"comment then insert", "-- harmless\nINSERT INTO test_table VALUES (1, 'a')", true},
		{"cte wrapping insert", "WITH cte AS (SELECT 1) INSERT INTO test_table SELECT * FROM cte", true},
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
