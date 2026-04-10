package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/duckdb/duckdb-go/v2"
	"github.com/duggan/bewitch/internal/collector"
	"github.com/duggan/bewitch/internal/db"
)

// processKey uniquely identifies a process instance (PID reuse is handled by start_time)
type processKey struct {
	pid       int32
	startTime int64
}

// Store writes collected metrics to DuckDB. When paused (during maintenance
// or compaction), incoming samples are buffered and flushed on resume.
type Store struct {
	db     *sql.DB
	mu     sync.Mutex
	buf    []collector.Sample
	paused bool
	opMu   sync.Mutex // prevents concurrent maintenance/compaction

	// Dimension ID caches (category -> value -> id)
	dimCache   map[string]map[string]int16
	dimCacheMu sync.RWMutex
	dimNextID  map[string]int16 // next available ID per category

	// Process info cache - tracks which (pid, start_time) pairs have been inserted
	procInfoCache   map[processKey]bool
	procInfoCacheMu sync.RWMutex
}

func New(db *sql.DB) *Store {
	s := &Store{
		db:            db,
		dimCache:      make(map[string]map[string]int16),
		dimNextID:     make(map[string]int16),
		procInfoCache: make(map[processKey]bool),
	}
	// Initialize caches for each dimension category
	for _, cat := range []string{"sensor", "interface", "mount", "device", "zone", "gpu"} {
		s.dimCache[cat] = make(map[string]int16)
	}
	// Load existing dimension values from DB
	s.loadDimensionCache()
	// Load existing process info from DB
	s.loadProcessInfoCache()
	return s
}

// loadDimensionCache populates the in-memory cache from the database
func (s *Store) loadDimensionCache() {
	rows, err := s.db.Query("SELECT category, id, value FROM dimension_values")
	if err != nil {
		return // Table might not exist yet
	}
	defer rows.Close()

	for rows.Next() {
		var category, value string
		var id int16
		if err := rows.Scan(&category, &id, &value); err != nil {
			continue
		}
		if s.dimCache[category] == nil {
			s.dimCache[category] = make(map[string]int16)
		}
		s.dimCache[category][value] = id
		if id >= s.dimNextID[category] {
			s.dimNextID[category] = id + 1
		}
	}
}

// loadProcessInfoCache populates the process info cache from the database
func (s *Store) loadProcessInfoCache() {
	rows, err := s.db.Query("SELECT pid, start_time FROM process_info")
	if err != nil {
		return // Table might not exist yet
	}
	defer rows.Close()

	for rows.Next() {
		var pid int32
		var startTime int64
		if err := rows.Scan(&pid, &startTime); err != nil {
			continue
		}
		s.procInfoCache[processKey{pid: pid, startTime: startTime}] = true
	}
}

// getDimensionID returns the ID for a dimension value, creating it if needed
func (s *Store) getDimensionID(tx *sql.Tx, category, value string) (int16, error) {
	// Check cache first
	s.dimCacheMu.RLock()
	if id, ok := s.dimCache[category][value]; ok {
		s.dimCacheMu.RUnlock()
		return id, nil
	}
	s.dimCacheMu.RUnlock()

	// Not in cache, need to insert
	s.dimCacheMu.Lock()
	defer s.dimCacheMu.Unlock()

	// Double-check after acquiring write lock
	if id, ok := s.dimCache[category][value]; ok {
		return id, nil
	}

	// Assign new ID
	id := s.dimNextID[category]
	s.dimNextID[category]++

	// Insert into database
	_, err := tx.Exec("INSERT INTO dimension_values (id, category, value) VALUES (?, ?, ?)", id, category, value)
	if err != nil {
		return 0, fmt.Errorf("inserting dimension value: %w", err)
	}

	// Update cache
	if s.dimCache[category] == nil {
		s.dimCache[category] = make(map[string]int16)
	}
	s.dimCache[category][value] = id
	return id, nil
}

// DB returns the current database connection. This may change after compaction.
func (s *Store) DB() *sql.DB {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db
}

