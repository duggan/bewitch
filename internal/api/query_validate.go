package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/duckdb/duckdb-go/v2"
)

// readOnlyStmtTypes is the set of DuckDB statement types considered read-only.
var readOnlyStmtTypes = map[duckdb.StmtType]bool{
	duckdb.STATEMENT_TYPE_SELECT:  true,
	duckdb.STATEMENT_TYPE_EXPLAIN: true,
	duckdb.STATEMENT_TYPE_PRAGMA:  true,
}

// errMultiStatement is returned when a query contains more than one SQL statement.
var errMultiStatement = errors.New("only a single read-only statement is allowed; multiple statements are not permitted")

// checkReadOnly uses DuckDB's parser to verify the SQL is a *single* read-only
// statement before the caller executes it. Both checks matter:
//
//   - Single-statement: this is a security boundary, not cosmetic. The driver's
//     PrepareContext path executes every statement except the last while preparing,
//     so validating "DELETE FROM cpu_metrics; SELECT 1" would run the DELETE during
//     the check itself and then report the trailing SELECT's type as read-only. The
//     non-context Prepare instead refuses any query that doesn't extract to exactly
//     one statement *before* preparing or executing anything, closing that bypass.
//   - Read-only: the surviving single statement's type must be in the allowlist
//     (SELECT/EXPLAIN/PRAGMA), rejecting DELETE/DROP/COPY/ATTACH/etc.
func checkReadOnly(db *sql.DB, query string) error {
	conn, err := db.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Close()

	var stmtType duckdb.StmtType
	err = conn.Raw(func(driverConn any) error {
		dc := driverConn.(*duckdb.Conn)
		// Prepare (no context) extracts statements and refuses count != 1 before
		// preparing or executing — unlike PrepareContext, which executes all but the
		// last statement during preparation (the read-only bypass we're closing).
		s, prepErr := dc.Prepare(query)
		if prepErr != nil {
			// The driver's multi-statement rejection is the common case here; map it
			// to a clear message. Note the security property does not depend on this
			// string match — any prepare failure still rejects the query.
			if strings.Contains(prepErr.Error(), "multi-statement") {
				return errMultiStatement
			}
			return prepErr
		}
		stmt := s.(*duckdb.Stmt)
		defer stmt.Close()

		var typeErr error
		stmtType, typeErr = stmt.StatementType()
		return typeErr
	})
	if err != nil {
		if errors.Is(err, errMultiStatement) {
			return err
		}
		return fmt.Errorf("preparing statement: %w", err)
	}

	if !readOnlyStmtTypes[stmtType] {
		return fmt.Errorf("only SELECT, EXPLAIN, and PRAGMA statements are allowed")
	}
	return nil
}
