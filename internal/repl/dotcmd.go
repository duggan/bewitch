package repl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/duggan/bewitch/internal/api"
)

func (r *REPL) handleDotCommand(input string) {
	parts := strings.Fields(input)
	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case ".tables":
		r.dotTables()
	case ".schema":
		if len(args) > 0 {
			r.dotDescribe(args[0])
		} else {
			r.dotSchemaAll()
		}
	case ".columns":
		if len(args) > 0 {
			r.dotDescribe(args[0])
		} else {
			fmt.Fprintln(r.out(), "Usage: .columns <table>")
		}
	case ".count":
		if len(args) > 0 {
			r.dotCount(args[0])
		} else {
			r.dotCountAll()
		}
	case ".metrics":
		r.dotMetrics()
	case ".export":
		// Use raw input to preserve parenthesized SQL
		rest := strings.TrimSpace(input[len(parts[0]):])
		r.dotExport(rest)
	case ".dimensions":
		r.dotDimensions()
	case ".help":
		r.dotHelp()
	case ".quit", ".exit":
		fmt.Fprintln(r.out(), "Bye!")
		os.Exit(0)
	default:
		fmt.Fprintf(r.out(), "Unknown command: %s (try .help)\n", cmd)
	}
}

func (r *REPL) dotTables() {
	qr, err := r.query(`SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'main' ORDER BY table_name`)
	if err != nil {
		fmt.Fprintf(r.out(), "Error: %v\n", err)
		return
	}
	if qr.Error != "" {
		fmt.Fprintf(r.out(), "Error: %s\n", qr.Error)
		return
	}

	var tables []string
	for _, row := range qr.Rows {
		if len(row) > 0 {
			tables = append(tables, fmt.Sprintf("%v", row[0]))
		}
	}

	if len(tables) == 0 {
		fmt.Fprintln(r.out(), "No tables found.")
		return
	}

	nameWidth := 0
	for _, t := range tables {
		if len(t) > nameWidth {
			nameWidth = len(t)
		}
	}

	fmt.Fprintln(r.out())
	for _, t := range tables {
		cqr, err := r.query(fmt.Sprintf("SELECT COUNT(*) FROM %s", t))
		if err != nil || cqr.Error != "" {
			fmt.Fprintf(r.out(), " %-*s  (error)\n", nameWidth, t)
			continue
		}
		count := "0"
		if len(cqr.Rows) > 0 && len(cqr.Rows[0]) > 0 {
			count = formatValue(cqr.Rows[0][0])
		}
		fmt.Fprintf(r.out(), " %-*s  %s rows\n", nameWidth, t, count)
	}
	fmt.Fprintln(r.out())
}

func (r *REPL) dotSchemaAll() {
	qr, err := r.query(`SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'main' ORDER BY table_name`)
	if err != nil {
		fmt.Fprintf(r.out(), "Error: %v\n", err)
		return
	}
	if qr.Error != "" {
		fmt.Fprintf(r.out(), "Error: %s\n", qr.Error)
		return
	}

	for _, row := range qr.Rows {
		if len(row) > 0 {
			t := fmt.Sprintf("%v", row[0])
			fmt.Fprintf(r.out(), "\n-- %s\n", t)
			r.dotDescribe(t)
		}
	}
}

func (r *REPL) dotDescribe(table string) {
	qr, err := r.query(fmt.Sprintf("DESCRIBE %s", table))
	if err != nil {
		fmt.Fprintf(r.out(), "Error: %v\n", err)
		return
	}
	if qr.Error != "" {
		fmt.Fprintf(r.out(), "Error: %s\n", qr.Error)
		return
	}
	fmt.Fprint(r.out(), formatQueryResponse(qr))
}

func (r *REPL) dotCount(table string) {
	qr, err := r.query(fmt.Sprintf("SELECT COUNT(*) FROM %s", table))
	if err != nil {
		fmt.Fprintf(r.out(), "Error: %v\n", err)
		return
	}
	if qr.Error != "" {
		fmt.Fprintf(r.out(), "Error: %s\n", qr.Error)
		return
	}
	count := "0"
	if len(qr.Rows) > 0 && len(qr.Rows[0]) > 0 {
		count = formatValue(qr.Rows[0][0])
	}
	fmt.Fprintf(r.out(), "%s rows\n", count)
}

