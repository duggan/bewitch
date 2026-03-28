package api

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/charmbracelet/log"

	"github.com/duggan/bewitch/internal/alert"
	"github.com/duggan/bewitch/internal/config"
)

// Server is the daemon HTTP API server over a unix socket (and optionally TCP).
type Server struct {
	cfg              *config.Config
	dbFn             func() *sql.DB
	srv              *http.Server
	tcpSrv           *http.Server     // separate server for TCP (with auth middleware)
	mux              *http.ServeMux
	listener         net.Listener
	tcpListener      net.Listener
	startTime        time.Time
	compactFn        func() error        // called to trigger database compaction
	snapshotFn       func(string, bool) error // called to create a database snapshot
	archiveFn        func() error        // called to trigger Parquet archival
	unarchiveFn      func() error        // called to reload Parquet data into DuckDB
	archiveStatusFn  func() ([]ArchiveStatusItem, error)
	archiveDirStatFn func() (*ArchiveDirStats, error)
	archivePath      string
	archiveThreshold time.Duration
	notifiers          []alert.Notifier   // notification destinations for test endpoint
	collectorIntervals map[string]string // collector name → interval string (set once at startup)
	done               chan struct{}      // closed on Shutdown to stop background goroutines

	// Live process snapshot (all processes from collector, served instead of DB query)
	procSnapshot   *ProcessResponse
	procGen        uint64 // incremented on each SetProcessSnapshot call
	procSnapshotMu sync.RWMutex

	// Cached latest metrics (pushed by daemon after each collection cycle)
	metricsSnapshot *metricsCache
	metricsMu       sync.RWMutex

	// History response cache (avoids re-running expensive DB queries every TUI tick)
	historyCache   map[string]*historyCacheEntry
	historyCacheMu sync.RWMutex
}

// metricsCache holds snapshots of the latest metric data.
// A generation counter enables ETag-based change detection.
type metricsCache struct {
	gen uint64 // incremented on each SetMetricsSnapshot call

	cpu   []CPUCoreMetric
	mem   *MemoryMetric
	disks []DiskMetric
	net   []NetworkMetric
	temps []TemperatureMetric
	power []PowerMetric
	ecc   *ECCMetric
	gpus  []GPUMetric
	dash  *DashboardData // lazily built from components; nil until first request
}

// historyCacheEntry caches a history response.
type historyCacheEntry struct {
	data    any
	expires time.Time
}

// ArchiveStatusItem represents the archive state for a table.
type ArchiveStatusItem struct {
	TableName      string    `json:"table_name"`
	LastArchivedTS time.Time `json:"last_archived_ts"`
}

// ArchiveDirStats holds statistics about the Parquet archive directory.
type ArchiveDirStats struct {
	TotalFiles int64                       `json:"total_files"`
	TotalBytes int64                       `json:"total_bytes"`
	Tables     map[string]TableArchiveStats `json:"tables"`
}

// TableArchiveStats holds archive statistics for a single table.
type TableArchiveStats struct {
	FileCount  int    `json:"file_count"`
	TotalBytes int64  `json:"total_bytes"`
	OldestFile string `json:"oldest_file"`
	NewestFile string `json:"newest_file"`
}

// SetCompactFunc sets the callback invoked by POST /api/compact.
func (s *Server) SetCompactFunc(fn func() error) {
	s.compactFn = fn
}

// SetSnapshotFunc sets the callback invoked by POST /api/snapshot.
func (s *Server) SetSnapshotFunc(fn func(string, bool) error) {
	s.snapshotFn = fn
}

// SetNotifiers stores the alert notifiers for the test endpoint.
func (s *Server) SetNotifiers(notifiers []alert.Notifier) {
	s.notifiers = notifiers
}

// SetArchiveFunc sets the callback invoked by POST /api/archive.
func (s *Server) SetArchiveFunc(fn func() error) {
	s.archiveFn = fn
}

// SetUnarchiveFunc sets the callback invoked by POST /api/unarchive.
func (s *Server) SetUnarchiveFunc(fn func() error) {
	s.unarchiveFn = fn
}

// SetArchiveStatusFunc sets the callback to get archive status.
func (s *Server) SetArchiveStatusFunc(fn func() ([]ArchiveStatusItem, error)) {
	s.archiveStatusFn = fn
}

