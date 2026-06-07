package collector

import (
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/procfs"
)

// ProcessSample represents metrics for a single process.
type ProcessSample struct {
	PID          int32
	PPID         int32
	Name         string
	Cmdline      string
	State        string
	UID          uint32
	CPUUserPct   float64
	CPUSystemPct float64
	RSSBytes     uint64
	VSSBytes     uint64
	SharedBytes  uint64
	SwapBytes    uint64
	NumFDs       int32
	NumThreads   int32
	StartTime    int64 // unix nanoseconds
	// Disk I/O rates (bytes/sec) from /proc/[pid]/io read_bytes/write_bytes — actual
	// storage I/O, not page-cache (rchar/wchar). Phase-2 enriched only; 0 when the
	// daemon lacks permission to read another user's /proc/[pid]/io (needs root or
	// CAP_SYS_PTRACE) or on the first sample for a process.
	ReadBytesSec  float64
	WriteBytesSec float64
}

// ProcessData contains all process metrics from a single collection.
type ProcessData struct {
	Processes     []ProcessSample
	TotalProcs    int32
	RunningProcs  int32
	ActiveProcs   int32
	TotalCPUPct   float64
	TotalRSSBytes uint64
}

// ProcessBasicInfo holds lightweight data from Phase 1 for all processes.
// This is exported for the API to serve a full process list without DB overhead.
type ProcessBasicInfo struct {
	PID        int32
	Name       string // stat.Comm
	State      string
	CPUPct     float64 // user + system
	RSSBytes   uint64
	NumThreads int32
	StartTime  int64 // unix nanoseconds
}

// procTimes holds CPU time values for delta calculation.
type procTimes struct {
	utime uint64
	stime uint64
}

// cachedCmdline holds cached command line with refresh tracking.
type cachedCmdline struct {
	cmdline   string
	fetchedAt time.Time
}

// procBasic holds minimal data from /proc/[pid]/stat for fast first-pass sorting.
type procBasic struct {
	proc    procfs.Proc
	stat    procfs.ProcStat
	cpuUser float64
	cpuSys  float64
	rss     uint64 // from stat.RSS * page size
	state   string
	score   float64 // sorting score
}

// ProcessCollector collects process metrics via /proc.
type ProcessCollector struct {
	fs           procfs.FS
	prev         map[int]procTimes     // PID -> previous CPU times
	prevIO       map[int]procfs.ProcIO // PID -> previous I/O counters (enriched set only), for rates
	cmdlineCache map[int]cachedCmdline // PID -> cached cmdline
	prevTime     time.Time
	maxProcs     int
	bootTime     uint64          // system boot time in seconds since epoch
	clockTick    int64           // system clock ticks per second
	pageSize     int64           // system page size in bytes
	samples      []ProcessSample // reusable slice
	basics       []procBasic     // reusable slice for first pass

	// Pinning
	configPins    []string        // Glob patterns from config file
	runtimePinsFn func() []string // Callback to fetch runtime pins from preferences

	// All-process snapshot (Phase 1 data served to API)
	allProcs []ProcessBasicInfo
	mu       sync.RWMutex
}

// cmdlineCacheTTL is how long we cache command lines before refreshing.
// Command lines rarely change for long-running processes.
const cmdlineCacheTTL = 60 * time.Second

// NewProcessCollector creates a new process collector.
// maxProcs limits how many processes are tracked per sample (top N by resource usage).
// configPins are glob patterns of process names to always enrich and store.
func NewProcessCollector(maxProcs int, configPins []string) (*ProcessCollector, error) {
	fs, err := newProcFS()
	if err != nil {
		return nil, err
	}

	// Get system boot time for calculating process start times
	stat, err := fs.Stat()
	if err != nil {
		return nil, fmt.Errorf("reading /proc/stat: %w", err)
	}

	// Default to 100 if not specified
	if maxProcs <= 0 {
		maxProcs = 100
	}

	return &ProcessCollector{
		fs:           fs,
		prev:         make(map[int]procTimes),
		prevIO:       make(map[int]procfs.ProcIO),
		cmdlineCache: make(map[int]cachedCmdline),
		prevTime:     time.Now(),
		maxProcs:     maxProcs,
		bootTime:     stat.BootTime,
		clockTick:    100,  // Standard Linux default (sysconf(_SC_CLK_TCK))
		pageSize:     4096, // Standard Linux page size
		samples:      make([]ProcessSample, 0, maxProcs),
		basics:       make([]procBasic, 0, 512), // Pre-allocate for typical system
		configPins:   configPins,
	}, nil
}

