package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/log"
)

// ArchiveExclusive exports data older than threshold to Parquet files.
// Uses opMu mutex to coordinate with prune/compact.
func (s *Store) ArchiveExclusive(archivePath string, threshold time.Duration) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.Archive(archivePath, threshold)
}

// Archive exports data older than threshold to Parquet files.
// Data is organized into daily Parquet files per table.
func (s *Store) Archive(archivePath string, threshold time.Duration) error {
	if threshold == 0 || archivePath == "" {
		return nil
	}

	s.pause()
	defer s.resume()

	cutoff := time.Now().Add(-threshold)

	// Archive each metric table
	for _, table := range metricTables {
		if err := s.archiveTable(table, archivePath, cutoff); err != nil {
			return fmt.Errorf("archive %s: %w", table, err)
		}
	}

	// Snapshot dimension tables (these are append-only, so a full snapshot is safe)
	if err := s.snapshotDimensionTables(archivePath); err != nil {
		return fmt.Errorf("snapshot dimension tables: %w", err)
	}

	// Checkpoint to flush WAL and reclaim space from deleted rows
	if _, err := s.db.Exec("CHECKPOINT"); err != nil {
		log.Warnf("archive: checkpoint failed: %v", err)
	}

	log.Infof("archive: completed for data older than %v", threshold)
	return nil
}

// archiveTable exports one table's data to daily Parquet files.
func (s *Store) archiveTable(table, archivePath string, cutoff time.Time) error {
	// Get the last archived timestamp for this table
	var lastArchived time.Time
	err := s.db.QueryRow("SELECT last_archived_ts FROM archive_state WHERE table_name = ?", table).Scan(&lastArchived)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("query archive_state: %w", err)
	}
	// If no prior archive, start from the beginning of time
	if err == sql.ErrNoRows {
		lastArchived = time.Time{}
	}

	// Check if there's data to archive
	var minTs, maxTs time.Time
	var count int64
	query := fmt.Sprintf("SELECT MIN(ts), MAX(ts), COUNT(*) FROM %s WHERE ts > ? AND ts <= ?", table)
	if err := s.db.QueryRow(query, lastArchived, cutoff).Scan(&minTs, &maxTs, &count); err != nil {
		return fmt.Errorf("query time range: %w", err)
	}

	if count == 0 {
		return nil // Nothing to archive
	}

	// Create table directory
	tableDir := filepath.Join(archivePath, table)
	if err := os.MkdirAll(tableDir, 0755); err != nil {
		return fmt.Errorf("create archive dir: %w", err)
	}

	// Group data by day and export
	current := time.Date(minTs.Year(), minTs.Month(), minTs.Day(), 0, 0, 0, 0, time.UTC)
	for !current.After(maxTs) {
		nextDay := current.AddDate(0, 0, 1)
		dateStr := current.Format("2006-01-02")
		parquetPath := filepath.Join(tableDir, dateStr+".parquet")

		// Determine the effective range for this day
		dayStart := current
		if lastArchived.After(dayStart) {
			dayStart = lastArchived
		}
		dayEnd := nextDay
		if cutoff.Before(dayEnd) {
			dayEnd = cutoff
		}

		// Export data for this day
		if err := s.exportToParquet(table, parquetPath, dayStart, dayEnd); err != nil {
			return fmt.Errorf("export %s to %s: %w", table, parquetPath, err)
		}

		current = nextDay
	}

	// Delete archived data from DuckDB
	deleteQuery := fmt.Sprintf("DELETE FROM %s WHERE ts > ? AND ts <= ?", table)
	result, err := s.db.Exec(deleteQuery, lastArchived, cutoff)
	if err != nil {
		return fmt.Errorf("delete archived data: %w", err)
	}
	rowsDeleted, _ := result.RowsAffected()

	// Update archive state
	_, err = s.db.Exec(`INSERT INTO archive_state (table_name, last_archived_ts) VALUES (?, ?)
		ON CONFLICT (table_name) DO UPDATE SET last_archived_ts = excluded.last_archived_ts`,
		table, cutoff)
	if err != nil {
		return fmt.Errorf("update archive_state: %w", err)
	}

	log.Infof("archive: %s - exported %d rows, deleted from DuckDB", table, rowsDeleted)
	return nil
}