// Write dispatches a sample to the appropriate insert method. If the store
// is paused, the sample is buffered in memory and will be flushed on resume.
func (s *Store) Write(sample collector.Sample) error {
	s.mu.Lock()
	if s.paused {
		s.buf = append(s.buf, sample)
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	return s.writeSample(sample)
}

func (s *Store) writeSample(sample collector.Sample) error {
	switch data := sample.Data.(type) {
	case collector.CPUData:
		return s.writeCPU(sample, data)
	case collector.MemoryData:
		return s.writeMemory(sample, data)
	case collector.DiskData:
		return s.writeDisk(sample, data)
	case collector.NetworkData:
		return s.writeNetwork(sample, data)
	case collector.ECCData:
		return s.writeECC(sample, data)
	case collector.TemperatureData:
		return s.writeTemperature(sample, data)
	case collector.PowerData:
		return s.writePower(sample, data)
	case collector.GPUData:
		return s.writeGPU(sample, data)
	case collector.ProcessData:
		return s.writeProcess(sample, data)
	default:
		return fmt.Errorf("unknown sample kind: %s", sample.Kind)
	}
}

// WriteBatch writes multiple samples using DuckDB Appenders for efficiency.
// Samples with nil Data are skipped (from failed collectors).
func (s *Store) WriteBatch(samples []collector.Sample) error {
	s.mu.Lock()
	if s.paused {
		for _, sample := range samples {
			if sample.Data != nil {
				s.buf = append(s.buf, sample)
			}
		}
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	// Phase 1: Do all SQL-based operations BEFORE acquiring driver connection.
	// This avoids deadlock since we only have 1 connection (SetMaxOpenConns(1)).
	for _, sample := range samples {
		if sample.Data == nil {
			continue
		}
		if err := s.prepareSampleForAppender(sample); err != nil {
			return fmt.Errorf("prepare %s: %w", sample.Kind, err)
		}
	}

	// Phase 2: Get driver connection for appenders (blocks the single connection)
	driverConn, sqlConn, err := db.GetDriverConn(context.Background(), s.db)
	if err != nil {
		return fmt.Errorf("getting driver conn: %w", err)
	}
	defer sqlConn.Close()

	// Phase 3: Write all samples using appenders (no SQL calls allowed here)
	for _, sample := range samples {
		if sample.Data == nil {
			continue
		}
		if err := s.writeSampleAppender(driverConn, sample); err != nil {
			return fmt.Errorf("write %s: %w", sample.Kind, err)
		}
	}

	return nil
}

// prepareSampleForAppender does any SQL-based prep work before we acquire the driver connection.
// This includes inserting new dimension values and process_info records.
func (s *Store) prepareSampleForAppender(sample collector.Sample) error {
	switch data := sample.Data.(type) {
	case collector.DiskData:
		// Pre-cache dimension IDs (inserts if needed)
		for _, m := range data.Mounts {
			s.ensureDimensionID("mount", m.Mount)
			if m.Device != "" {
				s.ensureDimensionID("device", m.Device)
			}
		}
	case collector.NetworkData:
		for _, iface := range data.Interfaces {
			s.ensureDimensionID("interface", iface.Interface)
		}
	case collector.TemperatureData:
		for _, sensor := range data.Sensors {
			s.ensureDimensionID("sensor", sensor.Sensor)
		}
	case collector.PowerData:
		for _, z := range data.Zones {
			s.ensureDimensionID("zone", z.Zone)
		}
	case collector.GPUData:
		for _, g := range data.GPUs {
			s.ensureDimensionID("gpu", g.Name)
		}
	case collector.ProcessData:
		// Insert new process_info records
		s.prepareProcessInfo(sample, data)
	}
	return nil
}

// prepareProcessInfo inserts new process_info records before appender phase.
// Uses batch insert for better performance per DuckDB guidelines.
func (s *Store) prepareProcessInfo(sample collector.Sample, data collector.ProcessData) {
	s.procInfoCacheMu.RLock()
	var newProcs []collector.ProcessSample
	for _, p := range data.Processes {
		key := processKey{pid: p.PID, startTime: p.StartTime}
		if !s.procInfoCache[key] {
			newProcs = append(newProcs, p)
		}
	}
	s.procInfoCacheMu.RUnlock()

	if len(newProcs) == 0 {
		return
	}

	// Build batch insert with multiple VALUES
	var args []interface{}
	placeholders := make([]string, len(newProcs))
	for i, p := range newProcs {
		placeholders[i] = "(?, ?, ?, ?, ?, ?, ?)"
		args = append(args, p.PID, p.StartTime, p.PPID, p.Name, p.Cmdline, p.UID, sample.Timestamp)
	}

	query := `INSERT INTO process_info (pid, start_time, ppid, name, cmdline, uid, first_seen)
		VALUES ` + strings.Join(placeholders, ", ") + `
		ON CONFLICT (pid, start_time) DO NOTHING`

	_, err := s.db.Exec(query, args...)
	if err != nil {
		log.Errorf("batch insert process_info: %v", err)
		return
	}

	// Update cache for all successfully inserted processes
	s.procInfoCacheMu.Lock()
	for _, p := range newProcs {
		s.procInfoCache[processKey{pid: p.PID, startTime: p.StartTime}] = true
	}
	s.procInfoCacheMu.Unlock()
}

func (s *Store) writeSampleAppender(driverConn driver.Conn, sample collector.Sample) error {
	switch data := sample.Data.(type) {
	case collector.CPUData:
		return s.writeCPUAppender(driverConn, sample, data)
	case collector.MemoryData:
		return s.writeMemoryAppender(driverConn, sample, data)
	case collector.DiskData:
		return s.writeDiskAppender(driverConn, sample, data)
	case collector.NetworkData:
		return s.writeNetworkAppender(driverConn, sample, data)
	case collector.ECCData:
		return s.writeECCAppender(driverConn, sample, data)
	case collector.TemperatureData:
		return s.writeTemperatureAppender(driverConn, sample, data)
	case collector.PowerData:
		return s.writePowerAppender(driverConn, sample, data)
	case collector.GPUData:
		return s.writeGPUAppender(driverConn, sample, data)
	case collector.ProcessData:
		return s.writeProcessAppender(driverConn, sample, data)
	default:
		return fmt.Errorf("unknown sample kind: %s", sample.Kind)
	}
}

func (s *Store) pause() {
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
}

func (s *Store) resume() {
	s.mu.Lock()
	pending := s.buf
	s.buf = nil
	s.paused = false
	s.mu.Unlock()

	for _, sample := range pending {
		if err := s.writeSample(sample); err != nil {
			log.Errorf("flush buffered %s: %v", sample.Kind, err)
		}
	}
}

// metricTables aliases db.MetricTables for convenience.
var metricTables = db.MetricTables

// Prune deletes rows older than the retention cutoff from all metric tables.
// DuckDB handles WAL checkpointing automatically via wal_autocheckpoint.
func (s *Store) Prune(retention time.Duration) error {
	if retention == 0 {
		return nil
	}
	s.pause()
	defer s.resume()

	cutoff := time.Now().Add(-retention)
	for _, table := range metricTables {
		if _, err := s.db.Exec(fmt.Sprintf("DELETE FROM %s WHERE ts < ?", table), cutoff); err != nil {
			return fmt.Errorf("prune %s: %w", table, err)
		}
	}

	// Clean up orphaned process_info entries (processes with no remaining metrics)
	if _, err := s.db.Exec(`DELETE FROM process_info WHERE NOT EXISTS (
		SELECT 1 FROM process_metrics pm
		WHERE pm.pid = process_info.pid AND pm.start_time = process_info.start_time
	)`); err != nil {
		return fmt.Errorf("prune process_info: %w", err)
	}

	log.Infof("prune: deleted rows older than %v", retention)
	return nil
}

// PruneExclusive runs Prune with exclusive access (blocks compaction).
func (s *Store) PruneExclusive(retention time.Duration) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.Prune(retention)
}

// CompactExclusive runs Compact with exclusive access (blocks maintenance).
func (s *Store) CompactExclusive(dbPath string) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.Compact(dbPath)
}

