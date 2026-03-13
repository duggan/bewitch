package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"path/filepath"

	"github.com/duckdb/duckdb-go/v2"
)

// Open opens a DuckDB database at the given path and runs migrations.
// It creates the parent directory if it does not exist.
// checkpointThreshold configures wal_autocheckpoint (e.g. "16MB", "256MB");
// empty uses DuckDB's default (16MB).
func Open(path string, checkpointThreshold string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, fmt.Errorf("opening duckdb: %w", err)
	}
	// Allow multiple connections for concurrent API access during batch writes.
	// DuckDB handles internal locking; single-writer is enforced at transaction level.
	db.SetMaxOpenConns(4)
	if checkpointThreshold != "" {
		if _, err := db.Exec(fmt.Sprintf("SET wal_autocheckpoint = '%s'", checkpointThreshold)); err != nil {
			db.Close()
			return nil, fmt.Errorf("setting wal_autocheckpoint: %w", err)
		}
	}
	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	// Cache Parquet file metadata in memory so repeated queries against
	// archived Parquet files skip metadata I/O.
	if _, err := db.Exec("SET parquet_metadata_cache = true"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling parquet metadata cache: %w", err)
	}
	return db, nil
}

// migrateAlertRules migrates alert rules from old denormalized schema to new normalized schema.
// It checks if the old 'metric' column exists in alert_rules and if so, migrates the data.
func migrateAlertRules(db *sql.DB) error {
	// Check if the old schema exists by looking for the 'metric' column
	var colCount int
	err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'alert_rules' AND column_name = 'metric'`).Scan(&colCount)
	if err != nil || colCount == 0 {
		// New schema or no alert_rules table yet, nothing to migrate
		return nil
	}

	// Migrate existing rules to type-specific tables
	rows, err := db.Query(`SELECT id, name, type, COALESCE(metric, ''), COALESCE(operator, ''),
		COALESCE(value, 0), COALESCE(duration, ''), COALESCE(mount, ''),
		COALESCE(interface_name, ''), COALESCE(sensor, ''), COALESCE(predict_hours, 0),
		COALESCE(threshold_pct, 0), severity, enabled FROM alert_rules`)
	if err != nil {
		return fmt.Errorf("reading old alert rules: %w", err)
	}
	defer rows.Close()

	type oldRule struct {
		id            int
		name          string
		ruleType      string
		metric        string
		operator      string
		value         float64
		duration      string
		mount         string
		interfaceName string
		sensor        string
		predictHours  int
		thresholdPct  float64
		severity      string
		enabled       bool
	}

	var rules []oldRule
	for rows.Next() {
		var r oldRule
		if err := rows.Scan(&r.id, &r.name, &r.ruleType, &r.metric, &r.operator,
			&r.value, &r.duration, &r.mount, &r.interfaceName, &r.sensor,
			&r.predictHours, &r.thresholdPct, &r.severity, &r.enabled); err != nil {
			return fmt.Errorf("scanning old rule: %w", err)
		}
		rules = append(rules, r)
	}

	if len(rules) == 0 {
		// No rules to migrate, just drop old columns
		return dropOldAlertRuleColumns(db)
	}

	// Insert into type-specific tables
	for _, r := range rules {
		switch r.ruleType {
		case "threshold":
			_, err = db.Exec(`INSERT INTO alert_rule_threshold
				(rule_id, metric, operator, value, duration, mount, interface_name, sensor)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				r.id, r.metric, r.operator, r.value, r.duration, r.mount, r.interfaceName, r.sensor)
		case "predictive":
			_, err = db.Exec(`INSERT INTO alert_rule_predictive
				(rule_id, metric, mount, predict_hours, threshold_pct)
				VALUES (?, ?, ?, ?, ?)`,
				r.id, r.metric, r.mount, r.predictHours, r.thresholdPct)
		case "variance":
			// Old schema stored delta in 'value' and min_count in 'threshold_pct'
			_, err = db.Exec(`INSERT INTO alert_rule_variance
				(rule_id, metric, delta_threshold, min_count, duration)
				VALUES (?, ?, ?, ?, ?)`,
				r.id, r.metric, r.value, int(r.thresholdPct), r.duration)
		}
		if err != nil {
			return fmt.Errorf("migrating rule %d: %w", r.id, err)
		}
	}

	return dropOldAlertRuleColumns(db)
}

// dropOldAlertRuleColumns removes the old type-specific columns from alert_rules table.
func dropOldAlertRuleColumns(db *sql.DB) error {
	columns := []string{"metric", "operator", "value", "duration", "mount",
		"interface_name", "sensor", "predict_hours", "threshold_pct"}
	for _, col := range columns {
		// Check if column exists before trying to drop
		var count int
		err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns
			WHERE table_name = 'alert_rules' AND column_name = ?`, col).Scan(&count)
		if err != nil || count == 0 {
			continue
		}
		if _, err := db.Exec(fmt.Sprintf("ALTER TABLE alert_rules DROP COLUMN %s", col)); err != nil {
			return fmt.Errorf("dropping column %s: %w", col, err)
		}
	}
	return nil
}

// GetDriverConn extracts the underlying DuckDB driver connection from an sql.Conn.
// This is needed for using the Appender API for bulk inserts.
func GetDriverConn(ctx context.Context, db *sql.DB) (driver.Conn, *sql.Conn, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("getting connection: %w", err)
	}

	var driverConn driver.Conn
	err = conn.Raw(func(dc interface{}) error {
		driverConn = dc.(driver.Conn)
		return nil
	})
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("getting driver connection: %w", err)
	}

	return driverConn, conn, nil
}

// NewAppender creates a DuckDB Appender for efficient bulk inserts.
func NewAppender(driverConn driver.Conn, table string) (*duckdb.Appender, error) {
	return duckdb.NewAppenderFromConn(driverConn, "", table)
}
