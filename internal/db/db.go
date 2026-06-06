package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/duckdb/duckdb-go/v2"
)

// PreCompactSuffix is appended to the DB path for the backup that store
// compaction creates while swapping in a freshly compacted file.
const PreCompactSuffix = ".pre-compact"

// Open opens a DuckDB database at the given path and runs migrations.
// It creates the parent directory if it does not exist.
// checkpointThreshold configures wal_autocheckpoint (e.g. "16MB", "256MB");
// empty uses DuckDB's default (16MB).
// memoryLimit caps DuckDB's working memory (e.g. "512MB", "1GB"); empty uses
// DuckDB's default (~80% of physical RAM). On memory-constrained hosts this is
// important so large sorts/exports spill to temp_directory instead of OOM-killing.
func Open(path string, checkpointThreshold string, memoryLimit string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}
	// Recover from a compaction that was interrupted mid-swap before we open the
	// path — otherwise a missing main file would silently become an empty DB.
	recoverInterruptedCompaction(path)
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, fmt.Errorf("opening duckdb: %w", err)
	}
	if err := applyConnSettings(db, path, checkpointThreshold, memoryLimit); err != nil {
		db.Close()
		return nil, err
	}
	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	// Force a checkpoint after migrations to flush WAL to the main DB file.
	// This prevents WAL replay failures on next startup caused by a DuckDB bug
	// where ALTER TABLE SET DEFAULT nextval() cannot be replayed.
	if _, err := db.Exec("CHECKPOINT"); err != nil {
		db.Close()
		return nil, fmt.Errorf("checkpointing after migrations: %w", err)
	}
	return db, nil
}

// Reopen opens an already-migrated database and applies the same runtime
// settings as Open, without re-running migrations. Store compaction uses this
// after swapping in the compacted file so the memory_limit / temp_directory /
// checkpoint / parquet-cache settings survive — they are per-instance in DuckDB
// and would otherwise reset to defaults (notably an ~80%-RAM memory_limit) on
// the fresh connection pool.
func Reopen(path string, checkpointThreshold string, memoryLimit string) (*sql.DB, error) {
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, fmt.Errorf("reopening duckdb: %w", err)
	}
	if err := applyConnSettings(db, path, checkpointThreshold, memoryLimit); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// applyConnSettings configures the connection pool and the runtime PRAGMAs.
// Shared by Open and Reopen so a compaction reopen doesn't silently drop them.
func applyConnSettings(db *sql.DB, path string, checkpointThreshold string, memoryLimit string) error {
	// Allow multiple connections for concurrent API access during batch writes.
	// DuckDB handles internal locking; single-writer is enforced at transaction level.
	db.SetMaxOpenConns(4)
	if checkpointThreshold != "" {
		if _, err := db.Exec(fmt.Sprintf("SET wal_autocheckpoint = '%s'", checkpointThreshold)); err != nil {
			return fmt.Errorf("setting wal_autocheckpoint: %w", err)
		}
	}
	if memoryLimit != "" {
		if _, err := db.Exec(fmt.Sprintf("SET memory_limit = '%s'", memoryLimit)); err != nil {
			return fmt.Errorf("setting memory_limit: %w", err)
		}
		// Spill to disk (next to the DB file) when a query exceeds memory_limit,
		// rather than failing or being OOM-killed.
		tempDir := filepath.Join(filepath.Dir(path), "duckdb_tmp")
		if err := os.MkdirAll(tempDir, 0755); err != nil {
			return fmt.Errorf("creating temp directory: %w", err)
		}
		if _, err := db.Exec(fmt.Sprintf("SET temp_directory = '%s'", tempDir)); err != nil {
			return fmt.Errorf("setting temp_directory: %w", err)
		}
	}
	// Cache Parquet file metadata in memory so repeated queries against
	// archived Parquet files skip metadata I/O.
	if _, err := db.Exec("SET parquet_metadata_cache = true"); err != nil {
		return fmt.Errorf("enabling parquet metadata cache: %w", err)
	}
	return nil
}