// exportToParquet exports data from a table to a Parquet file.
// If the file exists, it merges with existing data.
func (s *Store) exportToParquet(table, parquetPath string, start, end time.Time) error {
	// Check if file exists - if so, we need to merge
	_, err := os.Stat(parquetPath)
	fileExists := err == nil

	if fileExists {
		// Merge existing Parquet data with new data
		tmpPath := parquetPath + ".tmp"
		query := fmt.Sprintf(`COPY (
			SELECT * FROM read_parquet('%s')
			UNION ALL
			SELECT * FROM %s WHERE ts > ? AND ts <= ?
			ORDER BY ts
		) TO '%s' (FORMAT parquet, COMPRESSION zstd)`, parquetPath, table, tmpPath)

		if _, err := s.db.Exec(query, start, end); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("merge to parquet: %w", err)
		}

		// Atomically replace old file
		if err := os.Rename(tmpPath, parquetPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("rename parquet: %w", err)
		}
	} else {
		// Simple export - no existing file
		query := fmt.Sprintf(`COPY (SELECT * FROM %s WHERE ts > ? AND ts <= ? ORDER BY ts) TO '%s' (FORMAT parquet, COMPRESSION zstd)`,
			table, parquetPath)

		if _, err := s.db.Exec(query, start, end); err != nil {
			return fmt.Errorf("copy to parquet: %w", err)
		}
	}

	return nil
}

// snapshotDimensionTables exports dimension_values and process_info to Parquet.
// These are snapshotted in full on each archive run.
func (s *Store) snapshotDimensionTables(archivePath string) error {
	if err := os.MkdirAll(archivePath, 0755); err != nil {
		return fmt.Errorf("create archive dir: %w", err)
	}

	// Snapshot dimension_values
	dimPath := filepath.Join(archivePath, "dimension_values.parquet")
	if _, err := s.db.Exec(fmt.Sprintf("COPY dimension_values TO '%s' (FORMAT parquet, COMPRESSION zstd)", dimPath)); err != nil {
		return fmt.Errorf("snapshot dimension_values: %w", err)
	}

	// Snapshot process_info
	procPath := filepath.Join(archivePath, "process_info.parquet")
	if _, err := s.db.Exec(fmt.Sprintf("COPY process_info TO '%s' (FORMAT parquet, COMPRESSION zstd)", procPath)); err != nil {
		return fmt.Errorf("snapshot process_info: %w", err)
	}

	return nil
}

// PruneArchive deletes Parquet files older than the retention period.
func (s *Store) PruneArchive(archivePath string, retention time.Duration) error {
	if retention == 0 || archivePath == "" {
		return nil
	}

	cutoff := time.Now().Add(-retention)
	cutoffDate := time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, time.UTC)

	var pruned int
	for _, table := range metricTables {
		tableDir := filepath.Join(archivePath, table)
		entries, err := os.ReadDir(tableDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read archive dir %s: %w", tableDir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".parquet" {
				continue
			}

			// Parse date from filename (YYYY-MM-DD.parquet)
			name := entry.Name()
			dateStr := name[:len(name)-8] // Remove ".parquet"
			fileDate, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				continue // Skip files with invalid names
			}

			// Delete if older than retention cutoff
			if fileDate.Before(cutoffDate) {
				filePath := filepath.Join(tableDir, name)
				if err := os.Remove(filePath); err != nil {
					log.Errorf("prune archive: failed to remove %s: %v", filePath, err)
				} else {
					pruned++
				}
			}
		}
	}

	if pruned > 0 {
		log.Infof("prune archive: deleted %d Parquet files older than %v", pruned, retention)
	}
	return nil
}

// Unarchive reloads all Parquet archive data back into the live DuckDB tables,
// then removes the Parquet files and resets the archive state.
func (s *Store) Unarchive(archivePath string) error {
	if archivePath == "" {
		return fmt.Errorf("archive path not configured")
	}

	s.pause()
	defer s.resume()

	// Reload each metric table from its Parquet files
	for _, table := range metricTables {
		if err := s.unarchiveTable(table, archivePath); err != nil {
			return fmt.Errorf("unarchive %s: %w", table, err)
		}
	}

	// Reload dimension tables (process_info has a PK so needs ON CONFLICT handling;
	// dimension_values likewise). The live tables are append-only so they already
	// contain all values, but archived data may have entries from pruned periods.
	if err := s.unarchiveDimensionTables(archivePath); err != nil {
		return fmt.Errorf("unarchive dimension tables: %w", err)
	}

	// Clear archive state so future archives start fresh
	if _, err := s.db.Exec("DELETE FROM archive_state"); err != nil {
		return fmt.Errorf("clear archive_state: %w", err)
	}

	// Checkpoint to flush WAL
	if _, err := s.db.Exec("CHECKPOINT"); err != nil {
		log.Warnf("unarchive: checkpoint failed: %v", err)
	}

	log.Infof("unarchive: completed, all Parquet data reloaded into DuckDB")
	return nil
}