// Checkpoint forces a WAL checkpoint, flushing all WAL data to the main database file.
// This ensures data durability in case of a hard crash.
func (s *Store) Checkpoint() error {
	_, err := s.db.Exec("CHECKPOINT")
	return err
}

// GetJobLastRun reads the last run timestamp for a scheduled job.
// Returns the timestamp, whether a row was found, and any error.
func (s *Store) GetJobLastRun(jobName string) (time.Time, bool, error) {
	var lastRun time.Time
	err := s.db.QueryRow(
		"SELECT last_run_ts FROM scheduled_jobs WHERE job_name = ?",
		jobName,
	).Scan(&lastRun)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query scheduled_jobs: %w", err)
	}
	return lastRun, true, nil
}

// RecordJobRun records the current time as the last run for a scheduled job.
func (s *Store) RecordJobRun(jobName string) error {
	now := time.Now()
	_, err := s.db.Exec(
		`INSERT INTO scheduled_jobs (job_name, last_run_ts) VALUES (?, ?)
		 ON CONFLICT (job_name) DO UPDATE SET last_run_ts = excluded.last_run_ts`,
		jobName, now,
	)
	return err
}

// IsJobOverdue returns true if the named job has never run or if
// time since last run exceeds the given interval.
func (s *Store) IsJobOverdue(jobName string, interval time.Duration) (bool, error) {
	lastRun, found, err := s.GetJobLastRun(jobName)
	if err != nil {
		return false, err
	}
	if !found {
		return true, nil
	}
	return time.Since(lastRun) >= interval, nil
}