// SetArchiveDirStatFunc sets the callback to get archive directory stats.
func (s *Server) SetArchiveDirStatFunc(fn func() (*ArchiveDirStats, error)) {
	s.archiveDirStatFn = fn
}

// SetCollectorIntervals stores per-collector interval info for the status endpoint.
func (s *Server) SetCollectorIntervals(intervals map[string]string) {
	s.collectorIntervals = intervals
}

// SetProcessSnapshot updates the live process snapshot served by the API.
// Called by the daemon after each collection cycle.
func (s *Server) SetProcessSnapshot(resp *ProcessResponse) {
	s.procSnapshotMu.Lock()
	s.procSnapshot = resp
	s.procGen++
	s.procSnapshotMu.Unlock()
}

// SetMetricsSnapshot updates the cached latest metrics served by the API.
// Called by the daemon after each collection cycle. Nil values are skipped
// (the collector didn't run this tick), preserving previous cached data.
// Dashboard composition is deferred until first request.
func (s *Server) SetMetricsSnapshot(
	cpu []CPUCoreMetric, mem *MemoryMetric, disks []DiskMetric,
	net []NetworkMetric, temps []TemperatureMetric, power []PowerMetric,
	ecc *ECCMetric,
) {
	s.metricsMu.Lock()
	mc := s.metricsSnapshot
	if mc == nil {
		mc = &metricsCache{}
	}
	mc.gen++
	if cpu != nil {
		mc.cpu = cpu
	}
	if mem != nil {
		mc.mem = mem
	}
	if disks != nil {
		mc.disks = disks
	}
	if net != nil {
		mc.net = net
	}
	if temps != nil {
		mc.temps = temps
	}
	if power != nil {
		mc.power = power
	}
	if ecc != nil {
		mc.ecc = ecc
	}
	// Invalidate dashboard (will be lazily rebuilt from components on next request)
	mc.dash = nil
	s.metricsSnapshot = mc
	s.metricsMu.Unlock()
}

// getCachedCPU returns the cached CPU metrics.
// Returns nil if no data is cached. The gen value can be used as an ETag.
func (s *Server) getCachedCPU() ([]CPUCoreMetric, uint64) {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()
	mc := s.metricsSnapshot
	if mc == nil || mc.cpu == nil {
		return nil, 0
	}
	return mc.cpu, mc.gen
}

// getCachedMemory returns the cached memory metrics.
func (s *Server) getCachedMemory() (*MemoryMetric, uint64) {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()
	mc := s.metricsSnapshot
	if mc == nil || mc.mem == nil {
		return nil, 0
	}
	return mc.mem, mc.gen
}

// getCachedDisk returns the cached disk metrics.
func (s *Server) getCachedDisk() ([]DiskMetric, uint64) {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()
	mc := s.metricsSnapshot
	if mc == nil || mc.disks == nil {
		return nil, 0
	}
	return mc.disks, mc.gen
}

// getCachedNetwork returns the cached network metrics.
func (s *Server) getCachedNetwork() ([]NetworkMetric, uint64) {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()
	mc := s.metricsSnapshot
	if mc == nil || mc.net == nil {
		return nil, 0
	}
	return mc.net, mc.gen
}

// getCachedTemperature returns the cached temperature metrics.
func (s *Server) getCachedTemperature() ([]TemperatureMetric, uint64) {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()
	mc := s.metricsSnapshot
	if mc == nil || mc.temps == nil {
		return nil, 0
	}
	return mc.temps, mc.gen
}

// getCachedPower returns the cached power metrics.
func (s *Server) getCachedPower() ([]PowerMetric, uint64) {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()
	mc := s.metricsSnapshot
	if mc == nil || mc.power == nil {
		return nil, 0
	}
	return mc.power, mc.gen
}

// getCachedGPU returns the cached GPU metrics.
func (s *Server) getCachedGPU() ([]GPUMetric, uint64) {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()
	mc := s.metricsSnapshot
	if mc == nil || mc.gpus == nil {
		return nil, 0
	}
	return mc.gpus, mc.gen
}

// SetGPUSnapshot updates the cached GPU metrics served by the API.
// Separate from SetMetricsSnapshot to match the SetProcessSnapshot pattern.
func (s *Server) SetGPUSnapshot(gpus []GPUMetric) {
	s.metricsMu.Lock()
	mc := s.metricsSnapshot
	if mc == nil {
		mc = &metricsCache{}
		s.metricsSnapshot = mc
	}
	mc.gen++
	mc.gpus = gpus
	mc.dash = nil
	s.metricsMu.Unlock()
}

