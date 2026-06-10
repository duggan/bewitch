package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/duckdb/duckdb-go/v2"
)

// readOnlyStmtTypes is the set of DuckDB statement types considered read-only.
var readOnlyStmtTypes = map[duckdb.StmtType]bool{
	duckdb.STATEMENT_TYPE_SELECT:  true,
	duckdb.STATEMENT_TYPE_EXPLAIN: true,
	duckdb.STATEMENT_TYPE_PRAGMA:  true,
}

// blockedFuncPattern matches DuckDB table/scalar functions that read from the
// filesystem or reach the network. DuckDB classifies a SELECT that calls them as
// read-only, so the statement-type allowlist alone does not stop them — yet they
// let a caller exfiltrate arbitrary files the daemon can read (read_text /
// read_blob / read_csv / read_parquet / glob), or perform SSRF to internal HTTP
// endpoints via the auto-loaded httpfs extension (read_csv('http://…')). The
// ad-hoc query/export endpoints are reachable unauthenticated on the local socket
// and the daemon holds CAP_SYS_RAWIO + the disk group, so file reads are
// especially dangerous (e.g. reading raw block devices or tls-key.pem). We bound
// the *functions* a read-only statement may call in addition to its type.
//
// Matched as whole identifiers, case-insensitively, so quoted/whitespaced call
// forms are still caught. bewitch's fixed schema has no tables or columns named
// like these, so legitimate queries are unaffected; archived Parquet is served by
// the history API, which builds its own queries server-side and does not pass
// through this check.
var blockedFuncPattern = regexp.MustCompile(`(?i)\b(read_text|read_blob|read_csv|read_csv_auto|read_json|read_json_auto|read_json_objects|read_ndjson|read_ndjson_auto|read_ndjson_objects|read_parquet|parquet_scan|read_xlsx|st_read|glob|sniff_csv|parquet_metadata|parquet_schema|parquet_file_metadata|parquet_kv_metadata|parquet_bloom_probe|delta_scan|iceberg_scan|iceberg_metadata|iceberg_snapshots|postgres_scan|postgres_query|mysql_scan|mysql_query|sqlite_scan|sqlite_query)\b`)

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
	// Bound the functions a read-only statement may call before touching the DB:
	// reject filesystem/network table functions that would otherwise let a SELECT
	// read arbitrary files or perform SSRF (see blockedFuncPattern).
	if m := blockedFuncPattern.FindString(query); m != "" {
		return fmt.Errorf("function %q is not allowed: filesystem and network access are disabled for ad-hoc queries", strings.ToLower(strings.TrimSpace(m)))
	}

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
