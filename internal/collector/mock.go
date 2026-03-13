package collector

import (
	"math"
	"math/rand"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// smoothWave returns a value oscillating between min and max using a sine wave
// with the given period (seconds) and phase offset, plus small random jitter.
func smoothWave(min, max, periodSec, phase float64) float64 {
	t := float64(time.Now().UnixNano()) / 1e9
	base := (math.Sin(t*2*math.Pi/periodSec+phase) + 1) / 2 // [0, 1]
	noise := (rand.Float64() - 0.5) * 0.05                   // +/- 2.5%
	val := min + (max-min)*(base+noise)
	return math.Max(min, math.Min(max, val))
}

// --- CPU ---

type MockCPUCollector struct{}

func NewMockCPUCollector() *MockCPUCollector { return &MockCPUCollector{} }
func (c *MockCPUCollector) Name() string     { return "cpu" }

func (c *MockCPUCollector) Collect() (Sample, error) {
	const numCores = 8
	cores := make([]CPUCoreSample, 0, numCores+1)

	// Aggregate
	user := smoothWave(5, 25, 120, 0)
	sys := smoothWave(2, 10, 90, 1.0)
	iow := smoothWave(0, 3, 200, 2.0)
	idle := math.Max(0, 100-user-sys-iow)
	cores = append(cores, CPUCoreSample{Core: -1, UserPct: user, SystemPct: sys, IdlePct: idle, IOWaitPct: iow})

	// Per-core with varied phases
	for i := 0; i < numCores; i++ {
		phase := float64(i) * 0.7
		u := smoothWave(2, 40, 60+float64(i)*30, phase)
		s := smoothWave(1, 12, 80+float64(i)*20, phase+0.5)
		w := smoothWave(0, 4, 150, phase+1.0)
		id := math.Max(0, 100-u-s-w)
		cores = append(cores, CPUCoreSample{Core: i, UserPct: u, SystemPct: s, IdlePct: id, IOWaitPct: w})
	}

	return Sample{Timestamp: time.Now(), Kind: "cpu", Data: CPUData{Cores: cores}}, nil
}

// --- Memory ---

type MockMemoryCollector struct{}

func NewMockMemoryCollector() *MockMemoryCollector { return &MockMemoryCollector{} }
func (c *MockMemoryCollector) Name() string        { return "memory" }

func (c *MockMemoryCollector) Collect() (Sample, error) {
	const total = 32 * 1024 * 1024 * 1024   // 32 GB
	const swapTotal = 8 * 1024 * 1024 * 1024 // 8 GB

	usedPct := smoothWave(0.40, 0.65, 600, 0)
	used := uint64(float64(total) * usedPct)
	buffers := uint64(float64(total) * smoothWave(0.01, 0.03, 300, 1.5))
	cached := uint64(float64(total) * smoothWave(0.10, 0.20, 400, 2.0))
	swapUsed := uint64(float64(swapTotal) * smoothWave(0, 0.02, 900, 3.0))

	return Sample{Timestamp: time.Now(), Kind: "memory", Data: MemoryData{
		TotalBytes:     total,
		UsedBytes:      used,
		AvailableBytes: total - used,
		BuffersBytes:   buffers,
		CachedBytes:    cached,
		SwapTotalBytes: swapTotal,
		SwapUsedBytes:  swapUsed,
	}}, nil
}

// --- Disk ---

type MockDiskCollector struct{}

func NewMockDiskCollector() *MockDiskCollector { return &MockDiskCollector{} }
func (c *MockDiskCollector) Name() string      { return "disk" }

func (c *MockDiskCollector) Collect() (Sample, error) {
	rootTotal := uint64(500e9)
	rootUsed := uint64(float64(rootTotal) * smoothWave(0.58, 0.62, 1800, 0))
	homeTotal := uint64(2000e9)
	homeUsed := uint64(float64(homeTotal) * smoothWave(0.43, 0.47, 2400, 1.0))

	return Sample{Timestamp: time.Now(), Kind: "disk", Data: DiskData{
		Mounts: []DiskMountSample{
			{
				Mount: "/", Device: "/dev/nvme0n1p2",
				TotalBytes: rootTotal, UsedBytes: rootUsed, FreeBytes: rootTotal - rootUsed,
				ReadBytesSec: smoothWave(0, 50e6, 60, 0.5), WriteBytesSec: smoothWave(0, 30e6, 45, 1.0),
				ReadIOPS: smoothWave(0, 5000, 60, 0.5), WriteIOPS: smoothWave(0, 3000, 45, 1.0),
				SMART: &SMARTInfo{
					Available: true, Healthy: true,
					Temperature: 42, PowerOnHours: 12345, PowerCycles: 150,
					ReadSectors: 50000000, WrittenSectors: 30000000,
					AvailableSpare: 98, PercentUsed: 2,
				},
			},
			{
				Mount: "/home", Device: "/dev/sda1",
				TotalBytes: homeTotal, UsedBytes: homeUsed, FreeBytes: homeTotal - homeUsed,
				ReadBytesSec: smoothWave(0, 20e6, 90, 2.0), WriteBytesSec: smoothWave(0, 10e6, 70, 2.5),
				ReadIOPS: smoothWave(0, 2000, 90, 2.0), WriteIOPS: smoothWave(0, 1000, 70, 2.5),
				SMART: &SMARTInfo{
					Available: true, Healthy: true,
					Temperature: 35, PowerOnHours: 28000, PowerCycles: 450,
					ReadSectors: 120000000, WrittenSectors: 80000000,
					ReallocatedSectors: 3, ReadErrorRate: 200,
				},
			},
		},
	}}, nil
}

// --- Network ---

type MockNetworkCollector struct{}

func NewMockNetworkCollector() *MockNetworkCollector { return &MockNetworkCollector{} }
func (c *MockNetworkCollector) Name() string         { return "network" }

func (c *MockNetworkCollector) Collect() (Sample, error) {
	return Sample{Timestamp: time.Now(), Kind: "network", Data: NetworkData{
		Interfaces: []NetIfaceSample{
			{
				Interface: "eth0",
				RxBytesSec: smoothWave(1e6, 50e6, 45, 0), TxBytesSec: smoothWave(0.5e6, 20e6, 60, 0.5),
				RxPacketsSec: smoothWave(1000, 40000, 45, 0), TxPacketsSec: smoothWave(500, 15000, 60, 0.5),
			},
			{
				Interface: "wlan0",
				RxBytesSec: smoothWave(0.1e6, 5e6, 80, 2.0), TxBytesSec: smoothWave(0.05e6, 2e6, 100, 2.5),
				RxPacketsSec: smoothWave(100, 5000, 80, 2.0), TxPacketsSec: smoothWave(50, 2000, 100, 2.5),
			},
		},
	}}, nil
}

// --- ECC ---

type MockECCCollector struct{}

func NewMockECCCollector() *MockECCCollector { return &MockECCCollector{} }
func (c *MockECCCollector) Name() string     { return "ecc" }

func (c *MockECCCollector) Collect() (Sample, error) {
	return Sample{Timestamp: time.Now(), Kind: "ecc", Data: ECCData{}}, nil
}

// --- Temperature ---

type MockTemperatureCollector struct{}

func NewMockTemperatureCollector() *MockTemperatureCollector { return &MockTemperatureCollector{} }
func (c *MockTemperatureCollector) Name() string             { return "temperature" }

func (c *MockTemperatureCollector) Collect() (Sample, error) {
	return Sample{Timestamp: time.Now(), Kind: "temperature", Data: TemperatureData{
		Sensors: []TempSensorSample{
			{Sensor: "coretemp/Core 0", TempCelsius: smoothWave(38, 68, 90, 0)},
			{Sensor: "coretemp/Core 1", TempCelsius: smoothWave(36, 65, 100, 0.8)},
			{Sensor: "coretemp/Package id 0", TempCelsius: smoothWave(42, 75, 80, 0.3)},
			{Sensor: "acpitz/temp1", TempCelsius: smoothWave(30, 40, 300, 1.5)},
		},
	}}, nil
}

// --- Power ---

type MockPowerCollector struct{}

func NewMockPowerCollector() *MockPowerCollector { return &MockPowerCollector{} }
func (c *MockPowerCollector) Name() string       { return "power" }

func (c *MockPowerCollector) Collect() (Sample, error) {
	return Sample{Timestamp: time.Now(), Kind: "power", Data: PowerData{
		Zones: []PowerZoneSample{
			{Zone: "package-0", Watts: smoothWave(15, 65, 60, 0)},
			{Zone: "package-0/core", Watts: smoothWave(8, 45, 50, 0.5)},
			{Zone: "package-0/uncore", Watts: smoothWave(2, 10, 120, 1.0)},
		},
	}}, nil
}

// --- Process ---

type mockProcDef struct {
	pid     int32
	ppid    int32
	name    string
	cmdline string
	state   string
	uid     uint32
	baseCPU float64 // center of CPU oscillation
	cpuMax  float64 // max CPU
	baseRSS uint64  // center of RSS oscillation
	rssMax  uint64
	numFDs  int32
	threads int32
}

var mockProcs = []mockProcDef{
	// System daemons
	{1, 0, "systemd", "/usr/lib/systemd/systemd --system", "S", 0, 0.1, 0.5, 12 << 20, 16 << 20, 64, 1},
	{2, 0, "kthreadd", "", "S", 0, 0, 0.1, 0, 0, 0, 1},
	{150, 1, "systemd-journal", "/usr/lib/systemd/systemd-journald", "S", 0, 0.2, 1.0, 40 << 20, 60 << 20, 24, 1},
	{180, 1, "systemd-udevd", "/usr/lib/systemd/systemd-udevd", "S", 0, 0.1, 0.3, 8 << 20, 12 << 20, 16, 1},
	{220, 1, "dbus-daemon", "/usr/bin/dbus-daemon --system", "S", 81, 0.1, 0.5, 6 << 20, 10 << 20, 32, 1},
	{250, 1, "rsyslogd", "/usr/sbin/rsyslogd -n", "S", 0, 0.1, 0.4, 8 << 20, 12 << 20, 10, 4},
	{280, 1, "chronyd", "/usr/sbin/chronyd -F 2", "S", 0, 0, 0.1, 2 << 20, 4 << 20, 6, 1},
	{310, 1, "crond", "/usr/sbin/crond -n", "S", 0, 0, 0.1, 2 << 20, 4 << 20, 4, 1},
	{340, 1, "NetworkManager", "/usr/sbin/NetworkManager --no-daemon", "S", 0, 0.1, 0.5, 16 << 20, 24 << 20, 20, 3},
	{370, 1, "sshd", "sshd: /usr/sbin/sshd -D", "S", 0, 0.1, 0.3, 5 << 20, 8 << 20, 8, 1},
	{400, 370, "sshd", "sshd: user@pts/0", "S", 1000, 0.1, 0.2, 6 << 20, 8 << 20, 8, 1},
	{401, 400, "bash", "-bash", "S", 1000, 0.1, 0.3, 4 << 20, 6 << 20, 4, 1},

	// Workloads
	{500, 1, "postgres", "/usr/lib/postgresql/15/bin/postgres -D /var/lib/postgresql/15/main", "S", 26, 2.0, 8.0, 400 << 20, 600 << 20, 48, 8},
	{501, 500, "postgres", "postgres: checkpointer", "S", 26, 0.5, 2.0, 30 << 20, 50 << 20, 12, 1},
	{502, 500, "postgres", "postgres: background writer", "S", 26, 0.3, 1.5, 20 << 20, 40 << 20, 10, 1},
	{503, 500, "postgres", "postgres: walwriter", "S", 26, 0.4, 1.0, 20 << 20, 30 << 20, 8, 1},
	{504, 500, "postgres", "postgres: autovacuum launcher", "S", 26, 0.2, 3.0, 25 << 20, 50 << 20, 10, 1},
	{505, 500, "postgres", "postgres: logical replication launcher", "S", 26, 0.1, 0.5, 15 << 20, 25 << 20, 8, 1},
	{506, 500, "postgres", "postgres: stats collector", "S", 26, 0.3, 1.0, 20 << 20, 30 << 20, 8, 1},

	{600, 1, "nginx", "nginx: master process /usr/sbin/nginx", "S", 0, 0.1, 0.5, 8 << 20, 12 << 20, 16, 1},
	{601, 600, "nginx", "nginx: worker process", "S", 33, 1.0, 5.0, 40 << 20, 80 << 20, 64, 1},
	{602, 600, "nginx", "nginx: worker process", "S", 33, 0.8, 4.0, 35 << 20, 70 << 20, 64, 1},
	{603, 600, "nginx", "nginx: worker process", "S", 33, 0.6, 3.0, 30 << 20, 60 << 20, 64, 1},
	{604, 600, "nginx", "nginx: worker process", "S", 33, 0.5, 2.5, 28 << 20, 55 << 20, 64, 1},

	{700, 1, "node", "/usr/bin/node /opt/app/server.js", "S", 1000, 3.0, 15.0, 250 << 20, 400 << 20, 128, 12},
	{750, 1, "python3", "/usr/bin/python3 /opt/ml/train.py", "R", 1000, 5.0, 25.0, 800 << 20, 1200 << 20, 32, 4},

	{800, 1, "dockerd", "/usr/bin/dockerd -H fd://", "S", 0, 0.5, 2.0, 100 << 20, 150 << 20, 256, 16},
	{810, 800, "containerd", "/usr/bin/containerd", "S", 0, 0.3, 1.5, 60 << 20, 100 << 20, 128, 10},
	{820, 810, "containerd-shim", "containerd-shim -namespace moby", "S", 0, 0.1, 0.5, 12 << 20, 20 << 20, 16, 6},

	{900, 1, "redis-server", "/usr/bin/redis-server *:6379", "S", 999, 1.0, 4.0, 50 << 20, 80 << 20, 32, 4},

	// Kernel threads
	{3, 2, "rcu_gp", "", "I", 0, 0, 0, 0, 0, 0, 1},
	{4, 2, "rcu_par_gp", "", "I", 0, 0, 0, 0, 0, 0, 1},
	{10, 2, "migration/0", "", "S", 0, 0, 0.1, 0, 0, 0, 1},
	{11, 2, "migration/1", "", "S", 0, 0, 0.1, 0, 0, 0, 1},
	{14, 2, "cpuhp/0", "", "S", 0, 0, 0, 0, 0, 0, 1},
	{15, 2, "cpuhp/1", "", "S", 0, 0, 0, 0, 0, 0, 1},
	{20, 2, "kdevtmpfs", "", "S", 0, 0, 0, 0, 0, 0, 1},
	{21, 2, "netns", "", "I", 0, 0, 0, 0, 0, 0, 1},
	{25, 2, "kauditd", "", "S", 0, 0, 0, 0, 0, 0, 1},
	{30, 2, "ksoftirqd/0", "", "S", 0, 0.1, 0.5, 0, 0, 0, 1},
	{31, 2, "ksoftirqd/1", "", "S", 0, 0.1, 0.4, 0, 0, 0, 1},
	{40, 2, "kworker/0:0", "", "I", 0, 0, 0.2, 0, 0, 0, 1},
	{41, 2, "kworker/0:1", "", "I", 0, 0, 0.1, 0, 0, 0, 1},
	{42, 2, "kworker/1:0", "", "I", 0, 0, 0.2, 0, 0, 0, 1},
	{43, 2, "kworker/1:1", "", "I", 0, 0, 0.1, 0, 0, 0, 1},
	{44, 2, "kworker/2:0", "", "I", 0, 0, 0.2, 0, 0, 0, 1},
	{45, 2, "kworker/2:1", "", "I", 0, 0, 0.1, 0, 0, 0, 1},
	{46, 2, "kworker/3:0", "", "I", 0, 0, 0.2, 0, 0, 0, 1},
	{47, 2, "kworker/3:1", "", "I", 0, 0, 0.1, 0, 0, 0, 1},
	{50, 2, "kswapd0", "", "S", 0, 0, 0.3, 0, 0, 0, 1},
	{60, 2, "jbd2/nvme0n1p2", "", "S", 0, 0, 0.2, 0, 0, 0, 1},
	{70, 2, "kworker/u16:0", "", "I", 0, 0, 0.3, 0, 0, 0, 1},
	{71, 2, "kworker/u16:1", "", "I", 0, 0, 0.2, 0, 0, 0, 1},
	{72, 2, "kworker/u16:2", "", "I", 0, 0, 0.1, 0, 0, 0, 1},

	// More userspace
	{950, 1, "agetty", "/sbin/agetty -o -p -- \\u --noclear tty1 linux", "S", 0, 0, 0.1, 2 << 20, 3 << 20, 4, 1},
	{960, 1, "polkitd", "/usr/lib/polkit-1/polkitd --no-debug", "S", 0, 0, 0.1, 12 << 20, 16 << 20, 8, 3},
	{970, 1, "irqbalance", "/usr/sbin/irqbalance --foreground", "S", 0, 0, 0.1, 4 << 20, 6 << 20, 6, 1},
	{980, 1, "thermald", "/usr/sbin/thermald --systemd --dbus-enable", "S", 0, 0, 0.1, 4 << 20, 6 << 20, 8, 2},
	{990, 1, "udisksd", "/usr/lib/udisks2/udisksd", "S", 0, 0.1, 0.3, 8 << 20, 12 << 20, 16, 4},

	// A few more to round out
	{1010, 1, "snapd", "/usr/lib/snapd/snapd", "S", 0, 0.1, 0.5, 30 << 20, 50 << 20, 16, 8},
	{1020, 1, "accounts-daemon", "/usr/libexec/accounts-daemon", "S", 0, 0, 0.1, 4 << 20, 8 << 20, 8, 3},
	{1030, 1, "packagekitd", "/usr/libexec/packagekitd", "S", 0, 0, 0.1, 20 << 20, 30 << 20, 12, 3},
	{1100, 1, "prometheus-node", "/usr/bin/prometheus-node-exporter", "S", 65534, 0.3, 1.0, 20 << 20, 30 << 20, 16, 6},
	{1200, 1, "grafana-server", "/usr/sbin/grafana-server --config=/etc/grafana/grafana.ini", "S", 472, 1.0, 4.0, 100 << 20, 200 << 20, 64, 12},
	{1300, 1, "telegraf", "/usr/bin/telegraf --config /etc/telegraf/telegraf.conf", "S", 999, 0.5, 2.0, 40 << 20, 60 << 20, 32, 8},
	{1400, 1, "fail2ban-server", "/usr/bin/python3 /usr/bin/fail2ban-server -xf start", "S", 0, 0.1, 0.5, 20 << 20, 30 << 20, 12, 3},
}

// MockProcessCollector generates synthetic process data.
type MockProcessCollector struct {
	maxProcs      int
	configPins    []string
	runtimePinsFn func() []string
	startTime     int64 // fixed boot time for all mock processes

	mu       sync.RWMutex
	allProcs []ProcessBasicInfo
}

var _ ProcessCollectorI = (*MockProcessCollector)(nil)

func NewMockProcessCollector(maxProcs int, configPins []string) *MockProcessCollector {
	return &MockProcessCollector{
		maxProcs:   maxProcs,
		configPins: configPins,
		startTime:  time.Now().Add(-72 * time.Hour).UnixNano(), // "booted 3 days ago"
	}
}

func (c *MockProcessCollector) Name() string { return "process" }

func (c *MockProcessCollector) SetRuntimePinsFunc(fn func() []string) {
	c.runtimePinsFn = fn
}

func (c *MockProcessCollector) AllProcessSnapshot() []ProcessBasicInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]ProcessBasicInfo, len(c.allProcs))
	copy(out, c.allProcs)
	return out
}