// getCachedECC returns the cached ECC metrics.
func (s *Server) getCachedECC() (*ECCMetric, uint64) {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()
	mc := s.metricsSnapshot
	if mc == nil || mc.ecc == nil {
		return nil, 0
	}
	return mc.ecc, mc.gen
}

// getCachedDashboard returns the cached dashboard with lazy composition.
// The dashboard is built from cached metric components + the live process snapshot.
func (s *Server) getCachedDashboard() (*DashboardData, uint64) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	mc := s.metricsSnapshot
	if mc == nil {
		return nil, 0
	}
	if mc.dash == nil {
		// Lazily build dashboard from cached components
		dash := &DashboardData{
			CPU:         mc.cpu,
			Memory:      mc.mem,
			Disks:       mc.disks,
			Network:     mc.net,
			Temperature: mc.temps,
			Power:       mc.power,
		}
		if dash.CPU == nil {
			dash.CPU = []CPUCoreMetric{}
		}
		if dash.Disks == nil {
			dash.Disks = []DiskMetric{}
		}
		if dash.Network == nil {
			dash.Network = []NetworkMetric{}
		}
		if dash.Temperature == nil {
			dash.Temperature = []TemperatureMetric{}
		}
		if dash.Power == nil {
			dash.Power = []PowerMetric{}
		}
		if mc.gpus != nil {
			dash.GPU = mc.gpus
		}
		// Top 5 processes from the live process snapshot
		s.procSnapshotMu.RLock()
		snap := s.procSnapshot
		s.procSnapshotMu.RUnlock()
		if snap != nil {
			limit := 5
			if len(snap.Processes) < limit {
				limit = len(snap.Processes)
			}
			top := make([]ProcessMetric, limit)
			copy(top, snap.Processes[:limit])
			dash.Processes = &ProcessResponse{
				Processes:     top,
				TotalProcs:    snap.TotalProcs,
				RunningProcs:  snap.RunningProcs,
				ActiveProcs:   snap.ActiveProcs,
				TotalCPUPct:   snap.TotalCPUPct,
				TotalRSSBytes: snap.TotalRSSBytes,
			}
		}
		mc.dash = dash
	}
	return mc.dash, mc.gen
}

// getCachedProcess returns the cached process snapshot.
func (s *Server) getCachedProcess() (*ProcessResponse, uint64) {
	s.procSnapshotMu.RLock()
	defer s.procSnapshotMu.RUnlock()
	if s.procSnapshot == nil {
		return nil, 0
	}
	return s.procSnapshot, s.procGen
}

// metricsGenETag returns the ETag string for the given generation counter.
func metricsGenETag(gen uint64) string {
	return strconv.FormatUint(gen, 36)
}

// SetArchiveConfig sets the archive configuration for query building.
// It also creates all_* views that union DuckDB tables with Parquet archives.
func (s *Server) SetArchiveConfig(archivePath string, archiveThreshold time.Duration) {
	s.archivePath = archivePath
	s.archiveThreshold = archiveThreshold
	if archivePath != "" {
		s.CreateArchiveViews()
	}
}

// archiveViewTables lists the metric tables that get all_* archive views.
var archiveViewTables = []string{
	"cpu_metrics", "memory_metrics", "disk_metrics", "network_metrics",
	"ecc_metrics", "temperature_metrics", "power_metrics", "process_metrics", "gpu_metrics",
}