// unarchiveTable inserts all Parquet data for a metric table back into DuckDB,
// then removes the Parquet files.
func (s *Store) unarchiveTable(table, archivePath string) error {
	tableDir := filepath.Join(archivePath, table)
	pattern := filepath.Join(tableDir, "*.parquet")
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) == 0 {
		return nil // Nothing to unarchive
	}

	// Insert all Parquet data back into the live table
	query := fmt.Sprintf("INSERT INTO %s SELECT * FROM read_parquet('%s')", table, pattern)
	result, err := s.db.Exec(query)
	if err != nil {
		return fmt.Errorf("insert from parquet: %w", err)
	}
	rows, _ := result.RowsAffected()

	// Remove Parquet files
	for _, f := range files {
		os.Remove(f)
	}
	// Remove directory if empty
	os.Remove(tableDir)

	log.Infof("unarchive: %s - reloaded %d rows, removed %d files", table, rows, len(files))
	return nil
}

// unarchiveDimensionTables reloads dimension_values and process_info from Parquet,
// using INSERT OR IGNORE to skip rows that already exist in the live tables.
func (s *Store) unarchiveDimensionTables(archivePath string) error {
	dimPath := filepath.Join(archivePath, "dimension_values.parquet")
	if _, err := os.Stat(dimPath); err == nil {
		if _, err := s.db.Exec(fmt.Sprintf(
			"INSERT OR IGNORE INTO dimension_values SELECT * FROM read_parquet('%s')", dimPath)); err != nil {
			return fmt.Errorf("reload dimension_values: %w", err)
		}
		os.Remove(dimPath)
	}

	procPath := filepath.Join(archivePath, "process_info.parquet")
	if _, err := os.Stat(procPath); err == nil {
		if _, err := s.db.Exec(fmt.Sprintf(
			"INSERT OR IGNORE INTO process_info SELECT * FROM read_parquet('%s')", procPath)); err != nil {
			return fmt.Errorf("reload process_info: %w", err)
		}
		os.Remove(procPath)
	}

	return nil
}

// UnarchiveExclusive runs Unarchive with exclusive access.
func (s *Store) UnarchiveExclusive(archivePath string) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.Unarchive(archivePath)
}

// PruneArchiveExclusive runs PruneArchive with exclusive access.
func (s *Store) PruneArchiveExclusive(archivePath string, retention time.Duration) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.PruneArchive(archivePath, retention)
}

// ArchiveStatus represents the archive state for a table.
type ArchiveStatus struct {
	TableName      string
	LastArchivedTS time.Time
}

// GetArchiveStatus returns the current archive state for all tables.
func (s *Store) GetArchiveStatus() ([]ArchiveStatus, error) {
	rows, err := s.db.Query("SELECT table_name, last_archived_ts FROM archive_state ORDER BY table_name")
	if err != nil {
		return nil, fmt.Errorf("query archive_state: %w", err)
	}
	defer rows.Close()

	var statuses []ArchiveStatus
	for rows.Next() {
		var status ArchiveStatus
		if err := rows.Scan(&status.TableName, &status.LastArchivedTS); err != nil {
			return nil, fmt.Errorf("scan archive_state: %w", err)
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// ArchiveDirStats returns statistics about the archive directory.
type ArchiveDirStats struct {
	TotalFiles int64
	TotalBytes int64
	Tables     map[string]TableArchiveStats
}

// TableArchiveStats holds archive statistics for a single table.
type TableArchiveStats struct {
	FileCount  int
	TotalBytes int64
	OldestFile string
	NewestFile string
}

// GetArchiveDirStats returns statistics about the Parquet archive directory.
func GetArchiveDirStats(archivePath string) (*ArchiveDirStats, error) {
	stats := &ArchiveDirStats{
		Tables: make(map[string]TableArchiveStats),
	}

	if archivePath == "" {
		return stats, nil
	}

	for _, table := range metricTables {
		tableDir := filepath.Join(archivePath, table)
		entries, err := os.ReadDir(tableDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read archive dir %s: %w", tableDir, err)
		}

		var tableStats TableArchiveStats
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".parquet" {
				continue
			}

			info, err := entry.Info()
			if err != nil {
				continue
			}

			tableStats.FileCount++
			tableStats.TotalBytes += info.Size()
			stats.TotalFiles++
			stats.TotalBytes += info.Size()

			name := entry.Name()[:len(entry.Name())-8] // Remove ".parquet"
			if tableStats.OldestFile == "" || name < tableStats.OldestFile {
				tableStats.OldestFile = name
			}
			if tableStats.NewestFile == "" || name > tableStats.NewestFile {
				tableStats.NewestFile = name
			}
		}

		if tableStats.FileCount > 0 {
			stats.Tables[table] = tableStats
		}
	}

	return stats, nil
}