func (c *MockProcessCollector) Collect() (Sample, error) {
	// Gather pin patterns
	pins := append([]string{}, c.configPins...)
	if c.runtimePinsFn != nil {
		pins = append(pins, c.runtimePinsFn()...)
	}

	type scored struct {
		idx    int
		cpuPct float64
		pinned bool
	}

	all := make([]ProcessBasicInfo, len(mockProcs))
	enriched := make([]scored, 0, len(mockProcs))

	var totalCPU float64
	var totalRSS uint64
	var running int32

	for i, p := range mockProcs {
		phase := float64(p.pid) * 0.13
		cpuPct := smoothWave(p.baseCPU, p.cpuMax, 30+float64(i)*7, phase)
		rss := uint64(smoothWave(float64(p.baseRSS), float64(p.rssMax), 120+float64(i)*11, phase+0.5))

		all[i] = ProcessBasicInfo{
			PID:        p.pid,
			Name:       p.name,
			State:      p.state,
			CPUPct:     cpuPct,
			RSSBytes:   rss,
			NumThreads: p.threads,
			StartTime:  c.startTime + int64(p.pid)*1e9, // stagger start times
		}

		totalCPU += cpuPct
		totalRSS += rss
		if p.state == "R" {
			running++
		}

		isPinned := false
		for _, pattern := range pins {
			if matched, _ := filepath.Match(pattern, p.name); matched {
				isPinned = true
				break
			}
		}

		enriched = append(enriched, scored{idx: i, cpuPct: cpuPct, pinned: isPinned})
	}

	// Store all-process snapshot
	c.mu.Lock()
	c.allProcs = all
	c.mu.Unlock()

	// Sort by CPU descending to pick top N
	sort.Slice(enriched, func(a, b int) bool {
		return enriched[a].cpuPct > enriched[b].cpuPct
	})

	// Select top N + pinned
	selected := make(map[int]bool)
	count := 0
	for _, e := range enriched {
		if e.pinned {
			selected[e.idx] = true
		} else if count < c.maxProcs {
			selected[e.idx] = true
			count++
		}
	}

	procs := make([]ProcessSample, 0, len(selected))
	for idx := range selected {
		p := mockProcs[idx]
		b := all[idx]
		userPct := b.CPUPct * 0.7
		sysPct := b.CPUPct * 0.3
		procs = append(procs, ProcessSample{
			PID:          p.pid,
			PPID:         p.ppid,
			Name:         p.name,
			Cmdline:      p.cmdline,
			State:        p.state,
			UID:          p.uid,
			CPUUserPct:   userPct,
			CPUSystemPct: sysPct,
			RSSBytes:     b.RSSBytes,
			VSSBytes:     b.RSSBytes * 3, // typical VSS/RSS ratio
			SharedBytes:  b.RSSBytes / 4,
			NumFDs:       p.numFDs,
			NumThreads:   p.threads,
			StartTime:    b.StartTime,
		})
	}

	// Count active (CPU > 0.5%)
	var active int32
	for _, b := range all {
		if b.CPUPct > 0.5 {
			active++
		}
	}

	return Sample{Timestamp: time.Now(), Kind: "process", Data: ProcessData{
		Processes:     procs,
		TotalProcs:    int32(len(mockProcs)),
		RunningProcs:  running,
		ActiveProcs:   active,
		TotalCPUPct:   totalCPU,
		TotalRSSBytes: totalRSS,
	}}, nil
}