func (r *REPL) dotCountAll() {
	metricTables := []string{
		"cpu_metrics", "memory_metrics", "disk_metrics", "network_metrics",
		"temperature_metrics", "power_metrics", "ecc_metrics", "process_metrics",
	}

	nameWidth := 0
	for _, t := range metricTables {
		if len(t) > nameWidth {
			nameWidth = len(t)
		}
	}

	fmt.Fprintln(r.out())
	for _, t := range metricTables {
		qr, err := r.query(fmt.Sprintf(
			"SELECT COUNT(*), strftime(MIN(ts), '%%Y-%%m-%%d %%H:%%M:%%S'), strftime(MAX(ts), '%%Y-%%m-%%d %%H:%%M:%%S') FROM %s", t))
		if err != nil || qr.Error != "" {
			fmt.Fprintf(r.out(), " %-*s  (error)\n", nameWidth, t)
			continue
		}
		if len(qr.Rows) == 0 || len(qr.Rows[0]) < 3 {
			fmt.Fprintf(r.out(), " %-*s  0 rows\n", nameWidth, t)
			continue
		}
		row := qr.Rows[0]
		count := formatValue(row[0])
		if count == "0" || row[1] == nil {
			fmt.Fprintf(r.out(), " %-*s  0 rows\n", nameWidth, t)
		} else {
			fmt.Fprintf(r.out(), " %-*s  %s rows  (%v to %v)\n", nameWidth, t, count, row[1], row[2])
		}
	}
	fmt.Fprintln(r.out())
}

func (r *REPL) dotMetrics() {
	metricTables := []string{
		"cpu_metrics", "memory_metrics", "disk_metrics", "network_metrics",
		"temperature_metrics", "power_metrics", "ecc_metrics", "process_metrics",
	}

	// Check which all_* views exist
	views := make(map[string]bool)
	qr, err := r.query(`SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'main' AND table_name LIKE 'all_%'`)
	if err == nil && qr.Error == "" {
		for _, row := range qr.Rows {
			if len(row) > 0 {
				views[fmt.Sprintf("%v", row[0])] = true
			}
		}
	}

	// Build display list: prefer all_* view name when available, else base table
	type entry struct {
		display string // name shown to user (and queryable)
		source  string // table/view to query for stats
	}
	var entries []entry
	for _, t := range metricTables {
		viewName := "all_" + t
		if views[viewName] {
			entries = append(entries, entry{display: viewName, source: viewName})
		} else {
			entries = append(entries, entry{display: t, source: t})
		}
	}

	nameWidth := 0
	for _, e := range entries {
		if len(e.display) > nameWidth {
			nameWidth = len(e.display)
		}
	}

	fmt.Fprintln(r.out())
	for _, e := range entries {
		cqr, err := r.query(fmt.Sprintf(
			"SELECT COUNT(*), strftime(MIN(ts), '%%Y-%%m-%%d %%H:%%M:%%S'), strftime(MAX(ts), '%%Y-%%m-%%d %%H:%%M:%%S') FROM %s", e.source))
		if err != nil || cqr.Error != "" {
			fmt.Fprintf(r.out(), " %-*s  (error)\n", nameWidth, e.display)
			continue
		}
		if len(cqr.Rows) == 0 || len(cqr.Rows[0]) < 3 {
			fmt.Fprintf(r.out(), " %-*s  0 rows\n", nameWidth, e.display)
			continue
		}
		row := cqr.Rows[0]
		count := formatValue(row[0])
		if count == "0" || row[1] == nil {
			fmt.Fprintf(r.out(), " %-*s  0 rows\n", nameWidth, e.display)
		} else {
			fmt.Fprintf(r.out(), " %-*s  %s rows  (%v to %v)\n", nameWidth, e.display, count, row[1], row[2])
		}
	}
	fmt.Fprintln(r.out())
}

func (r *REPL) dotDimensions() {
	qr, err := r.query(`SELECT category, id, value FROM dimension_values ORDER BY category, id`)
	if err != nil {
		fmt.Fprintf(r.out(), "Error: %v\n", err)
		return
	}
	if qr.Error != "" {
		fmt.Fprintf(r.out(), "Error: %s\n", qr.Error)
		return
	}
	fmt.Fprint(r.out(), formatQueryResponse(qr))
}

func (r *REPL) dotExport(args string) {
	sql, path, err := parseExportArgs(args)
	if err != nil {
		fmt.Fprintf(r.out(), "Error: %v\n", err)
		fmt.Fprintln(r.out(), "Usage: .export <table> <path>")
		fmt.Fprintln(r.out(), "       .export (<sql query>) <path>")
		return
	}

	resp, err := r.export(api.ExportRequest{SQL: sql, Path: path})
	if err != nil {
		fmt.Fprintf(r.out(), "Error: %v\n", err)
		return
	}
	if resp.Error != "" {
		fmt.Fprintf(r.out(), "Error: %s\n", resp.Error)
		return
	}
	fmt.Fprintf(r.out(), "Exported %d rows to %s\n", resp.RowCount, resp.Path)
}

// parseExportArgs parses the arguments to the .export command.
// Formats:
//
//	.export <table> <path>
//	.export (<sql query>) <path>
func parseExportArgs(input string) (sql, path string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", fmt.Errorf("missing arguments")
	}

	if input[0] == '(' {
		// Find matching closing paren
		depth := 0
		end := -1
		for i, ch := range input {
			if ch == '(' {
				depth++
			} else if ch == ')' {
				depth--
				if depth == 0 {
					end = i
					break
				}
			}
		}
		if end < 0 {
			return "", "", fmt.Errorf("unmatched parenthesis")
		}
		sql = strings.TrimSpace(input[1:end])
		path = strings.TrimSpace(input[end+1:])
	} else {
		// First token is a table name
		fields := strings.Fields(input)
		if len(fields) < 2 {
			return "", "", fmt.Errorf("missing output path")
		}
		sql = fmt.Sprintf("SELECT * FROM %s", fields[0])
		path = fields[1]
	}

	if sql == "" {
		return "", "", fmt.Errorf("empty SQL query")
	}
	if path == "" {
		return "", "", fmt.Errorf("missing output path")
	}
	return sql, path, nil
}