// CreateArchiveViews creates or replaces all_* views that union each metric
// table with its Parquet archive (if archive files exist for that table).
// Also creates all_dimension_values and all_process_info views.
// Safe to call multiple times (e.g., after each archive run to pick up new files).
func (s *Server) CreateArchiveViews() {
	if s.archivePath == "" {
		return
	}
	db := s.dbFn()

	for _, table := range archiveViewTables {
		viewName := "all_" + table
		parquetGlob := filepath.Join(s.archivePath, table, "*.parquet")

		if hasParquetFiles(s.archivePath, table) {
			query := fmt.Sprintf(
				`CREATE OR REPLACE VIEW %s AS SELECT * FROM %s UNION ALL SELECT * FROM read_parquet('%s')`,
				viewName, table, parquetGlob)
			if _, err := db.Exec(query); err != nil {
				log.Warnf("failed to create view %s: %v", viewName, err)
			}
		} else {
			// No archive files yet — view is just an alias for the base table
			query := fmt.Sprintf(
				`CREATE OR REPLACE VIEW %s AS SELECT * FROM %s`,
				viewName, table)
			if _, err := db.Exec(query); err != nil {
				log.Warnf("failed to create view %s: %v", viewName, err)
			}
		}
	}

	// Dimension tables (single Parquet file, not partitioned by month)
	for _, item := range []struct{ table, file string }{
		{"dimension_values", "dimension_values.parquet"},
		{"process_info", "process_info.parquet"},
	} {
		viewName := "all_" + item.table
		parquetFile := filepath.Join(s.archivePath, item.file)

		if _, err := os.Stat(parquetFile); err == nil {
			query := fmt.Sprintf(
				`CREATE OR REPLACE VIEW %s AS SELECT * FROM %s UNION ALL SELECT * FROM read_parquet('%s')`,
				viewName, item.table, parquetFile)
			if _, err := db.Exec(query); err != nil {
				log.Warnf("failed to create view %s: %v", viewName, err)
			}
		} else {
			query := fmt.Sprintf(
				`CREATE OR REPLACE VIEW %s AS SELECT * FROM %s`,
				viewName, item.table)
			if _, err := db.Exec(query); err != nil {
				log.Warnf("failed to create view %s: %v", viewName, err)
			}
		}
	}
}

// hasParquetFiles checks whether any .parquet files exist in the archive
// directory for the given table.
func hasParquetFiles(archivePath, table string) bool {
	pattern := filepath.Join(archivePath, table, "*.parquet")
	matches, err := filepath.Glob(pattern)
	return err == nil && len(matches) > 0
}

func NewServer(cfg *config.Config, dbFn func() *sql.DB) *Server {
	s := &Server{
		cfg:          cfg,
		dbFn:         dbFn,
		startTime:    time.Now(),
		historyCache: make(map[string]*historyCacheEntry),
		done:         make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/alerts", s.handleListAlerts)
	mux.HandleFunc("POST /api/alerts/{id}/ack", s.handleAckAlert)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("GET /api/metrics/cpu", s.handleMetricsCPU)
	mux.HandleFunc("GET /api/metrics/memory", s.handleMetricsMemory)
	mux.HandleFunc("GET /api/metrics/disk", s.handleMetricsDisk)
	mux.HandleFunc("GET /api/metrics/network", s.handleMetricsNetwork)
	mux.HandleFunc("GET /api/metrics/temperature", s.handleMetricsTemperature)
	mux.HandleFunc("GET /api/metrics/power", s.handleMetricsPower)
	mux.HandleFunc("GET /api/metrics/ecc", s.handleMetricsECC)
	mux.HandleFunc("GET /api/metrics/gpu", s.handleMetricsGPU)
	mux.HandleFunc("GET /api/metrics/process", s.handleMetricsProcess)
	mux.HandleFunc("GET /api/metrics/dashboard", s.handleMetricsDashboard)
	mux.HandleFunc("POST /api/compact", s.handleCompact)
	mux.HandleFunc("GET /api/history/cpu", s.handleHistoryCPU)
	mux.HandleFunc("GET /api/history/memory", s.handleHistoryMemory)
	mux.HandleFunc("GET /api/history/disk", s.handleHistoryDisk)
	mux.HandleFunc("GET /api/history/temperature", s.handleHistoryTemperature)
	mux.HandleFunc("GET /api/history/network", s.handleHistoryNetwork)
	mux.HandleFunc("GET /api/history/power", s.handleHistoryPower)
	mux.HandleFunc("GET /api/history/gpu", s.handleHistoryGPU)
	mux.HandleFunc("GET /api/history/process", s.handleHistoryProcess)
	mux.HandleFunc("GET /api/alert-rules", s.handleListAlertRules)
	mux.HandleFunc("POST /api/alert-rules", s.handleCreateAlertRule)
	mux.HandleFunc("DELETE /api/alert-rules/{id}", s.handleDeleteAlertRule)
	mux.HandleFunc("PUT /api/alert-rules/{id}/toggle", s.handleToggleAlertRule)
	mux.HandleFunc("GET /api/preferences", s.handleGetPreferences)
	mux.HandleFunc("POST /api/preferences", s.handleSetPreference)
	mux.HandleFunc("POST /api/test-notifications", s.handleTestNotifications)
	mux.HandleFunc("POST /api/archive", s.handleArchive)
	mux.HandleFunc("POST /api/unarchive", s.handleUnarchive)
	mux.HandleFunc("GET /api/archive/status", s.handleArchiveStatus)
	mux.HandleFunc("POST /api/query", s.handleQuery)
	mux.HandleFunc("POST /api/export", s.handleExport)
	mux.HandleFunc("POST /api/snapshot", s.handleSnapshot)

	s.mux = mux
	s.srv = &http.Server{Handler: mux}
	go s.cleanHistoryCache()
	return s
}

// cleanHistoryCache periodically evicts expired entries from the history cache.
func (s *Server) cleanHistoryCache() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			s.historyCacheMu.Lock()
			for k, entry := range s.historyCache {
				if now.After(entry.expires) {
					delete(s.historyCache, k)
				}
			}
			s.historyCacheMu.Unlock()
		case <-s.done:
			return
		}
	}
}