// Compact rebuilds the database file to reclaim fragmented space. It creates
// a fresh database with proper schema and copies data into it.
// Note: COPY FROM DATABASE preserves internal fragmentation, so we create
// tables with schema first and then INSERT data.
func (s *Store) Compact(dbPath string) error {
	s.pause()
	defer s.resume()

	tmpPath := dbPath + ".compact"

	// Clean up any leftover temp file
	os.Remove(tmpPath)

	// Attach a fresh database
	if _, err := s.db.Exec(fmt.Sprintf("ATTACH '%s' AS compact_db", tmpPath)); err != nil {
		return fmt.Errorf("attach compact db: %w", err)
	}

	// Create schema by introspecting the live database — this ensures
	// compaction always matches the current schema after migrations.
	if err := db.CreateSequencesIn(s.db, "compact_db"); err != nil {
		s.db.Exec("DETACH compact_db")
		os.Remove(tmpPath)
		return fmt.Errorf("creating compact sequences: %w", err)
	}
	tables := db.AllTables()
	if err := db.CreateTablesIn(s.db, "compact_db", tables); err != nil {
		s.db.Exec("DETACH compact_db")
		os.Remove(tmpPath)
		return fmt.Errorf("creating compact schema: %w", err)
	}

	// Copy data from each table
	for _, table := range tables {
		query := fmt.Sprintf("INSERT INTO compact_db.%s SELECT * FROM %s", table, table)
		if _, err := s.db.Exec(query); err != nil {
			s.db.Exec("DETACH compact_db")
			os.Remove(tmpPath)
			return fmt.Errorf("copy data %s: %w", table, err)
		}
		// Log row count for diagnostics
		var count int64
		if err := s.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count); err == nil {
			log.Infof("compaction: copied %s (%d rows)", table, count)
		}
	}

	if err := db.CreateIndexesIn(s.db, "compact_db"); err != nil {
		log.Warnf("compact index creation: %v", err)
	}

	// Force checkpoint on the compact database to ensure all data is written
	if _, err := s.db.Exec("CHECKPOINT compact_db"); err != nil {
		s.db.Exec("DETACH compact_db")
		os.Remove(tmpPath)
		return fmt.Errorf("checkpoint compact db: %w", err)
	}

	if _, err := s.db.Exec("DETACH compact_db"); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("detach compact db: %w", err)
	}

	// Log the size of the compacted database for diagnostics
	if info, err := os.Stat(tmpPath); err == nil {
		log.Infof("compacted db size: %d bytes (%.1f MB)", info.Size(), float64(info.Size())/(1024*1024))
	}
	if info, err := os.Stat(dbPath); err == nil {
		log.Infof("original db size: %d bytes (%.1f MB)", info.Size(), float64(info.Size())/(1024*1024))
	}

	// Close the current connection, swap files, reopen
	s.db.Close()

	// Preserve the original as a backup until swap succeeds
	backupPath := dbPath + ".pre-compact"
	if err := os.Rename(dbPath, backupPath); err != nil {
		return fmt.Errorf("backup original: %w", err)
	}

	if err := os.Rename(tmpPath, dbPath); err != nil {
		// Try to restore the backup
		os.Rename(backupPath, dbPath)
		return fmt.Errorf("swap compact db: %w", err)
	}

	// Also move any WAL file that might exist
	os.Remove(dbPath + ".wal")

	// Reopen the database
	newDB, err := sql.Open("duckdb", dbPath)
	if err != nil {
		// Restore backup
		os.Rename(backupPath, dbPath)
		return fmt.Errorf("reopen after compact: %w", err)
	}
	newDB.SetMaxOpenConns(4)
	s.db = newDB

	// Reload caches for the new connection
	s.loadDimensionCache()
	s.loadProcessInfoCache()

	// Remove backup and any leftover compact WAL
	os.Remove(backupPath)
	os.Remove(filepath.Join(filepath.Dir(dbPath), filepath.Base(tmpPath)+".wal"))

	log.Infof("compaction complete: %s", dbPath)
	return nil
}

// SnapshotExclusive runs Snapshot with exclusive access (blocks maintenance).
func (s *Store) SnapshotExclusive(snapshotPath, archivePath string, withSystemTables bool) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.Snapshot(snapshotPath, archivePath, withSystemTables)
}

// Snapshot creates a standalone DuckDB file containing all metric and dimension
// data from both the live database and any Parquet archives. The resulting file
// can be opened independently for ad-hoc analysis without a running daemon.
// When withSystemTables is true, daemon-internal tables (preferences, alerts,
// alert rules, archive state, scheduled jobs) are also included for backup purposes.
func (s *Store) Snapshot(snapshotPath, archivePath string, withSystemTables bool) error {
	s.pause()
	defer s.resume()

	// Clean up any leftover file
	os.Remove(snapshotPath)

	if _, err := s.db.Exec(fmt.Sprintf("ATTACH '%s' AS snap_db", snapshotPath)); err != nil {
		return fmt.Errorf("attach snapshot db: %w", err)
	}

	snapshotTables := append(db.DimensionTables, db.MetricTables...)
	if err := db.CreateTablesIn(s.db, "snap_db", snapshotTables); err != nil {
		s.db.Exec("DETACH snap_db")
		os.Remove(snapshotPath)
		return fmt.Errorf("creating snapshot schema: %w", err)
	}

	// Copy metric tables, merging with Parquet archives when available
	for _, table := range metricTables {
		var query string
		parquetGlob := filepath.Join(archivePath, table, "*.parquet")
		if archivePath != "" && HasParquetFiles(archivePath, table) {
			query = fmt.Sprintf(
				"INSERT INTO snap_db.%s SELECT * FROM %s UNION ALL SELECT * FROM read_parquet('%s')",
				table, table, parquetGlob)
		} else {
			query = fmt.Sprintf("INSERT INTO snap_db.%s SELECT * FROM %s", table, table)
		}
		if _, err := s.db.Exec(query); err != nil {
			s.db.Exec("DETACH snap_db")
			os.Remove(snapshotPath)
			return fmt.Errorf("copy %s: %w", table, err)
		}
		var count int64
		if err := s.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM snap_db.%s", table)).Scan(&count); err == nil {
			log.Infof("snapshot: %s (%d rows)", table, count)
		}
	}

	// Copy dimension tables, merging with Parquet snapshots when available
	for _, item := range []struct{ table, file string }{
		{"dimension_values", "dimension_values.parquet"},
		{"process_info", "process_info.parquet"},
	} {
		var query string
		parquetFile := filepath.Join(archivePath, item.file)
		if archivePath != "" {
			if _, err := os.Stat(parquetFile); err == nil {
				query = fmt.Sprintf(
					"INSERT INTO snap_db.%s SELECT DISTINCT * FROM (SELECT * FROM %s UNION ALL SELECT * FROM read_parquet('%s'))",
					item.table, item.table, parquetFile)
			}
		}
		if query == "" {
			query = fmt.Sprintf("INSERT INTO snap_db.%s SELECT * FROM %s", item.table, item.table)
		}
		if _, err := s.db.Exec(query); err != nil {
			s.db.Exec("DETACH snap_db")
			os.Remove(snapshotPath)
			return fmt.Errorf("copy %s: %w", item.table, err)
		}
		var count int64
		if err := s.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM snap_db.%s", item.table)).Scan(&count); err == nil {
			log.Infof("snapshot: %s (%d rows)", item.table, count)
		}
	}

	if err := db.CreateIndexesIn(s.db, "snap_db"); err != nil {
		log.Warnf("snapshot index: %v", err)
	}

	// Optionally include system/daemon tables for backup purposes
	if withSystemTables {
		if err := db.CreateTablesIn(s.db, "snap_db", db.SystemTables); err != nil {
			s.db.Exec("DETACH snap_db")
			os.Remove(snapshotPath)
			return fmt.Errorf("creating system tables schema: %w", err)
		}

		systemTables := db.SystemTables
		for _, table := range systemTables {
			query := fmt.Sprintf("INSERT INTO snap_db.%s SELECT * FROM %s", table, table)
			if _, err := s.db.Exec(query); err != nil {
				s.db.Exec("DETACH snap_db")
				os.Remove(snapshotPath)
				return fmt.Errorf("copy %s: %w", table, err)
			}
			var count int64
			if err := s.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM snap_db.%s", table)).Scan(&count); err == nil && count > 0 {
				log.Infof("snapshot: %s (%d rows)", table, count)
			}
		}
	}

	if _, err := s.db.Exec("CHECKPOINT snap_db"); err != nil {
		s.db.Exec("DETACH snap_db")
		os.Remove(snapshotPath)
		return fmt.Errorf("checkpoint snapshot db: %w", err)
	}

	if _, err := s.db.Exec("DETACH snap_db"); err != nil {
		os.Remove(snapshotPath)
		return fmt.Errorf("detach snapshot db: %w", err)
	}

	if info, err := os.Stat(snapshotPath); err == nil {
		log.Infof("snapshot complete: %s (%.1f MB)", snapshotPath, float64(info.Size())/(1024*1024))
	}
	return nil
}