// SetRuntimePinsFunc sets a callback to fetch runtime pinned process patterns.
// Called once per collection cycle to get the latest pins from preferences.
func (c *ProcessCollector) SetRuntimePinsFunc(fn func() []string) {
	c.runtimePinsFn = fn
}

// AllProcessSnapshot returns the latest lightweight snapshot of all processes.
// Safe for concurrent access from the API server.
func (c *ProcessCollector) AllProcessSnapshot() []ProcessBasicInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]ProcessBasicInfo, len(c.allProcs))
	copy(out, c.allProcs)
	return out
}

func (c *ProcessCollector) Name() string { return "process" }

func (c *ProcessCollector) Collect() (Sample, error) {
	now := time.Now()
	dt := now.Sub(c.prevTime).Seconds()

	procs, err := c.fs.AllProcs()
	if err != nil {
		return Sample{}, fmt.Errorf("reading /proc: %w", err)
	}

	// === PHASE 1: Fast pass - read only /proc/[pid]/stat for all processes ===
	// This gives us CPU times, memory (RSS pages), state, and allows sorting.
	// We skip expensive reads (cmdline, status, fd) until we know which processes matter.

	c.basics = c.basics[:0] // Reuse slice
	var totalRunning int32
	var totalActive int32
	var totalCPU float64
	var totalRSS uint64
	newPrev := make(map[int]procTimes, len(procs))

	for _, proc := range procs {
		stat, err := proc.Stat()
		if err != nil {
			continue
		}

		// Calculate CPU percentages from delta
		var cpuUser, cpuSys float64
		utime := uint64(stat.UTime)
		stime := uint64(stat.STime)
		if prev, ok := c.prev[proc.PID]; ok && dt > 0 {
			uDelta := float64(utime-prev.utime) / float64(c.clockTick)
			sDelta := float64(stime-prev.stime) / float64(c.clockTick)
			cpuUser = (uDelta / dt) * 100
			cpuSys = (sDelta / dt) * 100
		}

		// RSS from stat (in pages, convert to bytes)
		rss := uint64(stat.RSS) * uint64(c.pageSize)

		// Track totals
		if stat.State == "R" {
			totalRunning++
		}
		if cpuUser+cpuSys > 0 {
			totalActive++
		}
		totalCPU += cpuUser + cpuSys
		totalRSS += rss

		// Store for next delta calculation
		newPrev[proc.PID] = procTimes{utime: utime, stime: stime}

		// Calculate sorting score (CPU weighted + memory normalized)
		score := cpuUser + cpuSys + float64(rss)/(1024*1024*1024)*10

		c.basics = append(c.basics, procBasic{
			proc:    proc,
			stat:    stat,
			cpuUser: cpuUser,
			cpuSys:  cpuSys,
			rss:     rss,
			state:   stat.State,
			score:   score,
		})
	}

	// Build lightweight snapshot of all processes before sorting/limiting.
	allSnap := make([]ProcessBasicInfo, len(c.basics))
	for i, b := range c.basics {
		startTimeSec := c.bootTime + uint64(b.stat.Starttime)/uint64(c.clockTick)
		allSnap[i] = ProcessBasicInfo{
			PID:        int32(b.proc.PID),
			Name:       b.stat.Comm,
			State:      b.state,
			CPUPct:     b.cpuUser + b.cpuSys,
			RSSBytes:   b.rss,
			NumThreads: int32(b.stat.NumThreads),
			StartTime:  int64(startTimeSec) * 1e9,
		}
	}
	c.mu.Lock()
	c.allProcs = allSnap
	c.mu.Unlock()

	// Sort by score descending
	sort.Slice(c.basics, func(i, j int) bool {
		return c.basics[i].score > c.basics[j].score
	})

	// Collect pinned patterns (config + runtime)
	allPins := c.configPins
	if c.runtimePinsFn != nil {
		if runtimePins := c.runtimePinsFn(); len(runtimePins) > 0 {
			allPins = append(append([]string(nil), c.configPins...), runtimePins...)
		}
	}

	// Build enrichment set: top N + pinned processes (deduped)
	var topN []procBasic
	if len(allPins) > 0 {
		pinnedPIDs := make(map[int]bool)
		commMatched := make(map[string]bool, len(allPins)) // pins satisfied by a comm match
		for _, b := range c.basics {
			for _, p := range allPins {
				if matched, _ := filepath.Match(p, b.stat.Comm); matched {
					commMatched[p] = true
					if !pinnedPIDs[b.proc.PID] {
						topN = append(topN, b)
						pinnedPIDs[b.proc.PID] = true
					}
				}
			}
		}

		// Pins that no process's comm satisfied are likely cmdline globs (a rule's
		// ProcessPattern): comm is only the 15-char basename, so the comm pass can't
		// catch them. Re-check those against each process's full cmdline using an
		// anchored glob — the same semantics the alert engine's SQL LIKE uses — so a
		// ProcessPattern rule's target actually gets force-enriched and recorded.
		// Cmdline is read through the same cache Phase-2 uses (60s TTL), so the cost
		// is amortized; the whole pass is gated on having unsatisfied pins, so the
		// common comm-only case reads no cmdlines here.
		var cmdlinePins []string
		for _, p := range allPins {
			if !commMatched[p] {
				cmdlinePins = append(cmdlinePins, p)
			}
		}
		if len(cmdlinePins) > 0 {
			for _, b := range c.basics {
				if pinnedPIDs[b.proc.PID] {
					continue
				}
				cmd := c.getCmdline(b.proc, now)
				if cmd == "" {
					continue
				}
				for _, p := range cmdlinePins {
					if globMatchAny(p, cmd) {
						topN = append(topN, b)
						pinnedPIDs[b.proc.PID] = true
						break
					}
				}
			}
		}

		count := 0
		for _, b := range c.basics {
			if count >= c.maxProcs {
				break
			}
			if !pinnedPIDs[b.proc.PID] {
				topN = append(topN, b)
				count++
			}
		}
	} else {
		topN = c.basics
		if len(topN) > c.maxProcs {
			topN = topN[:c.maxProcs]
		}
	}

	// === PHASE 2: Enrich only selected processes with expensive data ===
	// Now we read cmdline, status, and fd count only for processes we'll actually store.

	c.samples = c.samples[:0] // Reuse slice
	// prevIO is rebuilt from this cycle's enriched set, which auto-evicts processes
	// that dropped out of enrichment or died (no separate stale sweep needed).
	newPrevIO := make(map[int]procfs.ProcIO, len(topN))
	for _, b := range topN {
		ps := c.enrichProc(b, now)
		// Disk I/O rates (Phase-2 only — one /proc/[pid]/io read per enriched proc).
		// Reading another user's /proc/[pid]/io needs CAP_SYS_PTRACE (the debian unit
		// grants it); without it proc.IO() returns EACCES, which we leave as rate 0
		// without logging. The first sample for a process also has no prev.
		if io, err := b.proc.IO(); err == nil {
			newPrevIO[b.proc.PID] = io
			if prev, ok := c.prevIO[b.proc.PID]; ok && dt > 0 {
				if d := int64(io.ReadBytes) - int64(prev.ReadBytes); d > 0 {
					ps.ReadBytesSec = float64(d) / dt
				}
				if d := int64(io.WriteBytes) - int64(prev.WriteBytes); d > 0 {
					ps.WriteBytesSec = float64(d) / dt
				}
			}
		}
		c.samples = append(c.samples, ps)
	}

	// Clean up stale cmdline cache entries (processes that no longer exist)
	c.cleanCmdlineCache(newPrev)

	c.prev = newPrev
	c.prevIO = newPrevIO
	c.prevTime = now

	return Sample{
		Timestamp: now,
		Kind:      "process",
		Data: ProcessData{
			Processes:     c.samples,
			TotalProcs:    int32(len(procs)),
			RunningProcs:  totalRunning,
			ActiveProcs:   totalActive,
			TotalCPUPct:   totalCPU,
			TotalRSSBytes: totalRSS,
		},
	}, nil
}