// recoverInterruptedCompaction restores a database left half-swapped by a
// compaction that was interrupted (crash/SIGKILL) between renaming the original
// aside (path -> path+PreCompactSuffix) and renaming the compacted file into
// place. Without this, a missing main file would cause DuckDB to create an empty
// database on open and the data would appear lost.
func recoverInterruptedCompaction(path string) {
	backup := path + PreCompactSuffix
	if _, err := os.Stat(backup); err != nil {
		return // no interrupted compaction
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// The compacted file was never moved into place; restore the original.
		log.Printf("recovering interrupted compaction: restoring %s from %s", path, backup)
		if err := os.Rename(backup, path); err != nil {
			log.Printf("WARNING: failed to restore %s from %s: %v", path, backup, err)
		}
		return
	}
	// The new (compacted) database is already in place; drop the stale backup.
	log.Printf("removing leftover compaction backup %s", backup)
	if err := os.Remove(backup); err != nil {
		log.Printf("WARNING: failed to remove stale backup %s: %v", backup, err)
	}
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

// migrateFixSequenceSchema fixes DEFAULT expressions on alerts and alert_rules
// that were incorrectly schema-qualified as compact_db.* after compaction.
func migrateFixSequenceSchema(db *sql.DB) error {
	// Only run if the alerts table exists (skipped for pre-migration DBs)
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables
		WHERE table_name = 'alerts'`).Scan(&count)
	if err != nil || count == 0 {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE alerts ALTER COLUMN id SET DEFAULT nextval('alert_id_seq')`); err != nil {
		return fmt.Errorf("fixing alerts sequence: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE alert_rules ALTER COLUMN id SET DEFAULT nextval('alert_rule_id_seq')`); err != nil {
		return fmt.Errorf("fixing alert_rules sequence: %w", err)
	}
	return nil
}

// migrateUniqueRuleNames deduplicates alert rule names and adds a UNIQUE index on
// alert_rules.name. Fired alerts, the engine debounce, and the delete-time cleanup all
// link to a rule by name, so two rules sharing a name corrupt each other. The API rejects
// new duplicates; this migration repairs any that predate that check (the lowest id keeps
// the original name; later ones get a " #N" suffix) so the unique index can be created.
func migrateUniqueRuleNames(db *sql.DB) error {
	exists, err := tableExists(db, "alert_rules")
	if err != nil {
		return fmt.Errorf("checking alert_rules table: %w", err)
	}
	if !exists {
		return nil
	}

	rows, err := db.Query(`SELECT id, name FROM alert_rules ORDER BY name, id`)
	if err != nil {
		return fmt.Errorf("listing alert rules: %w", err)
	}
	type rule struct {
		id   int
		name string
	}
	var rules []rule
	for rows.Next() {
		var r rule
		if err := rows.Scan(&r.id, &r.name); err != nil {
			rows.Close()
			return fmt.Errorf("scanning alert rule: %w", err)
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	seen := make(map[string]bool, len(rules))
	for _, r := range rules {
		if !seen[r.name] {
			seen[r.name] = true
			continue
		}
		// Duplicate name: find an unused " #N" variant.
		newName := r.name
		for n := 2; ; n++ {
			candidate := fmt.Sprintf("%s #%d", r.name, n)
			if !seen[candidate] {
				newName = candidate
				break
			}
		}
		seen[newName] = true
		if _, err := db.Exec(`UPDATE alert_rules SET name = ? WHERE id = ?`, newName, r.id); err != nil {
			return fmt.Errorf("renaming duplicate rule %d: %w", r.id, err)
		}
		log.Printf("alert rule %d renamed %q -> %q (duplicate name)", r.id, r.name, newName)
	}

	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_alert_rules_name ON alert_rules(name)`); err != nil {
		return fmt.Errorf("creating unique index on alert_rules.name: %w", err)
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
