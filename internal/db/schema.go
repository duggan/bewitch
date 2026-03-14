package db

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// Table groupings for compaction and snapshot operations.
var (
	// MetricTables lists tables with time-series metric data (have a ts column).
	MetricTables = []string{
		"cpu_metrics",
		"memory_metrics",
		"disk_metrics",
		"network_metrics",
		"ecc_metrics",
		"temperature_metrics",
		"power_metrics",
		"process_metrics",
	}

	// DimensionTables lists dimension/lookup tables.
	DimensionTables = []string{
		"dimension_values",
		"process_info",
	}

	// SystemTables lists daemon-internal tables (alerts, preferences, etc).
	SystemTables = []string{
		"preferences",
		"alerts",
		"alert_rules",
		"alert_rule_threshold",
		"alert_rule_predictive",
		"alert_rule_variance",
		"alert_rule_process_down",
		"alert_rule_process_thrashing",
		"archive_state",
		"scheduled_jobs",
	}
)

// AllTables returns all tables in dependency order (dimensions first, then
// metrics, then system tables).
func AllTables() []string {
	out := make([]string, 0, len(DimensionTables)+len(MetricTables)+len(SystemTables))
	out = append(out, DimensionTables...)
	out = append(out, MetricTables...)
	out = append(out, SystemTables...)
	return out
}

// nextvalSchemaRe matches nextval('schema_name.seq_name') and captures the
// schema prefix so it can be stripped.
var nextvalSchemaRe = regexp.MustCompile(`nextval\('[^']*\.([^']+)'\)`)

// CreateTablesIn introspects the main schema and creates matching tables in
// the given attached schema. Only tables in the provided list are created.
// DEFAULT expressions containing nextval() have schema prefixes stripped so
// sequences resolve correctly after a DB file swap.
func CreateTablesIn(db *sql.DB, schema string, tables []string) error {
	for _, table := range tables {
		ddl, err := buildCreateTable(db, schema, table)
		if err != nil {
			return fmt.Errorf("introspecting %s: %w", table, err)
		}
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("creating %s.%s: %w", schema, table, err)
		}
	}
	return nil
}

// CreateSequencesIn introspects sequences in the main schema and creates
// matching sequences in the target schema.
func CreateSequencesIn(db *sql.DB, schema string) error {
	rows, err := db.Query(`SELECT sequence_name FROM duckdb_sequences() WHERE schema_name = 'main'`)
	if err != nil {
		return fmt.Errorf("listing sequences: %w", err)
	}
	defer rows.Close()

	var seqs []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		seqs = append(seqs, name)
	}

	for _, seq := range seqs {
		ddl := fmt.Sprintf("CREATE SEQUENCE %s.%s START 1", schema, seq)
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("creating sequence %s.%s: %w", schema, seq, err)
		}
	}
	return nil
}

// CreateIndexesIn creates the standard indexes in the target schema.
func CreateIndexesIn(db *sql.DB, schema string) error {
	indexes := []struct{ ddl string }{
		{fmt.Sprintf("CREATE INDEX idx_dimension_lookup ON %s.dimension_values(category, value)", schema)},
	}
	for _, idx := range indexes {
		if _, err := db.Exec(idx.ddl); err != nil {
			return fmt.Errorf("creating index: %w", err)
		}
	}
	return nil
}

type columnInfo struct {
	name     string
	dataType string
	nullable bool
	defVal   sql.NullString
}

func buildCreateTable(db *sql.DB, schema, table string) (string, error) {
	// Get columns
	rows, err := db.Query(`
		SELECT column_name, data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = 'main' AND table_name = $1
		ORDER BY ordinal_position`, table)
	if err != nil {
		return "", fmt.Errorf("querying columns: %w", err)
	}
	defer rows.Close()

	var cols []columnInfo
	for rows.Next() {
		var c columnInfo
		var nullable string
		if err := rows.Scan(&c.name, &c.dataType, &nullable, &c.defVal); err != nil {
			return "", err
		}
		c.nullable = nullable == "YES"
		cols = append(cols, c)
	}
	if len(cols) == 0 {
		return "", fmt.Errorf("table %s not found in main schema", table)
	}

	// Get primary key columns
	pkCols, err := getPrimaryKey(db, table)
	if err != nil {
		return "", err
	}

	// Build DDL
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE %s.%s (\n", schema, table)
	for i, c := range cols {
		fmt.Fprintf(&b, "    %s %s", c.name, c.dataType)
		if c.defVal.Valid && c.defVal.String != "" {
			def := c.defVal.String
			// Strip schema prefix from nextval() calls
			def = nextvalSchemaRe.ReplaceAllString(def, "nextval('$1')")
			fmt.Fprintf(&b, " DEFAULT %s", def)
		}
		if !c.nullable {
			b.WriteString(" NOT NULL")
		}
		if i < len(cols)-1 || len(pkCols) > 0 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	if len(pkCols) > 0 {
		fmt.Fprintf(&b, "    PRIMARY KEY (%s)\n", strings.Join(pkCols, ", "))
	}
	b.WriteString(")")
	return b.String(), nil
}

func getPrimaryKey(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(`
		SELECT constraint_column_names
		FROM duckdb_constraints()
		WHERE table_name = $1 AND constraint_type = 'PRIMARY KEY'`, table)
	if err != nil {
		return nil, fmt.Errorf("querying pk: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		var raw interface{}
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		// DuckDB driver returns LIST columns as []interface{}
		if arr, ok := raw.([]interface{}); ok {
			cols := make([]string, len(arr))
			for i, v := range arr {
				cols[i] = fmt.Sprint(v)
			}
			return cols, nil
		}
	}
	return nil, nil
}
