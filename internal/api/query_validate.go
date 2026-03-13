package api

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/duckdb/duckdb-go/v2"
)

// readOnlyStmtTypes is the set of DuckDB statement types considered read-only.
var readOnlyStmtTypes = map[duckdb.StmtType]bool{
	duckdb.STATEMENT_TYPE_SELECT:  true,
	duckdb.STATEMENT_TYPE_EXPLAIN: true,
	duckdb.STATEMENT_TYPE_PRAGMA:  true,
}

// checkReadOnly uses DuckDB's parser to verify the SQL is a read-only statement.
// It prepares the statement (triggering a full parse), checks the statement type
// against an allowlist, and returns an error if the statement would modify data.
func checkReadOnly(db *sql.DB, query string) error {
	conn, err := db.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Close()

	var stmtType duckdb.StmtType
	err = conn.Raw(func(driverConn any) error {
		dc := driverConn.(*duckdb.Conn)
		s, prepErr := dc.PrepareContext(context.Background(), query)
		if prepErr != nil {
			return prepErr
		}
		stmt := s.(*duckdb.Stmt)
		defer stmt.Close()

		var typeErr error
		stmtType, typeErr = stmt.StatementType()
		return typeErr
	})
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}

	if !readOnlyStmtTypes[stmtType] {
		return fmt.Errorf("only SELECT, EXPLAIN, and PRAGMA statements are allowed")
	}
	return nil
}