func (r *REPL) dotHelp() {
	fmt.Fprint(r.out(), `
Commands:
  .metrics             Metric tables with row counts and time ranges
  .tables              List all tables with row counts
  .schema [table]      Show column definitions (all tables or one)
  .columns <table>     Alias for .schema <table>
  .count [table]       Row counts with time ranges (metric tables or one)
  .dimensions          Show dimension lookup values (mounts, sensors, etc.)
  .export              Export data to file (csv, parquet, json)
  .help                Show this help
  .quit / .exit        Exit

SQL:
  Terminate statements with a semicolon (;)
  Multi-line input is supported (prompt changes to ...>)
  Ctrl+C cancels current input, Ctrl+D exits

Archive views:
  When Parquet archival is configured, all_* views combine DuckDB and
  archived data (e.g., all_cpu_metrics, all_disk_metrics). Use these to
  query across the full history including data archived to Parquet.

Examples:
  SELECT * FROM cpu_metrics ORDER BY ts DESC LIMIT 10;

  SELECT d.value AS mount, AVG(m.used_bytes * 100.0 / m.total_bytes) AS used_pct
  FROM disk_metrics m JOIN dimension_values d ON d.category = 'mount' AND d.id = m.mount_id
  WHERE m.ts > now() - INTERVAL '1 hour' GROUP BY d.value;

  SELECT p.name, AVG(m.cpu_user_pct + m.cpu_system_pct) AS avg_cpu
  FROM process_metrics m JOIN process_info p ON p.pid = m.pid AND p.start_time = m.start_time
  WHERE m.ts > now() - INTERVAL '1 hour' GROUP BY p.name ORDER BY avg_cpu DESC LIMIT 10;

  -- Query across DuckDB + archived Parquet data:
  SELECT COUNT(*) FROM all_cpu_metrics WHERE ts > '2025-01-01';

Export:
  .export all_cpu_metrics /tmp/cpu.csv
  .export all_disk_metrics /tmp/disk.parquet
  .export (SELECT * FROM all_cpu_metrics WHERE ts > now() - INTERVAL '1 hour') /tmp/recent.json

  Format is inferred from file extension (.csv, .parquet, .json).
  Path must be absolute.

Snapshots:
  For advanced analysis, create a standalone DuckDB snapshot:
    bewitch -config /etc/bewitch.toml snapshot /tmp/metrics.duckdb

  The snapshot merges live database and archived Parquet data into a
  single DuckDB file. Open it directly with DuckDB CLI, DBeaver,
  Jupyter, or any DuckDB-compatible tool — no daemon connection needed.

  For ad-hoc backups, include system tables (alerts, preferences, etc.):
    bewitch snapshot -with-system-tables /tmp/backup.duckdb

`)
}

// printBanner prints the startup banner with database info.
func (r *REPL) printBanner() {
	fmt.Fprintf(r.out(), "bewitch sql — interactive DuckDB query console\n")
	fmt.Fprintf(r.out(), "Connected to daemon at %s\n", r.target)

	// Show table summary
	metricTables := []string{
		"cpu_metrics", "memory_metrics", "disk_metrics", "network_metrics",
		"temperature_metrics", "power_metrics", "ecc_metrics", "process_metrics",
	}
	var parts []string
	for _, t := range metricTables {
		qr, err := r.query(fmt.Sprintf("SELECT COUNT(*) FROM %s", t))
		if err != nil || qr.Error != "" || len(qr.Rows) == 0 {
			continue
		}
		count := formatValue(qr.Rows[0][0])
		if count != "0" {
			short := strings.TrimSuffix(t, "_metrics")
			parts = append(parts, fmt.Sprintf("%s: %s", short, count))
		}
	}
	if len(parts) > 0 {
		fmt.Fprintf(r.out(), "Rows:     %s\n", strings.Join(parts, ", "))
	}

	// Show archive info if configured
	if r.archivePath != "" {
		if entries, err := filepath.Glob(filepath.Join(r.archivePath, "*", "*.parquet")); err == nil && len(entries) > 0 {
			var totalSize int64
			for _, e := range entries {
				if info, err := os.Stat(e); err == nil {
					totalSize += info.Size()
				}
			}
			fmt.Fprintf(r.out(), "Archive:  %s (%d files, %s)\n", r.archivePath, len(entries), formatBytes(totalSize))
			fmt.Fprintf(r.out(), "          Use all_* views to query across DuckDB + archived data\n")
		}
	}

	fmt.Fprintf(r.out(), "Type .help for commands, Ctrl+D to exit.\n\n")
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