// memoryFromStatus extracts the memory breakdown from /proc/[pid]/status.
// prometheus/procfs already converts these fields from kB to bytes when parsing
// (see procfs proc_status.go), so they must be used AS-IS. A previous bug
// multiplied each by 1024, inflating every stored RSS value 1024× (~1 TB reported
// for a ~1 GB process). Keep this conversion-free; the test guards it.
func memoryFromStatus(status procfs.ProcStatus) (rss, vss, shared, swap uint64) {
	return status.VmRSS, status.VmSize, status.VmLib, status.VmSwap
}

// enrichProc adds expensive data (cmdline, status, fd count) to a process.
// Called only for top N processes after the fast sorting pass.
func (c *ProcessCollector) enrichProc(b procBasic, now time.Time) ProcessSample {
	stat := b.stat
	proc := b.proc

	// Get command line with caching
	cmdlineStr := c.getCmdline(proc, now)

	// Get UID and detailed memory from /proc/[pid]/status
	var uid uint32
	var vss, shared, swap uint64
	rss := b.rss // Use RSS from stat by default
	if status, err := proc.NewStatus(); err == nil {
		uid = uint32(status.UIDs[0])
		rss, vss, shared, swap = memoryFromStatus(status)
	}

	// Get FD count (requires readdir on /proc/[pid]/fd)
	var numFDs int32
	if fds, err := proc.FileDescriptorsLen(); err == nil {
		numFDs = int32(fds)
	}

	// Calculate start time as unix nanoseconds
	startTimeSec := c.bootTime + uint64(stat.Starttime)/uint64(c.clockTick)
	startTimeNs := int64(startTimeSec) * 1e9

	return ProcessSample{
		PID:          int32(proc.PID),
		PPID:         int32(stat.PPID),
		Name:         stat.Comm,
		Cmdline:      cmdlineStr,
		State:        b.state,
		UID:          uid,
		CPUUserPct:   b.cpuUser,
		CPUSystemPct: b.cpuSys,
		RSSBytes:     rss,
		VSSBytes:     vss,
		SharedBytes:  shared,
		SwapBytes:    swap,
		NumFDs:       numFDs,
		NumThreads:   int32(stat.NumThreads),
		StartTime:    startTimeNs,
	}
}