// buildTLSConfig returns a TLS configuration for the TCP listener, or nil if TLS is disabled.
func (s *Server) buildTLSConfig() (*tls.Config, error) {
	cfg := s.cfg.Daemon

	if cfg.TLSDisabled {
		return nil, nil
	}

	var cert tls.Certificate
	var certDER []byte

	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		var err error
		cert, err = tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return nil, fmt.Errorf("loading TLS cert/key: %w", err)
		}
		certDER = cert.Certificate[0]
		log.Infof("TLS using certificate from %s", cfg.TLSCert)
	} else {
		// Auto-generate self-signed cert, persisted next to DB
		certPath := filepath.Join(filepath.Dir(cfg.DBPath), "tls-cert.pem")
		keyPath := filepath.Join(filepath.Dir(cfg.DBPath), "tls-key.pem")
		var err error
		cert, err = LoadOrGenerateCert(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("auto TLS cert: %w", err)
		}
		certDER = cert.Certificate[0]
		log.Infof("TLS using auto-generated certificate from %s", certPath)
	}

	log.Infof("TLS fingerprint: %s", CertFingerprint(certDER))

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// Start begins listening on the unix socket (and optionally TCP). It blocks until the server is stopped.
func (s *Server) Start() error {
	// Remove stale socket file
	os.Remove(s.cfg.Daemon.Socket)

	ln, err := net.Listen("unix", s.cfg.Daemon.Socket)
	if err != nil {
		return err
	}
	s.listener = ln

	// Make socket world-accessible (read-only API, no auth needed)
	os.Chmod(s.cfg.Daemon.Socket, 0666)

	log.Infof("API listening on %s", s.cfg.Daemon.Socket)

	// Optional TCP listener for remote TUI connections
	if s.cfg.Daemon.Listen != "" {
		tcpLn, err := net.Listen("tcp", s.cfg.Daemon.Listen)
		if err != nil {
			ln.Close()
			return fmt.Errorf("TCP listen on %s: %w", s.cfg.Daemon.Listen, err)
		}

		// Wrap with TLS unless explicitly disabled
		tlsCfg, tlsErr := s.buildTLSConfig()
		if tlsErr != nil {
			ln.Close()
			tcpLn.Close()
			return fmt.Errorf("TLS config: %w", tlsErr)
		}
		if tlsCfg != nil {
			tcpLn = tls.NewListener(tcpLn, tlsCfg)
			log.Infof("API listening on tls://%s", s.cfg.Daemon.Listen)
		} else {
			log.Infof("API listening on tcp://%s (no TLS)", s.cfg.Daemon.Listen)
		}

		s.tcpListener = tcpLn
		s.tcpSrv = &http.Server{
			Handler: bearerAuth(s.cfg.Daemon.AuthToken, s.mux),
		}
		if s.cfg.Daemon.AuthToken != "" {
			log.Infof("TCP listener requires bearer token authentication")
		}
		go s.tcpSrv.Serve(tcpLn)
	}

	return s.srv.Serve(ln)
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	close(s.done)
	if s.tcpSrv != nil {
		s.tcpSrv.Shutdown(ctx)
	}
	return s.srv.Shutdown(ctx)
}
