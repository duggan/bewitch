package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.up.sql
var migrationsFS embed.FS

// migration represents a schema migration, either SQL or Go-function based.
type migration struct {
	Version int
	Name    string
	SQL     string             // non-empty for SQL file migrations
	Fn      func(*sql.DB) error // non-nil for Go function migrations
}

// goMigrations returns migrations implemented as Go functions.
func goMigrations() []migration {
	return []migration{
		{Version: 2, Name: "alert_rules_normalize", Fn: migrateAlertRules},
	}
}

// loadMigrations reads embedded SQL files and merges with Go migrations.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("reading migrations dir: %w", err)
	}

	var migrations []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		// Parse version from "000001_name.up.sql"
		parts := strings.SplitN(e.Name(), "_", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid migration filename: %s", e.Name())
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid migration version in %s: %w", e.Name(), err)
		}
		name := strings.TrimSuffix(parts[1], ".up.sql")

		data, err := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("reading migration %s: %w", e.Name(), err)
		}
		migrations = append(migrations, migration{Version: version, Name: name, SQL: string(data)})
	}

	// Merge Go migrations
	for _, gm := range goMigrations() {
		migrations = append(migrations, gm)
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

// tableExists checks if a table exists in the database.
func tableExists(db *sql.DB, table string) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = ?`, table).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// runMigrations applies pending migrations to the database.
func runMigrations(db *sql.DB) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	hasVersionTable, err := tableExists(db, "schema_version")
	if err != nil {
		return fmt.Errorf("checking schema_version table: %w", err)
	}

	if !hasVersionTable {
		// Detect pre-migration databases by checking for an existing table.
		hasData, err := tableExists(db, "cpu_metrics")
		if err != nil {
			return fmt.Errorf("checking cpu_metrics table: %w", err)
		}

		if _, err := db.Exec(`CREATE TABLE schema_version (
			version INTEGER NOT NULL,
			dirty BOOLEAN NOT NULL DEFAULT false,
			applied_at TIMESTAMP NOT NULL DEFAULT current_timestamp
		)`); err != nil {
			return fmt.Errorf("creating schema_version table: %w", err)
		}

		startVersion := 0
		if hasData {
			// Existing database: stamp as version 1 (initial schema already applied).
			startVersion = 1
			log.Printf("existing database detected, stamping schema version %d", startVersion)
		}
		if _, err := db.Exec(`INSERT INTO schema_version (version, dirty) VALUES (?, false)`, startVersion); err != nil {
			return fmt.Errorf("initializing schema_version: %w", err)
		}
	}

	var currentVersion int
	var dirty bool
	if err := db.QueryRow(`SELECT version, dirty FROM schema_version`).Scan(&currentVersion, &dirty); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	if dirty {
		return fmt.Errorf("database is dirty at version %d; a previous migration failed — restore from backup or fix manually", currentVersion)
	}

	for _, m := range migrations {
		if m.Version <= currentVersion {
			continue
		}

		// Mark dirty before executing.
		if _, err := db.Exec(`UPDATE schema_version SET version = ?, dirty = true, applied_at = current_timestamp`, m.Version); err != nil {
			return fmt.Errorf("marking migration %06d dirty: %w", m.Version, err)
		}

		log.Printf("applying migration %06d_%s", m.Version, m.Name)

		if m.SQL != "" {
			if _, err := db.Exec(m.SQL); err != nil {
				return fmt.Errorf("migration %06d_%s failed: %w", m.Version, m.Name, err)
			}
		} else if m.Fn != nil {
			if err := m.Fn(db); err != nil {
				return fmt.Errorf("migration %06d_%s failed: %w", m.Version, m.Name, err)
			}
		}

		// Mark clean.
		if _, err := db.Exec(`UPDATE schema_version SET dirty = false, applied_at = current_timestamp`); err != nil {
			return fmt.Errorf("marking migration %06d clean: %w", m.Version, err)
		}
	}

	return nil
}