// getCmdline returns the command line for a process, using cache when possible.
func (c *ProcessCollector) getCmdline(proc procfs.Proc, now time.Time) string {
	pid := proc.PID

	// Check cache
	if cached, ok := c.cmdlineCache[pid]; ok {
		if now.Sub(cached.fetchedAt) < cmdlineCacheTTL {
			return cached.cmdline
		}
	}

	// Fetch fresh
	cmdline, _ := proc.CmdLine()
	var cmdlineStr string
	if len(cmdline) > 0 {
		// Use strings.Join for efficiency instead of loop concatenation
		for i, arg := range cmdline {
			if i > 0 {
				cmdlineStr += " "
			}
			cmdlineStr += arg
		}
		if len(cmdlineStr) > 256 {
			cmdlineStr = cmdlineStr[:256]
		}
	}

	// Cache it
	c.cmdlineCache[pid] = cachedCmdline{
		cmdline:   cmdlineStr,
		fetchedAt: now,
	}

	return cmdlineStr
}

// cleanCmdlineCache removes entries for PIDs that no longer exist.
func (c *ProcessCollector) cleanCmdlineCache(currentPIDs map[int]procTimes) {
	for pid := range c.cmdlineCache {
		if _, exists := currentPIDs[pid]; !exists {
			delete(c.cmdlineCache, pid)
		}
	}
}

// globMatchAny reports whether s matches the shell-style pattern, where '*' matches
// any run of characters INCLUDING '/' and '?' matches exactly one. Unlike
// filepath.Match it is not path-aware, and the match is anchored (the whole string
// must match) — together that mirrors the SQL LIKE semantics the alert engine uses
// for cmdline ProcessPattern globs, so a pattern enriches the same processes the
// rule will later count. Byte-based two-pointer wildcard matcher (no regexp/alloc).
func globMatchAny(pattern, s string) bool {
	var px, sx int
	starPx, starSx := -1, -1
	for sx < len(s) {
		switch {
		case px < len(pattern) && (pattern[px] == '?' || pattern[px] == s[sx]):
			px++
			sx++
		case px < len(pattern) && pattern[px] == '*':
			starPx, starSx = px, sx
			px++
		case starPx != -1:
			px = starPx + 1
			starSx++
			sx = starSx
		default:
			return false
		}
	}
	for px < len(pattern) && pattern[px] == '*' {
		px++
	}
	return px == len(pattern)
}

// Ensure ProcessCollector implements Collector
var _ Collector = (*ProcessCollector)(nil)
