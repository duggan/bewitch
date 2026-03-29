package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/log"

	"github.com/duggan/bewitch/internal/alert"
	"github.com/duggan/bewitch/internal/api"
	"github.com/duggan/bewitch/internal/collector"
	"github.com/duggan/bewitch/internal/config"
	"github.com/duggan/bewitch/internal/db"
	"github.com/duggan/bewitch/internal/store"
)

var version = "dev"

func main() {
	log.SetReportTimestamp(true)

	configPath := flag.String("config", "/etc/bewitch.toml", "path to config file")
	logLevel := flag.String("log-level", "", "log level: debug, info, warn, error (overrides config)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("bewitchd", version)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	// Apply log level: CLI flag > config > default (info)
	if *logLevel != "" {
		level, err := log.ParseLevel(*logLevel)
		if err != nil {
			log.Fatalf("invalid log level %q: valid values are debug, info, warn, error", *logLevel)
		}
		log.SetLevel(level)
	} else if cfg.Daemon.LogLevel != "" {
		level, err := log.ParseLevel(cfg.Daemon.LogLevel)
		if err != nil {
			log.Fatalf("invalid log_level in config %q: valid values are debug, info, warn, error", cfg.Daemon.LogLevel)
		}
		log.SetLevel(level)
	}

	if warn := cfg.Daemon.ValidateAuth(); warn != "" {
		log.Warn(warn)
	}

	database, err := db.Open(cfg.Daemon.DBPath, cfg.Daemon.CheckpointThreshold)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	if cfg.Daemon.CheckpointThreshold != "" {
		log.Infof("checkpoint threshold: %s", cfg.Daemon.CheckpointThreshold)
	}

	st := store.New(database)
	defer st.DB().Close()

	// Initialize collectors
	var (
		cpuCollector  collector.Collector
		memCollector  collector.Collector
		diskCollector collector.Collector
		netCollector  collector.Collector
		procCollector collector.ProcessCollectorI
	)

	if cfg.Daemon.Mock {
		log.Infof("mock mode enabled: using synthetic data")

		// Seed historical data so TUI charts have data immediately
		if err := st.SeedMockHistory(1*time.Hour, 5*time.Second); err != nil {
			log.Errorf("seeding mock history: %v", err)
		}
		// Seed demo alert rules and fired alerts
		if err := store.SeedMockAlerts(database); err != nil {
			log.Errorf("seeding mock alerts: %v", err)
		}
		cpuCollector = collector.NewMockCPUCollector()
		memCollector = collector.NewMockMemoryCollector()
		diskCollector = collector.NewMockDiskCollector()
		netCollector = collector.NewMockNetworkCollector()
		procCollector = collector.NewMockProcessCollector(cfg.Collectors.Process.GetMaxProcesses(), cfg.Collectors.Process.Pinned)
	} else {
		var err error
		cpu, err := collector.NewCPUCollector()
		if err != nil {
			log.Fatalf("initializing cpu collector: %v", err)
		}
		cpuCollector = cpu
		mem, err := collector.NewMemoryCollector()
		if err != nil {
			log.Fatalf("initializing memory collector: %v", err)
		}
		memCollector = mem
		diskExcludes := cfg.Collectors.Disk.GetDiskExcludes()
		disk, err := collector.NewDiskCollector(diskExcludes, cfg.Collectors.Disk.GetSMARTInterval())
		if err != nil {
			log.Fatalf("initializing disk collector: %v", err)
		}
		diskCollector = disk
		net, err := collector.NewNetworkCollector()
		if err != nil {
			log.Fatalf("initializing network collector: %v", err)
		}
		netCollector = net
		proc, err := collector.NewProcessCollector(cfg.Collectors.Process.GetMaxProcesses(), cfg.Collectors.Process.Pinned)
		if err != nil {
			log.Fatalf("initializing process collector: %v", err)
		}
		procCollector = proc
	}

	// Set up runtime pins callback (reads TUI-pinned processes from preferences DB)
	procCollector.SetRuntimePinsFunc(func() []string {
		var value string
		if err := database.QueryRow("SELECT value FROM preferences WHERE key = 'pinned_processes'").Scan(&value); err != nil || value == "" {
			return nil
		}
		var pins []string
		json.Unmarshal([]byte(value), &pins)
		return pins
	})

	// Parse archive configuration
	archiveThreshold, err := cfg.Daemon.ArchiveThresholdDuration()
	if err != nil {
		log.Fatalf("invalid archive_threshold config: %v", err)
	}
	archiveInterval, err := cfg.Daemon.ArchiveIntervalDuration()
	if err != nil {
		log.Fatalf("invalid archive_interval config: %v", err)
	}

	// Start API server
	apiServer := api.NewServer(cfg, st.DB)
	apiServer.SetCompactFunc(func() error {
		return st.CompactExclusive(cfg.Daemon.DBPath)
	})
	apiServer.SetSnapshotFunc(func(path string, withSystemTables bool) error {
		return st.SnapshotExclusive(path, cfg.Daemon.ArchivePath, withSystemTables)
	})

	// Configure archive if enabled
	if archiveThreshold > 0 && cfg.Daemon.ArchivePath != "" {
		apiServer.SetArchiveConfig(cfg.Daemon.ArchivePath, archiveThreshold)
		apiServer.SetArchiveFunc(func() error {
			if err := st.ArchiveExclusive(cfg.Daemon.ArchivePath, archiveThreshold); err != nil {
				return err
			}
			log.Infof("post-archive compaction starting")
			if err := st.CompactExclusive(cfg.Daemon.DBPath); err != nil {
				log.Errorf("post-archive compaction error: %v", err)
			}
			apiServer.CreateArchiveViews() // refresh views with new Parquet files
			return nil
		})
		apiServer.SetUnarchiveFunc(func() error {
			if err := st.UnarchiveExclusive(cfg.Daemon.ArchivePath); err != nil {
				return err
			}
			apiServer.CreateArchiveViews() // refresh views (Parquet files now gone)
			return nil
		})
		apiServer.SetArchiveStatusFunc(func() ([]api.ArchiveStatusItem, error) {
			statuses, err := st.GetArchiveStatus()
			if err != nil {
				return nil, err
			}
			items := make([]api.ArchiveStatusItem, len(statuses))
			for i, s := range statuses {
				items[i] = api.ArchiveStatusItem{
					TableName:      s.TableName,
					LastArchivedTS: s.LastArchivedTS,
				}
			}
			return items, nil
		})
		apiServer.SetArchiveDirStatFunc(func() (*api.ArchiveDirStats, error) {
			stats, err := store.GetArchiveDirStats(cfg.Daemon.ArchivePath)
			if err != nil {
				return nil, err
			}
			result := &api.ArchiveDirStats{
				TotalFiles: stats.TotalFiles,
				TotalBytes: stats.TotalBytes,
				Tables:     make(map[string]api.TableArchiveStats),
			}
			for k, v := range stats.Tables {
				result.Tables[k] = api.TableArchiveStats{
					FileCount:  v.FileCount,
					TotalBytes: v.TotalBytes,
					OldestFile: v.OldestFile,
					NewestFile: v.NewestFile,
				}
			}
			return result, nil
		})
	}

	if hints := collector.DetectGPUHints(); len(hints) > 0 {
		apiServer.SetGPUHints(hints)
		for _, h := range hints {
			log.Warnf("GPU: %s", h)
		}
	}

	go func() {
		if err := apiServer.Start(); err != nil && err != http.ErrServerClosed {
			log.Errorf("API server error: %v", err)
		}
	}()

	// Start alert engine
	alertEngine := alert.NewEngine(st.DB, &cfg.Alerts)
	apiServer.SetNotifiers(alertEngine.Notifiers())
	alertEngine.Start()

	// Resolve per-collector intervals
	defaultInterval, err := cfg.Daemon.DefaultCollectionInterval()
	if err != nil {
		log.Fatalf("invalid default_interval config: %v", err)
	}

	type scheduledCollector struct {
		collector        collector.Collector
		interval         time.Duration
		tickMod          int // fires when tickCount % tickMod == 0
		consecutiveFails int // reset to 0 on success
		skipUntilTick    int // skip collection if tickCount < this
	}

	var eccCollector collector.Collector
	if cfg.Daemon.Mock {
		eccCollector = collector.NewMockECCCollector()
	} else {
		eccCollector = collector.NewECCCollector()
	}

	scheduled := []scheduledCollector{
		{collector: cpuCollector, interval: cfg.Collectors.CPU.GetInterval(defaultInterval)},
		{collector: memCollector, interval: cfg.Collectors.Memory.GetInterval(defaultInterval)},
		{collector: diskCollector, interval: cfg.Collectors.Disk.GetInterval(defaultInterval)},
		{collector: netCollector, interval: cfg.Collectors.Network.GetInterval(defaultInterval)},
		{collector: eccCollector, interval: cfg.Collectors.ECC.GetInterval(defaultInterval)},
	}
	if cfg.Daemon.Mock || cfg.Collectors.Temperature.IsEnabled() {
		var tempCollector collector.Collector
		if cfg.Daemon.Mock {
			tempCollector = collector.NewMockTemperatureCollector()
		} else {
			tempCollector = collector.NewTemperatureCollector()
		}
		scheduled = append(scheduled, scheduledCollector{
			collector: tempCollector,
			interval:  cfg.Collectors.Temperature.GetInterval(defaultInterval),
		})
	}
	if cfg.Daemon.Mock || cfg.Collectors.Power.IsEnabled() {
		var powerCollector collector.Collector
		if cfg.Daemon.Mock {
			powerCollector = collector.NewMockPowerCollector()
		} else {
			powerCollector = collector.NewPowerCollector()
		}
		scheduled = append(scheduled, scheduledCollector{
			collector: powerCollector,
			interval:  cfg.Collectors.Power.GetInterval(defaultInterval),
		})
	}
	if cfg.Daemon.Mock || cfg.Collectors.GPU.IsEnabled() {
		var gpuCollector collector.Collector
		if cfg.Daemon.Mock {
			gpuCollector = collector.NewMockGPUCollector()
		} else {
			gpuCollector = collector.NewGPUCollector()
		}
		scheduled = append(scheduled, scheduledCollector{
			collector: gpuCollector,
			interval:  cfg.Collectors.GPU.GetInterval(defaultInterval),
		})
	}
	scheduled = append(scheduled, scheduledCollector{
		collector: procCollector,
		interval:  cfg.Collectors.Process.GetInterval(defaultInterval),
	})

	// Compute GCD of all intervals to determine tick rate
	gcdDuration := func(a, b time.Duration) time.Duration {
		for b != 0 {
			a, b = b, a%b
		}
		return a
	}
	tickInterval := scheduled[0].interval
	for _, sc := range scheduled[1:] {
		tickInterval = gcdDuration(tickInterval, sc.interval)
	}

	// Initialize tick modulos (all fire on first tick: tickCount=0, 0%n==0)
	for i := range scheduled {
		scheduled[i].tickMod = int(scheduled[i].interval / tickInterval)
	}

	// Log per-collector intervals and push to API server
	{
		var parts string
		intervals := make(map[string]string, len(scheduled)+1)
		intervals["tick"] = tickInterval.String()
		for i, sc := range scheduled {
			if i > 0 {
				parts += " "
			}
			parts += sc.collector.Name() + "=" + sc.interval.String()
			intervals[sc.collector.Name()] = sc.interval.String()
		}
		log.Infof("bewitchd starting, tick=%v (%s)", tickInterval, parts)
		apiServer.SetCollectorIntervals(intervals)
	}

	// Parse retention duration for periodic pruning
	retention, err := cfg.Daemon.RetentionDuration()
	if err != nil {
		log.Fatalf("invalid retention config: %v", err)
	}
	if retention > 0 {
		log.Infof("data retention: %v", retention)
	}

	// Parse prune interval
	pruneInterval, err := cfg.Daemon.PruneDuration()
	if err != nil {
		log.Fatalf("invalid prune_interval config: %v", err)
	}
	if retention > 0 {
		log.Infof("prune interval: %v", pruneInterval)
	}

	// Parse compaction interval (0 = disabled)
	compactionInterval, err := cfg.Daemon.CompactionDuration()
	if err != nil {
		log.Fatalf("invalid compaction_interval config: %v", err)
	}
	if compactionInterval > 0 {
		log.Infof("compaction interval: %v", compactionInterval)
	}

	// Start periodic data pruning if retention is configured
	if retention > 0 {
		runScheduledJob(st, "prune", pruneInterval, func() error {
			return st.PruneExclusive(retention)
		})
	}

	// Start periodic compaction if configured
	if compactionInterval > 0 {
		runScheduledJob(st, "compact", compactionInterval, func() error {
			return st.CompactExclusive(cfg.Daemon.DBPath)
		})
	}

	// Start periodic checkpointing if configured (for crash safety)
	checkpointInterval, err := cfg.Daemon.CheckpointDuration()
	if err != nil {
		log.Fatalf("invalid checkpoint_interval config: %v", err)
	}
	if checkpointInterval > 0 {
		log.Infof("checkpoint interval: %v", checkpointInterval)
		go func() {
			ticker := time.NewTicker(checkpointInterval)
			defer ticker.Stop()
			for range ticker.C {
				log.Debugf("checkpoint starting")
				if checkpointErr := st.Checkpoint(); checkpointErr != nil {
					log.Errorf("checkpoint error: %v", checkpointErr)
				}
			}
		}()
	}

	// Start periodic archiving if configured
	if archiveThreshold > 0 && cfg.Daemon.ArchivePath != "" {
		log.Infof("archive threshold: %v, interval: %v, path: %s", archiveThreshold, archiveInterval, cfg.Daemon.ArchivePath)
		runScheduledJob(st, "archive", archiveInterval, func() error {
			if err := st.ArchiveExclusive(cfg.Daemon.ArchivePath, archiveThreshold); err != nil {
				return err
			}
			if retention > 0 {
				if pruneErr := st.PruneArchiveExclusive(cfg.Daemon.ArchivePath, retention); pruneErr != nil {
					log.Errorf("archive prune error: %v", pruneErr)
				}
			}
			log.Infof("post-archive compaction starting")
			if err := st.CompactExclusive(cfg.Daemon.DBPath); err != nil {
				log.Errorf("post-archive compaction error: %v", err)
			}
			apiServer.CreateArchiveViews() // refresh views with new Parquet files
			return nil
		})
	}

	// Decoupled write pipeline: collectors push samples to a buffered channel,
	// a writer goroutine drains it asynchronously. This ensures slow DB writes
	// never delay the next collection tick or the API cache push.
	writeCh := make(chan []collector.Sample, 8)
	var writeWg sync.WaitGroup
	writeWg.Add(1)
	go func() {
		defer writeWg.Done()
		for samples := range writeCh {
			if err := st.WriteBatch(samples); err != nil {
				log.Errorf("batch write error: %v", err)
			}
		}
	}()

	const maxBackoffMultiplier = 64 // cap exponential backoff at 64× the collector's interval

	// collectTick runs all collectors due on this tick, writes to DB,
	// and pushes snapshots to the API cache.
	collectTick := func(tickCount int) {
		var wg sync.WaitGroup
		samples := make([]collector.Sample, len(scheduled))
		errors := make([]error, len(scheduled))
		ran := make([]bool, len(scheduled)) // tracks which collectors actually ran

		for i := range scheduled {
			if tickCount%scheduled[i].tickMod != 0 {
				continue
			}
			if tickCount < scheduled[i].skipUntilTick {
				continue // in backoff
			}
			ran[i] = true
			wg.Add(1)
			go func(idx int, col collector.Collector) {
				defer wg.Done()
				sample, err := col.Collect()
				samples[idx] = sample
				errors[idx] = err
				// Push to API cache immediately so fast collectors
				// don't wait for slow ones (disk/SMART, process, GPU).
				if err == nil {
					pushSampleToCache(apiServer, procCollector, sample)
				}
			}(i, scheduled[i].collector)
		}
		wg.Wait()

		// Handle collection errors with exponential backoff
		for i := range scheduled {
			if !ran[i] {
				continue
			}
			sc := &scheduled[i]
			if errors[i] != nil {
				sc.consecutiveFails++
				backoff := min(1<<(sc.consecutiveFails-1), maxBackoffMultiplier)
				sc.skipUntilTick = tickCount + backoff*sc.tickMod
				if sc.consecutiveFails == 1 {
					log.Errorf("collector %s error: %v", sc.collector.Name(), errors[i])
				} else {
					log.Errorf("collector %s error (attempt %d, backing off %d ticks): %v",
						sc.collector.Name(), sc.consecutiveFails, backoff*sc.tickMod, errors[i])
				}
			} else if sc.consecutiveFails > 0 {
				log.Infof("collector %s recovered after %d consecutive failures",
					sc.collector.Name(), sc.consecutiveFails)
				sc.consecutiveFails = 0
				sc.skipUntilTick = 0
			}
		}

		// Enqueue samples for async DB write
		select {
		case writeCh <- samples:
		default:
			log.Warnf("write queue full, dropping batch")
		}
	}

	// Run initial collection so the API cache is populated before any client connects.
	collectTick(0)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	tickCount := 1 // 0 was the initial collection above
	for {
		select {
		case <-ticker.C:
			collectTick(tickCount)
			tickCount++
		case sig := <-sigCh:
			log.Infof("received %v, shutting down", sig)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			alertEngine.Stop()
			// Stop long-lived collector subprocesses (e.g. intel_gpu_top)
			for _, sc := range scheduled {
				type stopper interface{ Stop() }
				if s, ok := sc.collector.(stopper); ok {
					s.Stop()
				}
			}
			apiServer.Shutdown(ctx)
			close(writeCh)
			writeWg.Wait() // drain remaining batches before DB close
			return
		}
	}
}

// buildProcessSnapshot merges enriched (Phase 2) process data with lightweight
// (Phase 1) data for all remaining processes into a single API response.
// Enriched processes have full details; non-enriched have basic stats only.
func buildProcessSnapshot(pd *collector.ProcessData, allBasic []collector.ProcessBasicInfo) *api.ProcessResponse {
	// Index enriched PIDs for fast lookup
	enrichedPIDs := make(map[int32]int, len(pd.Processes))
	for i, p := range pd.Processes {
		enrichedPIDs[p.PID] = i
	}

	procs := make([]api.ProcessMetric, 0, len(allBasic))

	// Add enriched processes first (sorted by CPU in collector)
	for _, p := range pd.Processes {
		procs = append(procs, api.ProcessMetric{
			PID:          p.PID,
			PPID:         p.PPID,
			Name:         p.Name,
			Cmdline:      p.Cmdline,
			State:        p.State,
			UID:          p.UID,
			CPUUserPct:   p.CPUUserPct,
			CPUSystemPct: p.CPUSystemPct,
			RSSBytes:     p.RSSBytes,
			VSSBytes:     p.VSSBytes,
			SharedBytes:  p.SharedBytes,
			SwapBytes:    p.SwapBytes,
			NumFDs:       p.NumFDs,
			NumThreads:   p.NumThreads,
			StartTimeNs:  p.StartTime,
			Enriched:     true,
		})
	}

	// Add non-enriched processes (lightweight Phase 1 data)
	for _, b := range allBasic {
		if _, ok := enrichedPIDs[b.PID]; ok {
			continue // Already added as enriched
		}
		procs = append(procs, api.ProcessMetric{
			PID:          b.PID,
			Name:         b.Name,
			State:        b.State,
			CPUUserPct:   b.CPUPct,
			RSSBytes:     b.RSSBytes,
			NumThreads:   b.NumThreads,
			StartTimeNs:  b.StartTime,
			Enriched:     false,
		})
	}

	return &api.ProcessResponse{
		Processes:     procs,
		TotalProcs:    pd.TotalProcs,
		RunningProcs:  pd.RunningProcs,
		ActiveProcs:   pd.ActiveProcs,
		TotalCPUPct:   pd.TotalCPUPct,
		TotalRSSBytes: pd.TotalRSSBytes,
	}
}

// pushSampleToCache converts a single collected sample to its API type and
// pushes it to the API server cache immediately. Called from each collector
// goroutine so fast collectors (CPU, memory) don't wait for slow ones
// (disk/SMART, process, GPU). Thread-safe: SetMetricsSnapshot and friends
// use internal mutexes.
func pushSampleToCache(srv *api.Server, procCol collector.ProcessCollectorI, s collector.Sample) {
	if s.Data == nil {
		return
	}
	switch d := s.Data.(type) {
	case collector.CPUData:
		cpu := make([]api.CPUCoreMetric, len(d.Cores))
		for i, c := range d.Cores {
			cpu[i] = api.CPUCoreMetric{
				Core: c.Core, UserPct: c.UserPct,
				SystemPct: c.SystemPct, IdlePct: c.IdlePct,
				IOWaitPct: c.IOWaitPct,
			}
		}
		srv.SetMetricsSnapshot(cpu, nil, nil, nil, nil, nil, nil)
	case collector.MemoryData:
		mem := &api.MemoryMetric{
			TotalBytes: d.TotalBytes, UsedBytes: d.UsedBytes,
			AvailableBytes: d.AvailableBytes, BuffersBytes: d.BuffersBytes,
			CachedBytes: d.CachedBytes, SwapTotalBytes: d.SwapTotalBytes,
			SwapUsedBytes: d.SwapUsedBytes,
		}
		srv.SetMetricsSnapshot(nil, mem, nil, nil, nil, nil, nil)
	case collector.DiskData:
		disks := make([]api.DiskMetric, len(d.Mounts))
		for i, m := range d.Mounts {
			disks[i] = api.DiskMetric{
				Mount: m.Mount, Device: m.Device, Transport: m.Transport,
				TotalBytes: m.TotalBytes, UsedBytes: m.UsedBytes,
				FreeBytes: m.FreeBytes, ReadBytesSec: m.ReadBytesSec,
				WriteBytesSec: m.WriteBytesSec, ReadIOPS: m.ReadIOPS,
				WriteIOPS: m.WriteIOPS,
			}
			if m.SMART != nil && m.SMART.Available {
				disks[i].SMARTAvailable = true
				disks[i].SMARTHealthy = m.SMART.Healthy
				disks[i].SMARTTemperature = m.SMART.Temperature
				disks[i].SMARTPowerOnHours = m.SMART.PowerOnHours
				disks[i].SMARTPowerCycles = m.SMART.PowerCycles
				disks[i].SMARTReadSectors = m.SMART.ReadSectors
				disks[i].SMARTWrittenSectors = m.SMART.WrittenSectors
				disks[i].SMARTReallocated = m.SMART.ReallocatedSectors
				disks[i].SMARTPending = m.SMART.PendingSectors
				disks[i].SMARTUncorrectable = m.SMART.UncorrectableErrs
				disks[i].SMARTReadErrorRate = m.SMART.ReadErrorRate
				disks[i].SMARTAvailableSpare = m.SMART.AvailableSpare
				disks[i].SMARTPercentUsed = m.SMART.PercentUsed
			}
		}
		srv.SetMetricsSnapshot(nil, nil, disks, nil, nil, nil, nil)
	case collector.NetworkData:
		net := make([]api.NetworkMetric, len(d.Interfaces))
		for i, n := range d.Interfaces {
			net[i] = api.NetworkMetric{
				Interface: n.Interface, RxBytesSec: n.RxBytesSec,
				TxBytesSec: n.TxBytesSec, RxPacketsSec: n.RxPacketsSec,
				TxPacketsSec: n.TxPacketsSec, RxErrors: n.RxErrors,
				TxErrors: n.TxErrors,
			}
		}
		srv.SetMetricsSnapshot(nil, nil, nil, net, nil, nil, nil)
	case collector.TemperatureData:
		temps := make([]api.TemperatureMetric, len(d.Sensors))
		for i, t := range d.Sensors {
			temps[i] = api.TemperatureMetric{
				Sensor: t.Sensor, TempCelsius: t.TempCelsius,
			}
		}
		srv.SetMetricsSnapshot(nil, nil, nil, nil, temps, nil, nil)
	case collector.PowerData:
		power := make([]api.PowerMetric, len(d.Zones))
		for i, z := range d.Zones {
			power[i] = api.PowerMetric{
				Zone: z.Zone, Watts: z.Watts,
			}
		}
		srv.SetMetricsSnapshot(nil, nil, nil, nil, nil, power, nil)
	case collector.ECCData:
		ecc := &api.ECCMetric{
			Corrected: d.Corrected, Uncorrected: d.Uncorrected,
		}
		srv.SetMetricsSnapshot(nil, nil, nil, nil, nil, nil, ecc)
	case collector.GPUData:
		gpus := make([]api.GPUMetric, len(d.GPUs))
		for i, g := range d.GPUs {
			gpus[i] = api.GPUMetric{
				Name: g.Name, Index: g.Index, Vendor: g.Vendor,
				UtilizationPct: g.UtilizationPct,
				MemoryUsedBytes: g.MemoryUsedBytes, MemoryTotalBytes: g.MemoryTotalBytes,
				TempCelsius: g.TempCelsius, PowerWatts: g.PowerWatts,
				FrequencyMHz: g.FrequencyMHz, FrequencyMaxMHz: g.FrequencyMaxMHz,
				ThrottlePct: g.ThrottlePct,
			}
		}
		srv.SetGPUSnapshot(gpus)
	case collector.ProcessData:
		allBasic := procCol.AllProcessSnapshot()
		snap := buildProcessSnapshot(&d, allBasic)
		srv.SetProcessSnapshot(snap)
	}
}

// runScheduledJob starts a goroutine that checks if jobName is overdue,
// runs it immediately if so, then ticks at interval.
func runScheduledJob(st *store.Store, jobName string, interval time.Duration, run func() error) {
	go func() {
		overdue, err := st.IsJobOverdue(jobName, interval)
		if err != nil {
			log.Warnf("%s schedule check: %v", jobName, err)
		}
		if overdue {
			log.Infof("%s overdue, running now", jobName)
			if err := run(); err != nil {
				log.Errorf("%s error: %v", jobName, err)
			} else {
				if recErr := st.RecordJobRun(jobName); recErr != nil {
					log.Errorf("record %s run: %v", jobName, recErr)
				}
			}
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			log.Infof("scheduled %s starting", jobName)
			if err := run(); err != nil {
				log.Errorf("%s error: %v", jobName, err)
			} else {
				if recErr := st.RecordJobRun(jobName); recErr != nil {
					log.Errorf("record %s run: %v", jobName, recErr)
				}
			}
		}
	}()
}