// HasParquetFiles checks whether any .parquet files exist for a table in the archive.
func HasParquetFiles(archivePath, table string) bool {
	pattern := filepath.Join(archivePath, table, "*.parquet")
	matches, err := filepath.Glob(pattern)
	return err == nil && len(matches) > 0
}

// withTx runs fn within a database transaction, handling begin, rollback on
// error, and commit.
func (s *Store) withTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) writeCPU(sample collector.Sample, data collector.CPUData) error {
	return s.withTx(func(tx *sql.Tx) error {
		return s.writeCPUTx(tx, sample, data)
	})
}

func (s *Store) writeCPUTx(tx *sql.Tx, sample collector.Sample, data collector.CPUData) error {
	stmt, err := tx.Prepare("INSERT INTO cpu_metrics (ts, core, user_pct, system_pct, idle_pct, iowait_pct) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, core := range data.Cores {
		if _, err := stmt.Exec(sample.Timestamp, core.Core, core.UserPct, core.SystemPct, core.IdlePct, core.IOWaitPct); err != nil {
			return fmt.Errorf("insert cpu core %d: %w", core.Core, err)
		}
	}
	return nil
}

func (s *Store) writeMemory(sample collector.Sample, data collector.MemoryData) error {
	return s.withTx(func(tx *sql.Tx) error {
		return s.writeMemoryTx(tx, sample, data)
	})
}

func (s *Store) writeMemoryTx(tx *sql.Tx, sample collector.Sample, data collector.MemoryData) error {
	_, err := tx.Exec(
		"INSERT INTO memory_metrics (ts, total_bytes, used_bytes, available_bytes, buffers_bytes, cached_bytes, swap_total_bytes, swap_used_bytes) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		sample.Timestamp, data.TotalBytes, data.UsedBytes, data.AvailableBytes, data.BuffersBytes, data.CachedBytes, data.SwapTotalBytes, data.SwapUsedBytes,
	)
	return err
}

func (s *Store) writeDisk(sample collector.Sample, data collector.DiskData) error {
	return s.withTx(func(tx *sql.Tx) error {
		return s.writeDiskTx(tx, sample, data)
	})
}

func (s *Store) writeDiskTx(tx *sql.Tx, sample collector.Sample, data collector.DiskData) error {
	stmt, err := tx.Prepare("INSERT INTO disk_metrics (ts, mount_id, device_id, total_bytes, used_bytes, free_bytes, read_bytes_sec, write_bytes_sec, read_iops, write_iops) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, m := range data.Mounts {
		mountID, err := s.getDimensionID(tx, "mount", m.Mount)
		if err != nil {
			return fmt.Errorf("get mount id: %w", err)
		}
		var deviceID *int16
		if m.Device != "" {
			id, err := s.getDimensionID(tx, "device", m.Device)
			if err != nil {
				return fmt.Errorf("get device id: %w", err)
			}
			deviceID = &id
		}
		if _, err := stmt.Exec(sample.Timestamp, mountID, deviceID, m.TotalBytes, m.UsedBytes, m.FreeBytes, m.ReadBytesSec, m.WriteBytesSec, m.ReadIOPS, m.WriteIOPS); err != nil {
			return fmt.Errorf("insert disk %s: %w", m.Mount, err)
		}
	}
	return nil
}

func (s *Store) writeNetwork(sample collector.Sample, data collector.NetworkData) error {
	return s.withTx(func(tx *sql.Tx) error {
		return s.writeNetworkTx(tx, sample, data)
	})
}

func (s *Store) writeNetworkTx(tx *sql.Tx, sample collector.Sample, data collector.NetworkData) error {
	stmt, err := tx.Prepare("INSERT INTO network_metrics (ts, interface_id, rx_bytes_sec, tx_bytes_sec, rx_packets_sec, tx_packets_sec, rx_errors, tx_errors) VALUES (?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, iface := range data.Interfaces {
		ifaceID, err := s.getDimensionID(tx, "interface", iface.Interface)
		if err != nil {
			return fmt.Errorf("get interface id: %w", err)
		}
		if _, err := stmt.Exec(sample.Timestamp, ifaceID, iface.RxBytesSec, iface.TxBytesSec, iface.RxPacketsSec, iface.TxPacketsSec, iface.RxErrors, iface.TxErrors); err != nil {
			return fmt.Errorf("insert network %s: %w", iface.Interface, err)
		}
	}
	return nil
}

func (s *Store) writeECC(sample collector.Sample, data collector.ECCData) error {
	return s.withTx(func(tx *sql.Tx) error {
		return s.writeECCTx(tx, sample, data)
	})
}

func (s *Store) writeECCTx(tx *sql.Tx, sample collector.Sample, data collector.ECCData) error {
	_, err := tx.Exec(
		"INSERT INTO ecc_metrics (ts, corrected, uncorrected) VALUES (?, ?, ?)",
		sample.Timestamp, data.Corrected, data.Uncorrected,
	)
	return err
}

func (s *Store) writeTemperature(sample collector.Sample, data collector.TemperatureData) error {
	return s.withTx(func(tx *sql.Tx) error {
		return s.writeTemperatureTx(tx, sample, data)
	})
}

func (s *Store) writeTemperatureTx(tx *sql.Tx, sample collector.Sample, data collector.TemperatureData) error {
	stmt, err := tx.Prepare("INSERT INTO temperature_metrics (ts, sensor_id, temp_celsius) VALUES (?, ?, ?)")
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, sensor := range data.Sensors {
		sensorID, err := s.getDimensionID(tx, "sensor", sensor.Sensor)
		if err != nil {
			return fmt.Errorf("get sensor id: %w", err)
		}
		if _, err := stmt.Exec(sample.Timestamp, sensorID, sensor.TempCelsius); err != nil {
			return fmt.Errorf("insert temp %s: %w", sensor.Sensor, err)
		}
	}
	return nil
}

func (s *Store) writePower(sample collector.Sample, data collector.PowerData) error {
	return s.withTx(func(tx *sql.Tx) error {
		return s.writePowerTx(tx, sample, data)
	})
}

func (s *Store) writePowerTx(tx *sql.Tx, sample collector.Sample, data collector.PowerData) error {
	stmt, err := tx.Prepare("INSERT INTO power_metrics (ts, zone_id, watts) VALUES (?, ?, ?)")
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, z := range data.Zones {
		zoneID, err := s.getDimensionID(tx, "zone", z.Zone)
		if err != nil {
			return fmt.Errorf("get zone id: %w", err)
		}
		if _, err := stmt.Exec(sample.Timestamp, zoneID, z.Watts); err != nil {
			return fmt.Errorf("insert power %s: %w", z.Zone, err)
		}
	}
	return nil
}

func (s *Store) writeGPU(sample collector.Sample, data collector.GPUData) error {
	return s.withTx(func(tx *sql.Tx) error {
		return s.writeGPUTx(tx, sample, data)
	})
}

func (s *Store) writeGPUTx(tx *sql.Tx, sample collector.Sample, data collector.GPUData) error {
	stmt, err := tx.Prepare(`INSERT INTO gpu_metrics (ts, gpu_id, utilization_pct, memory_used_bytes, memory_total_bytes, temp_celsius, power_watts, frequency_mhz, frequency_max_mhz, throttle_pct) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, g := range data.GPUs {
		gpuID, err := s.getDimensionID(tx, "gpu", g.Name)
		if err != nil {
			return fmt.Errorf("get gpu id: %w", err)
		}
		if _, err := stmt.Exec(sample.Timestamp, gpuID, g.UtilizationPct,
			int64(g.MemoryUsedBytes), int64(g.MemoryTotalBytes), g.TempCelsius,
			g.PowerWatts, int32(g.FrequencyMHz), int32(g.FrequencyMaxMHz), g.ThrottlePct); err != nil {
			return fmt.Errorf("insert gpu %s: %w", g.Name, err)
		}
	}
	return nil
}

func (s *Store) writeProcess(sample collector.Sample, data collector.ProcessData) error {
	return s.withTx(func(tx *sql.Tx) error {
		return s.writeProcessTx(tx, sample, data)
	})
}

func (s *Store) writeProcessTx(tx *sql.Tx, sample collector.Sample, data collector.ProcessData) error {
	// Upsert process info (static attributes) - only inserts if new
	infoStmt, err := tx.Prepare(`INSERT INTO process_info
		(pid, start_time, ppid, name, cmdline, uid, first_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (pid, start_time) DO NOTHING`)
	if err != nil {
		return fmt.Errorf("prepare info: %w", err)
	}
	defer infoStmt.Close()

	// Insert metrics (dynamic data only)
	metricStmt, err := tx.Prepare(`INSERT INTO process_metrics
		(ts, pid, start_time, state, cpu_user_pct, cpu_system_pct, rss_bytes, num_fds, num_threads)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare metrics: %w", err)
	}
	defer metricStmt.Close()

	for _, p := range data.Processes {
		// Upsert static process info
		if _, err := infoStmt.Exec(
			p.PID, p.StartTime, p.PPID, p.Name, p.Cmdline, p.UID, sample.Timestamp,
		); err != nil {
			return fmt.Errorf("insert process_info %d: %w", p.PID, err)
		}

		// Insert dynamic metrics
		if _, err := metricStmt.Exec(
			sample.Timestamp, p.PID, p.StartTime, p.State,
			p.CPUUserPct, p.CPUSystemPct, p.RSSBytes, p.NumFDs, p.NumThreads,
		); err != nil {
			return fmt.Errorf("insert process_metrics %d: %w", p.PID, err)
		}
	}
	return nil
}

// ============================================================================
// Appender-based write methods for high-performance bulk inserts
// ============================================================================

func (s *Store) writeCPUAppender(driverConn driver.Conn, sample collector.Sample, data collector.CPUData) error {
	appender, err := duckdb.NewAppenderFromConn(driverConn, "", "cpu_metrics")
	if err != nil {
		return fmt.Errorf("create appender: %w", err)
	}
	defer appender.Close()

	for _, core := range data.Cores {
		if err := appender.AppendRow(sample.Timestamp, int8(core.Core), core.UserPct, core.SystemPct, core.IdlePct, core.IOWaitPct); err != nil {
			return fmt.Errorf("append cpu core %d: %w", core.Core, err)
		}
	}
	return appender.Flush()
}

func (s *Store) writeMemoryAppender(driverConn driver.Conn, sample collector.Sample, data collector.MemoryData) error {
	appender, err := duckdb.NewAppenderFromConn(driverConn, "", "memory_metrics")
	if err != nil {
		return fmt.Errorf("create appender: %w", err)
	}
	defer appender.Close()

	if err := appender.AppendRow(
		sample.Timestamp,
		int64(data.TotalBytes), int64(data.UsedBytes), int64(data.AvailableBytes),
		int64(data.BuffersBytes), int64(data.CachedBytes),
		int64(data.SwapTotalBytes), int64(data.SwapUsedBytes),
	); err != nil {
		return fmt.Errorf("append memory: %w", err)
	}
	return appender.Flush()
}

func (s *Store) writeDiskAppender(driverConn driver.Conn, sample collector.Sample, data collector.DiskData) error {
	appender, err := duckdb.NewAppenderFromConn(driverConn, "", "disk_metrics")
	if err != nil {
		return fmt.Errorf("create appender: %w", err)
	}
	defer appender.Close()

	for _, m := range data.Mounts {
		mountID := s.getCachedDimensionID("mount", m.Mount)
		// For nullable columns, appender needs nil or a value (not a pointer)
		var deviceID any = nil
		if m.Device != "" {
			deviceID = s.getCachedDimensionID("device", m.Device)
		}
		if err := appender.AppendRow(
			sample.Timestamp, mountID, deviceID,
			int64(m.TotalBytes), int64(m.UsedBytes), int64(m.FreeBytes),
			m.ReadBytesSec, m.WriteBytesSec, m.ReadIOPS, m.WriteIOPS,
		); err != nil {
			return fmt.Errorf("append disk %s: %w", m.Mount, err)
		}
	}
	return appender.Flush()
}

func (s *Store) writeNetworkAppender(driverConn driver.Conn, sample collector.Sample, data collector.NetworkData) error {
	appender, err := duckdb.NewAppenderFromConn(driverConn, "", "network_metrics")
	if err != nil {
		return fmt.Errorf("create appender: %w", err)
	}
	defer appender.Close()

	for _, iface := range data.Interfaces {
		ifaceID := s.getCachedDimensionID("interface", iface.Interface)
		if err := appender.AppendRow(
			sample.Timestamp, ifaceID,
			iface.RxBytesSec, iface.TxBytesSec, iface.RxPacketsSec, iface.TxPacketsSec,
			int64(iface.RxErrors), int64(iface.TxErrors),
		); err != nil {
			return fmt.Errorf("append network %s: %w", iface.Interface, err)
		}
	}
	return appender.Flush()
}

func (s *Store) writeECCAppender(driverConn driver.Conn, sample collector.Sample, data collector.ECCData) error {
	appender, err := duckdb.NewAppenderFromConn(driverConn, "", "ecc_metrics")
	if err != nil {
		return fmt.Errorf("create appender: %w", err)
	}
	defer appender.Close()

	if err := appender.AppendRow(sample.Timestamp, int64(data.Corrected), int64(data.Uncorrected)); err != nil {
		return fmt.Errorf("append ecc: %w", err)
	}
	return appender.Flush()
}

func (s *Store) writeTemperatureAppender(driverConn driver.Conn, sample collector.Sample, data collector.TemperatureData) error {
	appender, err := duckdb.NewAppenderFromConn(driverConn, "", "temperature_metrics")
	if err != nil {
		return fmt.Errorf("create appender: %w", err)
	}
	defer appender.Close()

	for _, sensor := range data.Sensors {
		sensorID := s.getCachedDimensionID("sensor", sensor.Sensor)
		if err := appender.AppendRow(sample.Timestamp, sensorID, sensor.TempCelsius); err != nil {
			return fmt.Errorf("append temp %s: %w", sensor.Sensor, err)
		}
	}
	return appender.Flush()
}

func (s *Store) writePowerAppender(driverConn driver.Conn, sample collector.Sample, data collector.PowerData) error {
	appender, err := duckdb.NewAppenderFromConn(driverConn, "", "power_metrics")
	if err != nil {
		return fmt.Errorf("create appender: %w", err)
	}
	defer appender.Close()

	for _, z := range data.Zones {
		zoneID := s.getCachedDimensionID("zone", z.Zone)
		if err := appender.AppendRow(sample.Timestamp, zoneID, z.Watts); err != nil {
			return fmt.Errorf("append power %s: %w", z.Zone, err)
		}
	}
	return appender.Flush()
}

func (s *Store) writeGPUAppender(driverConn driver.Conn, sample collector.Sample, data collector.GPUData) error {
	appender, err := duckdb.NewAppenderFromConn(driverConn, "", "gpu_metrics")
	if err != nil {
		return fmt.Errorf("create appender: %w", err)
	}
	defer appender.Close()

	for _, g := range data.GPUs {
		gpuID := s.getCachedDimensionID("gpu", g.Name)
		if err := appender.AppendRow(sample.Timestamp, gpuID, g.UtilizationPct,
			int64(g.MemoryUsedBytes), int64(g.MemoryTotalBytes), g.TempCelsius,
			g.PowerWatts, int32(g.FrequencyMHz), int32(g.FrequencyMaxMHz), g.ThrottlePct); err != nil {
			return fmt.Errorf("append gpu %s: %w", g.Name, err)
		}
	}
	return appender.Flush()
}

func (s *Store) writeProcessAppender(driverConn driver.Conn, sample collector.Sample, data collector.ProcessData) error {
	// process_info inserts are handled in prepareProcessInfo() before we acquire driverConn
	// Here we only use the appender for process_metrics
	metricsAppender, err := duckdb.NewAppenderFromConn(driverConn, "", "process_metrics")
	if err != nil {
		return fmt.Errorf("create process_metrics appender: %w", err)
	}
	defer metricsAppender.Close()

	for _, p := range data.Processes {
		if err := metricsAppender.AppendRow(
			sample.Timestamp, int32(p.PID), p.StartTime, p.State,
			p.CPUUserPct, p.CPUSystemPct, int64(p.RSSBytes), int32(p.NumFDs), int32(p.NumThreads),
		); err != nil {
			return fmt.Errorf("append process_metrics %d: %w", p.PID, err)
		}
	}

	return metricsAppender.Flush()
}

// ensureDimensionID ensures a dimension value exists in the cache and DB.
// Called during Phase 1 (before driver connection is acquired).
func (s *Store) ensureDimensionID(category, value string) {
	s.dimCacheMu.RLock()
	if _, ok := s.dimCache[category][value]; ok {
		s.dimCacheMu.RUnlock()
		return
	}
	s.dimCacheMu.RUnlock()

	// Need to insert - use write lock
	s.dimCacheMu.Lock()
	defer s.dimCacheMu.Unlock()

	// Double-check
	if _, ok := s.dimCache[category][value]; ok {
		return
	}

	// Assign new ID and insert into DB
	id := s.dimNextID[category]
	s.dimNextID[category]++

	// Insert into DB (safe to call here, before driver connection is acquired)
	_, err := s.db.Exec("INSERT INTO dimension_values (id, category, value) VALUES (?, ?, ?)", id, category, value)
	if err != nil {
		log.Errorf("inserting dimension value %s/%s: %v", category, value, err)
	}

	if s.dimCache[category] == nil {
		s.dimCache[category] = make(map[string]int16)
	}
	s.dimCache[category][value] = id
}

// getCachedDimensionID returns a dimension ID from cache only.
// Called during Phase 3 (while driver connection is held). Must NOT do SQL calls.
// Panics if value is not cached (indicates bug in Phase 1 preparation).
func (s *Store) getCachedDimensionID(category, value string) int16 {
	s.dimCacheMu.RLock()
	defer s.dimCacheMu.RUnlock()
	if id, ok := s.dimCache[category][value]; ok {
		return id
	}
	// This should never happen if Phase 1 prepared correctly
	log.Errorf("BUG: dimension %s/%s not pre-cached", category, value)
	return 0
}
