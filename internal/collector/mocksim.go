package collector

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// MockSim is the synthetic host behind mock mode. Every reading is a pure,
// deterministic function of time (plus the daemon's start time), so:
//
//   - the history seeder and the live mock collectors produce one continuous
//     series — there is no seam where seeded history meets live data;
//   - the readings agree with each other, because they are all derived from a
//     few shared activity drivers (web traffic, an ML training job, backups):
//     process CPU sums to the aggregate, per-core figures average to it, disk
//     I/O drives iowait, memory follows process RSS, and temperatures lag
//     package power;
//   - recordings are reproducible.
//
// Variation comes from smooth value noise, not sine waves, so charts look like a
// real host at every zoom level rather than a waveform (a 120s sine sampled
// into 1-minute buckets rendered as an alternating sawtooth).
//
// An optional scenario layers a scripted event over the baseline; see
// MockScenarioIncident.
type MockSim struct {
	anchor   float64 // daemon start, unix seconds
	boot     float64 // simulated boot time
	scenario string
	// incidentAt is when the incident begins (unix ns), 0 until scheduled. It
	// is scheduled once history seeding finishes, so it always plays live.
	incidentAt atomic.Int64

	// The mock collectors all sample the same tick concurrently; share one
	// snapshot per tick rather than evaluating the host once per collector.
	mu       sync.Mutex
	lastKey  int64
	lastSnap *MockSnapshot

	runqMu   sync.Mutex
	runqMemo map[int64]float64 // run queue length per loadTick grid point
}

// MockScenarioIncident starts an oversized backup shortly after history
// seeding finishes (see ScheduleScenario): restic saturates the CPU, reads /home flat out, uploads over eth0 and
// stages its pack files on the root filesystem (already ~87% full). The package temperature and the
// root disk both cross the demo alert thresholds, then everything recovers.
const MockScenarioIncident = "incident"

// IncidentLeadIn is how long after seeding finishes the incident begins: time
// to start the TUI (or a recording) and see the host at rest first.
const IncidentLeadIn = 20 * time.Second

// Incident timeline, in seconds after the incident begins.
const (
	incidentRampUp     = 8.0   // restic spins up
	incidentStageStart = 4.0   // pack staging on / begins
	incidentStageFull  = 40.0  // / peaks (~92%)
	incidentWindDown   = 50.0  // upload done, restic winding down
	incidentEnd        = 58.0  // restic exits
	incidentCleanEnd   = 62.0  // staged packs deleted, / back to normal
	incidentStageFrac  = 0.046 // fraction of / the staged packs occupy at peak (~23GB)
	incidentBackupLoad = 3.2   // backup driver while running (nightly runs peak at 1)
)

const (
	mockCores     = 8
	mockMemTotal  = 32 << 30
	mockSwapTotal = 8 << 30
	mockRootTotal = 500e9
	mockHomeTotal = 2000e9
	mockHistory   = 30 * 86400.0 // seeded history span, seconds
)

// NewMockSim returns a simulation anchored at the daemon's start time. scenario
// is "" (baseline only) or MockScenarioIncident.
func NewMockSim(anchor time.Time, scenario string) (*MockSim, error) {
	s := &MockSim{anchor: unixSec(anchor), runqMemo: make(map[int64]float64)}
	s.boot = s.anchor - 41*86400 - 3*3600 - 17*60 // up 41 days, predates the seeded history
	switch scenario {
	case "", MockScenarioIncident:
		s.scenario = scenario
	default:
		return nil, fmt.Errorf("unknown mock scenario %q (want %q or empty)", scenario, MockScenarioIncident)
	}
	return s, nil
}

// Anchor returns the time the simulation was started (the daemon's start).
func (s *MockSim) Anchor() time.Time { return fromUnixSec(s.anchor) }

// ScheduleScenario starts the configured scenario, if any, at the given time;
// it reports whether there was one to schedule. Only the first call counts.
func (s *MockSim) ScheduleScenario(at time.Time) bool {
	if s.scenario == "" {
		return false
	}
	s.incidentAt.CompareAndSwap(0, at.UnixNano())
	return true
}

// ScenarioStart returns when the scenario begins, once it has been scheduled.
func (s *MockSim) ScenarioStart() (time.Time, bool) {
	ns := s.incidentAt.Load()
	if ns == 0 {
		return time.Time{}, false
	}
	return time.Unix(0, ns), true
}

func unixSec(t time.Time) float64 { return float64(t.UnixNano()) / 1e9 }

func fromUnixSec(t float64) time.Time {
	sec, frac := math.Modf(t)
	return time.Unix(int64(sec), int64(frac*1e9))
}

// --- Noise ---

func mix64(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

// lattice returns a deterministic pseudo-random value in [-1, 1] for (seed, i).
func lattice(seed uint64, i int64) float64 {
	h := mix64(seed*0x9e3779b97f4a7c15 ^ uint64(i))
	return float64(h>>11)/float64(1<<53)*2 - 1
}

// vnoise is smooth 1-D value noise with features one unit apart.
func vnoise(seed uint64, x float64) float64 {
	fl := math.Floor(x)
	f := x - fl
	i := int64(fl)
	a, b := lattice(seed, i), lattice(seed, i+1)
	u := f * f * (3 - 2*f)
	return a + (b-a)*u
}

// fbm is fractal noise in roughly [-1, 1] whose slowest features are about
// period seconds wide, with finer detail layered on top.
func fbm(seed uint64, t, period float64) float64 {
	x := t / period
	return (vnoise(seed, x) + 0.5*vnoise(seed+101, x*2.03) + 0.25*vnoise(seed+202, x*4.11)) / 1.75
}

// ramp rises smoothly from 0 at a to 1 at b.
func ramp(t, a, b float64) float64 {
	x := (t - a) / (b - a)
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	return x * x * (3 - 2*x)
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// lagged returns f's exponentially weighted average over the trailing ~3τ — a
// first-order lag, as a thermal mass applies to heat input or the kernel's load
// average applies to the run queue. Pure: it re-evaluates f at earlier times.
func lagged(f func(float64) float64, t, tau float64) float64 {
	const n = 8
	step := 3 * tau / n
	var sum, wsum float64
	for j := 0; j < n; j++ {
		back := (float64(j) + 0.5) * step // midpoint of each slice of the past
		w := math.Exp(-back / tau)
		sum += w * f(t-back)
		wsum += w
	}
	return sum / wsum
}

// --- Activity drivers ---

// mockDrivers are the few underlying activities everything else derives from.
type mockDrivers struct {
	web   float64 // 0..1 web/database traffic, diurnal
	train float64 // 0..1 ML training intensity (GPU busy); dips during eval
	// trainOn is 0..1 while a training job is loaded (ramps at start only):
	// memory follows it, not the intensity, since eval doesn't free the model.
	trainOn float64
	ckpt    float64 // 0..1 checkpoint write burst at the end of each epoch
	backup  float64 // backup intensity: nightly runs peak at 1, the incident at incidentBackupLoad
	stage   float64 // 0..1 fraction of the incident's staged packs on /
	stageW  float64 // bytes/s being written to / by staging

	trainJob    int64 // identifies the training run (process identity), when train > 0
	trainStart  float64
	backupRun   int64 // identifies the backup run, when backup > 0
	backupStart float64
}

func (s *MockSim) drivers(t float64) mockDrivers {
	var d mockDrivers
	lt := fromUnixSec(t) // local time: the host is busy by day
	hour := float64(lt.Hour()) + float64(lt.Minute())/60 + float64(lt.Second())/3600
	dayF := 0.5 - 0.5*math.Cos(2*math.Pi*(hour-3)/24) // trough 03:00, peak 15:00
	weekF := 1.0
	if wd := lt.Weekday(); wd == time.Saturday || wd == time.Sunday {
		weekF = 0.55
	}
	d.web = clamp(0.07+0.66*dayF*weekF+0.10*fbm(11, t, 1800)+0.07*fbm(12, t, 150)+0.03*fbm(13, t, 15), 0.02, 1)

	s.training(t, &d)
	s.nightlyBackup(t, lt, &d)
	if ns := s.incidentAt.Load(); ns != 0 {
		s.incident(t, float64(ns)/1e9, &d)
	}
	return d
}

// training models ML jobs in 6-hour slots: most slots run a job; the job
// running when the daemon starts (and anything after) always does, so the GPU
// has something to show. Each epoch ends with an eval pass (GPU dips) and a
// checkpoint write.
func (s *MockSim) training(t float64, d *mockDrivers) {
	const slot = 6 * 3600.0
	job := int64(math.Floor(t / slot))
	forced := job >= int64(math.Floor((s.anchor-5*3600)/slot))
	if !forced && lattice(21, job) < -0.35 {
		return
	}
	// Runs start and finish at irregular times within their slot — except
	// around the daemon's start, where they run back to back so the GPU is
	// always busy in a demo.
	start := float64(job)*slot + 600 + 2400*(lattice(23, job)+1)/2
	end := float64(job+1)*slot - 300 - 4800*(lattice(24, job)+1)/2
	switch anchorSlot := int64(math.Floor(s.anchor / slot)); {
	case job >= anchorSlot:
		start, end = float64(job)*slot, float64(job+1)*slot
	case job == anchorSlot-1:
		end = float64(job+1) * slot
	}
	if t < start || t >= end {
		return
	}
	since := t - start
	const epoch = 540.0
	phase := math.Mod(since, epoch)
	dip := ramp(phase, 492, 500) * (1 - ramp(phase, 532, 540))
	d.trainOn = ramp(since, 0, 150)
	d.train = d.trainOn * (1 - 0.62*dip) * (0.97 + 0.03*fbm(22, t, 30))
	d.ckpt = ramp(phase, 520, 523) * (1 - ramp(phase, 531, 534))
	d.trainJob, d.trainStart = job, start
}

// nightlyBackup runs restic at 02:30 local for 35–50 minutes.
func (s *MockSim) nightlyBackup(t float64, lt time.Time, d *mockDrivers) {
	day := time.Date(lt.Year(), lt.Month(), lt.Day(), 2, 30, 0, 0, lt.Location())
	start := unixSec(day)
	dayIdx := int64(math.Floor(start / 86400))
	end := start + 2100 + 900*(lattice(31, dayIdx)+1)/2
	if t < start || t >= end {
		return
	}
	d.backup = ramp(t, start, start+60) * (1 - ramp(t, end-90, end)) * (0.85 + 0.15*fbm(32, t, 120))
	d.backupRun, d.backupStart = dayIdx, start
}

func (s *MockSim) incident(t, s0 float64, d *mockDrivers) {
	if t < s0 {
		return
	}
	since := t - s0
	if since < incidentEnd {
		d.backup = incidentBackupLoad * ramp(since, 0, incidentRampUp) * (1 - ramp(since, incidentWindDown, incidentEnd)) *
			(0.95 + 0.05*fbm(33, t, 4))
		d.backupRun, d.backupStart = -1, s0
	}
	stage := func(x float64) float64 {
		return ramp(x, incidentStageStart, incidentStageFull) * (1 - ramp(x, incidentEnd, incidentCleanEnd))
	}
	d.stage = stage(since)
	// Staging writes at the rate the packs grow (deletion afterwards is free).
	if dv := (stage(since+0.5) - stage(since-0.5)) * incidentStageFrac * mockRootTotal; dv > 0 {
		d.stageW = dv
	}
}

// --- Processes ---

// load is a quantity driven linearly by the activity drivers.
type load struct{ base, web, train, ckpt, backup float64 }

func (l load) at(d *mockDrivers) float64 {
	return l.base + l.web*d.web + l.train*d.train + l.ckpt*d.ckpt + l.backup*d.backup
}

type procWhen int

const (
	procAlways procWhen = iota
	procTraining
	procBackup
)

type simProc struct {
	pid, ppid int32
	name      string
	cmdline   string
	uid       uint32
	threads   int32
	fds       int32
	kernel    bool // kernel thread: idles in state I
	when      procWhen
	worker    int32 // training data-loader index (pid offset from the main process)

	cpu     load    // per-core %, i.e. 100 = one full core
	sysFrac float64 // share of CPU spent in the kernel
	rss     load    // bytes
	rootR   load    // bytes/s read from /
	rootW   load    // bytes/s written to /
	homeR   load    // bytes/s read from /home
	homeW   load    // bytes/s written to /home
	rx, tx  load    // bytes/s over eth0
	jitter  float64 // relative noise on CPU and I/O
}

const mib = 1 << 20

var simProcs = func() []simProc {
	p := []simProc{
		{pid: 1, name: "systemd", cmdline: "/sbin/init", threads: 1, fds: 118,
			cpu: load{base: 0.05}, sysFrac: 0.6, rss: load{base: 13 * mib}, jitter: 0.5},
		{pid: 2, name: "kthreadd", kernel: true, threads: 1},
		{pid: 15, name: "rcu_preempt", kernel: true, threads: 1, cpu: load{base: 0.1, web: 0.3}, sysFrac: 1, jitter: 0.6},
		{pid: 124, name: "kswapd0", kernel: true, threads: 1, cpu: load{base: 0.01}, sysFrac: 1},
		{pid: 389, name: "jbd2/nvme0n1p2-8", kernel: true, threads: 1, cpu: load{base: 0.02, web: 0.2, backup: 0.6}, sysFrac: 1, jitter: 0.6},
		{pid: 402, name: "systemd-journal", cmdline: "/lib/systemd/systemd-journald", threads: 1, fds: 41,
			cpu: load{base: 0.1, web: 0.6}, sysFrac: 0.5, rss: load{base: 46 * mib, web: 10 * mib},
			rootW: load{base: 40e3, web: 400e3}, jitter: 0.4},
		{pid: 451, name: "systemd-udevd", cmdline: "/lib/systemd/systemd-udevd", threads: 1, fds: 22, rss: load{base: 9 * mib}},
		{pid: 902, name: "dbus-daemon", cmdline: "/usr/bin/dbus-daemon --system --address=systemd: --nofork --nopidfile --systemd-activation --syslog-only",
			uid: 101, threads: 1, fds: 31, cpu: load{base: 0.02}, rss: load{base: 6 * mib}},
		{pid: 917, name: "chronyd", cmdline: "/usr/sbin/chronyd -F 1", uid: 104, threads: 1, fds: 7, rss: load{base: 3 * mib}},
		{pid: 921, name: "cron", cmdline: "/usr/sbin/cron -f", threads: 1, fds: 5, rss: load{base: 3 * mib}},
		{pid: 944, name: "sshd", cmdline: "sshd: /usr/sbin/sshd -D [listener] 0 of 10-100 startups", threads: 1, fds: 6, rss: load{base: 8 * mib}},
		{pid: 1180, name: "containerd", cmdline: "/usr/bin/containerd", threads: 14, fds: 88,
			cpu: load{base: 0.2, web: 0.5}, sysFrac: 0.4, rss: load{base: 52 * mib}, jitter: 0.4},
		{pid: 1203, name: "dockerd", cmdline: "/usr/bin/dockerd -H fd:// --containerd=/run/containerd/containerd.sock", threads: 22, fds: 141,
			cpu: load{base: 0.2, web: 0.6}, sysFrac: 0.4, rss: load{base: 104 * mib}, jitter: 0.4},
		{pid: 1290, name: "postgres", cmdline: "/usr/lib/postgresql/16/bin/postgres -D /var/lib/postgresql/16/main -c config_file=/etc/postgresql/16/main/postgresql.conf",
			uid: 113, threads: 1, fds: 14, cpu: load{base: 0.1}, sysFrac: 0.3, rss: load{base: 1150 * mib}},
		{pid: 1302, ppid: 1290, name: "postgres", cmdline: "postgres: 16/main: checkpointer", uid: 113, threads: 1, fds: 12,
			cpu: load{base: 0.1, web: 0.8}, sysFrac: 0.5, rss: load{base: 210 * mib, web: 90 * mib}, rootW: load{base: 0.3e6, web: 4e6}, jitter: 0.8},
		{pid: 1303, ppid: 1290, name: "postgres", cmdline: "postgres: 16/main: background writer", uid: 113, threads: 1, fds: 10,
			cpu: load{base: 0.1, web: 0.6}, sysFrac: 0.5, rss: load{base: 140 * mib}, rootW: load{web: 2.2e6}, jitter: 0.6},
		{pid: 1305, ppid: 1290, name: "postgres", cmdline: "postgres: 16/main: walwriter", uid: 113, threads: 1, fds: 9,
			cpu: load{base: 0.1, web: 1.4}, sysFrac: 0.4, rss: load{base: 24 * mib}, rootW: load{base: 0.1e6, web: 6.5e6}, jitter: 0.4},
		{pid: 1306, ppid: 1290, name: "postgres", cmdline: "postgres: 16/main: autovacuum launcher", uid: 113, threads: 1, fds: 9,
			cpu: load{base: 0.05, web: 0.4}, rss: load{base: 12 * mib}, jitter: 1.2},
		{pid: 1307, ppid: 1290, name: "postgres", cmdline: "postgres: 16/main: logical replication launcher", uid: 113, threads: 1, fds: 9,
			rss: load{base: 9 * mib}},
		{pid: 1366, name: "redis-server", cmdline: "/usr/bin/redis-server 127.0.0.1:6379", uid: 112, threads: 5, fds: 64,
			cpu: load{base: 0.3, web: 4.5}, sysFrac: 0.45, rss: load{base: 1380 * mib, web: 160 * mib}, rootW: load{web: 0.6e6}, jitter: 0.4},
		{pid: 1420, name: "nginx", cmdline: "nginx: master process /usr/sbin/nginx -g daemon on; master_process on;", threads: 1, fds: 12,
			rss: load{base: 11 * mib}},
		{pid: 1520, name: "node", cmdline: "node /srv/app/dist/server.js", uid: 1001, threads: 11, fds: 96,
			cpu: load{base: 0.6, web: 58}, sysFrac: 0.2, rss: load{base: 310 * mib, web: 190 * mib},
			rx: load{web: 2.2e6}, tx: load{web: 1.1e6}, jitter: 0.3},
		{pid: 1588, name: "prometheus", cmdline: "/usr/bin/prometheus --config.file=/etc/prometheus/prometheus.yml --storage.tsdb.path=/var/lib/prometheus/metrics2/",
			uid: 110, threads: 16, fds: 211, cpu: load{base: 1.4, web: 0.8}, sysFrac: 0.2, rss: load{base: 690 * mib},
			rootW: load{base: 0.9e6}, rx: load{base: 0.2e6}, jitter: 0.9},
		{pid: 1590, name: "prometheus-node", cmdline: "/usr/bin/prometheus-node-exporter", uid: 110, threads: 7, fds: 12,
			cpu: load{base: 0.3}, sysFrac: 0.5, rss: load{base: 21 * mib}, jitter: 0.8},
		{pid: 1612, name: "grafana", cmdline: "/usr/share/grafana/bin/grafana server --config=/etc/grafana/grafana.ini --homepath /usr/share/grafana",
			uid: 472, threads: 19, fds: 47, cpu: load{base: 0.4, web: 1.2}, sysFrac: 0.2, rss: load{base: 186 * mib}, jitter: 0.8},
		{pid: 1730, name: "bewitchd", cmdline: "/usr/bin/bewitchd -config /etc/bewitch.toml", uid: 998, threads: 12, fds: 29,
			cpu: load{base: 0.5, web: 0.2}, sysFrac: 0.4, rss: load{base: 64 * mib}, rootW: load{base: 60e3}, jitter: 0.3},
		{pid: 2214, name: "containerd-shim", cmdline: "/usr/bin/containerd-shim-runc-v2 -namespace moby -id 6f1e2b9c4a07 -address /run/containerd/containerd.sock",
			threads: 11, fds: 19, cpu: load{base: 0.05}, rss: load{base: 14 * mib}},
		{pid: 2260, name: "fail2ban-server", cmdline: "/usr/bin/python3 /usr/bin/fail2ban-server -xf start", threads: 5, fds: 14,
			cpu: load{base: 0.1, web: 0.3}, rss: load{base: 31 * mib}, jitter: 0.6},
		{pid: 3301, ppid: 944, name: "sshd", cmdline: "sshd: ross [priv]", threads: 1, fds: 6, rss: load{base: 10 * mib}},
		{pid: 3318, ppid: 3301, name: "tmux: server", cmdline: "tmux new -s train", uid: 1000, threads: 1, fds: 12,
			cpu: load{base: 0.05}, rss: load{base: 6 * mib}},
		{pid: 3319, ppid: 3318, name: "bash", cmdline: "-bash", uid: 1000, threads: 1, fds: 4, rss: load{base: 5 * mib}},
	}
	// Database backends: one per pooled connection from the app.
	for i := int32(0); i < 4; i++ {
		p = append(p, simProc{pid: 1640 + i*3, ppid: 1290, name: "postgres",
			cmdline: fmt.Sprintf("postgres: 16/main: app appdb 127.0.0.1(%d) idle", 41522+i*17),
			uid:     113, threads: 1, fds: 21,
			cpu: load{base: 0.05, web: 34 - float64(i)*5.5}, sysFrac: 0.25,
			rss:    load{base: 38 * mib, web: 70 * mib},
			rootR:  load{base: 0.2e6, web: 9e6 - float64(i)*1.2e6},
			jitter: 0.5})
	}
	for i := int32(0); i < 4; i++ {
		p = append(p, simProc{pid: 1421 + i, ppid: 1420, name: "nginx", cmdline: "nginx: worker process",
			uid: 33, threads: 1, fds: 58, cpu: load{base: 0.05, web: 5.2 - float64(i)*0.6}, sysFrac: 0.55,
			rss: load{base: 14 * mib, web: 8 * mib},
			rx:  load{base: 0.05e6, web: 7.5e6 - float64(i)*0.9e6}, tx: load{base: 0.08e6, web: 2.9e6 - float64(i)*0.3e6},
			jitter: 0.35})
	}
	// Kernel worker threads; softirq load follows the network.
	for c := int32(0); c < mockCores; c++ {
		p = append(p, simProc{pid: 14 + c*6, name: fmt.Sprintf("ksoftirqd/%d", c), kernel: true, threads: 1,
			cpu: load{web: 0.25, backup: 0.35}, sysFrac: 1, jitter: 0.9})
		p = append(p, simProc{pid: 300 + c*2, name: fmt.Sprintf("kworker/%d:1-events", c), kernel: true, threads: 1,
			cpu: load{base: 0.02, web: 0.1}, sysFrac: 1, jitter: 1})
	}
	p = append(p,
		simProc{pid: 288, name: "kworker/u16:2-flush-259:0", kernel: true, threads: 1, cpu: load{base: 0.02, backup: 0.9}, sysFrac: 1, jitter: 0.8},
		simProc{pid: 291, name: "kworker/u16:4-events_unbound", kernel: true, threads: 1, cpu: load{base: 0.02, web: 0.1}, sysFrac: 1, jitter: 0.8},
	)

	// ML training: the main process feeds the GPU and writes checkpoints; four
	// data-loader workers decode the dataset from /home.
	p = append(p, simProc{name: "python3", when: procTraining, uid: 1000, threads: 46, fds: 187,
		cmdline: "python3 train.py --config configs/vit_b16.yaml --data /home/ross/datasets/imagenet",
		cpu:     load{base: 2, train: 102}, sysFrac: 0.18, rss: load{base: 2800 * mib, train: 6900 * mib},
		homeW: load{ckpt: 165e6}, jitter: 0.08})
	for i := int32(1); i <= 4; i++ {
		p = append(p, simProc{name: "python3", when: procTraining, worker: i, uid: 1000, threads: 4, fds: 23,
			cmdline: "python3 train.py --config configs/vit_b16.yaml --data /home/ross/datasets/imagenet",
			cpu:     load{train: 26 - float64(i)}, sysFrac: 0.12, rss: load{base: 480 * mib, train: 560 * mib},
			homeR: load{train: 9.5e6}, jitter: 0.15})
	}

	// Backups: restic reads /home, uploads offsite, and caches packs on /.
	p = append(p, simProc{name: "restic", when: procBackup,
		cmdline: "restic backup /home --exclude-caches --compression max --one-file-system",
		threads: 22, fds: 61,
		cpu: load{backup: 162}, sysFrac: 0.14, rss: load{base: 210 * mib, backup: 520 * mib},
		homeR: load{backup: 150e6}, rootW: load{backup: 6e6}, tx: load{backup: 35e6}, jitter: 0.12})
	return p
}()

// simProcState is one process's reading at an instant.
type simProcState struct {
	def          *simProc
	pid, ppid    int32
	start        float64
	state        string
	user, sys    float64 // per-core %
	rss          uint64
	rootR, rootW float64
	homeR, homeW float64
	rx, tx       float64
	fds, threads int32
}

// procAt evaluates one process; ok is false when it isn't running at t.
func (s *MockSim) procAt(p *simProc, i int, t float64, d *mockDrivers) (ps simProcState, ok bool) {
	ps = simProcState{def: p, pid: p.pid, ppid: p.ppid, threads: p.threads, fds: p.fds}
	switch p.when {
	case procTraining:
		if d.train <= 0 {
			return ps, false
		}
		main := 20000 + int32(mix64(uint64(d.trainJob))%9000)*3
		ps.pid, ps.start = main+p.worker, d.trainStart+float64(p.worker)*4.2
		ps.ppid = 3319
		if p.worker > 0 {
			ps.ppid = main
		}
	case procBackup:
		if d.backup <= 0 {
			return ps, false
		}
		ps.pid = 50000 + int32(mix64(uint64(d.backupRun+7))%9000)
		ps.start, ps.ppid = d.backupStart, 921 // launched from cron
	default:
		ps.start = s.boot + 2 + float64(p.pid)*0.011
		if ps.ppid == 0 && !p.kernel && p.pid != 1 {
			ps.ppid = 1
		}
		if p.kernel && p.pid != 2 {
			ps.ppid = 2
		}
	}
	seed := uint64(1000 + i)
	jit := func(sub uint64, period float64) float64 {
		return math.Max(0, 1+p.jitter*fbm(seed*16+sub, t, period))
	}
	cpu := p.cpu.at(d) * jit(1, 6+float64(i%7)*3)
	ps.user, ps.sys = cpu*(1-p.sysFrac), cpu*p.sysFrac
	resident := *d
	resident.train = d.trainOn
	ps.rss = uint64(math.Max(0, p.rss.at(&resident)*(1+0.04*fbm(seed*16+2, t, 900))))
	io := jit(3, 9)
	ps.rootR, ps.rootW = p.rootR.at(d)*io, p.rootW.at(d)*io
	ps.homeR, ps.homeW = p.homeR.at(d)*io, p.homeW.at(d)
	ps.rx, ps.tx = p.rx.at(d)*io, p.tx.at(d)*jit(4, 5)
	if p.when == procBackup {
		ps.rootW += d.stageW
	}
	if p.webDriven() {
		ps.fds += int32(float64(p.fds) / 2 * d.web)
	}

	switch {
	case p.kernel && cpu < 0.5:
		ps.state = "I"
	case p.when == procBackup && d.backup > 0.5 && fbm(seed*16+5, t, 3) > 0.25:
		ps.state = "D" // blocked on /home reads
	case cpu > 45 || (cpu > 4 && fbm(seed*16+6, t, 2) > 0.2):
		ps.state = "R"
	default:
		ps.state = "S"
	}
	return ps, true
}

// web reports whether any of the process's load is web-driven (so its open
// file count scales with connections).
func (p *simProc) webDriven() bool { return p.cpu.web > 0 || p.rx.web > 0 }

// --- Snapshot ---

// MockSnapshot is the whole simulated host at one instant.
type MockSnapshot struct {
	Time        time.Time
	CPU         CPUData
	Memory      MemoryData
	Load        LoadData
	Disk        DiskData
	Network     NetworkData
	Temperature TemperatureData
	Power       PowerData
	GPU         GPUData
	ECC         ECCData
	procs       []simProcState
}

type mockAgg struct {
	d                          mockDrivers
	procs                      []simProcState
	user, sys, iow             float64 // aggregate %, of the whole machine
	rootR, rootW, homeR, homeW float64
	rx, tx                     float64
}

func (s *MockSim) aggregate(t float64) mockAgg {
	a := mockAgg{d: s.drivers(t)}
	var procUser, procSys float64
	for i := range simProcs {
		ps, ok := s.procAt(&simProcs[i], i, t, &a.d)
		if !ok {
			continue
		}
		a.procs = append(a.procs, ps)
		procUser += ps.user
		procSys += ps.sys
		a.rootR += ps.rootR
		a.rootW += ps.rootW
		a.homeR += ps.homeR
		a.homeW += ps.homeW
		a.rx += ps.rx
		a.tx += ps.tx
	}
	// Traffic the process table doesn't attribute (other clients, ARP, etc.),
	// plus filesystem metadata and log writes.
	a.rx += 0.15e6 + 0.4e6*a.d.web*(1+0.3*fbm(41, t, 20))
	a.tx += 0.08e6 + 0.2e6*a.d.web*(1+0.3*fbm(42, t, 20))
	a.rootR += 0.3e6 * (1 + 0.5*fbm(43, t, 30))
	a.rootW += 0.5e6 * (1 + 0.5*fbm(44, t, 30))
	a.homeW += 0.05e6

	irq := (a.rx + a.tx) / 1e8 * 0.35 // interrupt handling
	// A saturated CPU can't give every process what it asks for: the scheduler
	// shares out what there is, so the process figures shrink with it and still
	// add up to the aggregate.
	const capacity = 97.0
	if demand := (procUser+procSys)/mockCores + irq; demand > capacity {
		f := (capacity - irq) / (demand - irq)
		procUser, procSys = procUser*f, procSys*f
		for i := range a.procs {
			a.procs[i].user *= f
			a.procs[i].sys *= f
		}
	}
	a.user = procUser / mockCores
	a.sys = procSys/mockCores + irq
	// iowait is idle time with I/O outstanding, so it shrinks as the CPU fills.
	a.iow = (0.25 + (a.homeR+a.homeW)/1e6*0.021 + (a.rootR+a.rootW)/1e6*0.0016) * (1 + 0.2*fbm(45, t, 10))
	a.iow = math.Min(a.iow, math.Max(0, 99.5-a.user-a.sys))
	return a
}

// busy returns aggregate non-idle % — the quantity power and heat follow.
func (s *MockSim) busy(t float64) float64 {
	a := s.aggregate(t)
	return a.user + a.sys + a.iow
}

func (s *MockSim) igpuUtil(t float64) float64 {
	return 3 + 4*(0.5+0.5*fbm(51, t, 40))
}

func (s *MockSim) packageWatts(t float64) float64 {
	a := s.aggregate(t)
	return 8.5 + 0.97*(a.user+a.sys) + 0.15*a.iow + (0.4 + 0.11*s.igpuUtil(t))
}

func (s *MockSim) nvidiaUtil(t float64) float64 {
	d := s.drivers(t)
	if d.train <= 0 {
		return clamp(1+fbm(52, t, 60), 0, 3)
	}
	return clamp(97*d.train+1.5*fbm(53, t, 8), 0, 100)
}

func (s *MockSim) nvidiaWatts(t float64) float64 { return 21 + 3.95*s.nvidiaUtil(t) }

// Now returns the snapshot for the current instant, shared by every caller
// within the same 250ms window.
func (s *MockSim) Now() *MockSnapshot {
	now := time.Now()
	key := now.UnixNano() / int64(250*time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastSnap == nil || s.lastKey != key {
		snap := s.Snapshot(now)
		s.lastKey, s.lastSnap = key, &snap
	}
	return s.lastSnap
}

// Snapshot evaluates the whole host at t.
func (s *MockSim) Snapshot(at time.Time) MockSnapshot {
	t := unixSec(at)
	a := s.aggregate(t)
	snap := MockSnapshot{Time: at, procs: a.procs}

	// CPU: per-core figures spread the aggregate unevenly but average to it.
	busy := a.user + a.sys
	userShare := 0.0
	if busy > 0 {
		userShare = a.user / busy
	}
	var w [mockCores]float64
	var wsum float64
	for c := range w {
		w[c] = math.Max(0.15, 1+0.45*fbm(uint64(500+c), t, 9+float64(c)*2.5))
		if c == 0 {
			w[c] += 0.12 // IRQs land on core 0
		}
		wsum += w[c]
	}
	coreBusy := make([]float64, mockCores)
	for c := range coreBusy {
		coreBusy[c] = busy * w[c] * mockCores / wsum
	}
	for pass := 0; pass < 3; pass++ { // a saturated core's excess spills onto the others
		var excess, room float64
		for _, b := range coreBusy {
			if b > 97 {
				excess += b - 97
			} else {
				room += 97 - b
			}
		}
		if excess == 0 || room == 0 {
			break
		}
		for c, b := range coreBusy {
			if b > 97 {
				coreBusy[c] = 97
			} else {
				coreBusy[c] = b + excess*(97-b)/room
			}
		}
	}
	for c, b := range coreBusy { // pinned cores still wobble a little, independently
		if b >= 96.9 {
			coreBusy[c] = 99.4 - 2.4*(0.5+0.5*fbm(uint64(700+c), t, 2.5))
		}
	}
	cores := []CPUCoreSample{{Core: -1, UserPct: a.user, SystemPct: a.sys, IOWaitPct: a.iow, IdlePct: 100 - a.user - a.sys - a.iow}}
	for c, b := range coreBusy {
		iow := math.Min(a.iow*(1+0.5*fbm(uint64(600+c), t, 7)), 99-b)
		cores = append(cores, CPUCoreSample{Core: c, UserPct: b * userShare, SystemPct: b * (1 - userShare),
			IOWaitPct: iow, IdlePct: math.Max(0, 100-b-iow)})
	}
	snap.CPU = CPUData{Cores: cores}

	// Memory follows the processes' RSS (shared pages counted once) plus kernel
	// overhead; the page cache fills with whatever is being read.
	var rss float64
	for _, p := range a.procs {
		rss += float64(p.rss)
	}
	used := 1.3*(1<<30) + rss*0.9
	buffers := 390*mib + 110*mib*fbm(61, t, 3600)
	cached := 6.4*(1<<30) + 1.6*(1<<30)*fbm(62, t, 6*3600) + 2.5*(1<<30)*a.d.trainOn + 1.1*(1<<30)*math.Min(a.d.backup, 1)
	cached = math.Min(cached, mockMemTotal-used-buffers-450*mib)
	snap.Memory = MemoryData{
		TotalBytes: mockMemTotal, UsedBytes: uint64(used), AvailableBytes: uint64(mockMemTotal - used),
		BuffersBytes: uint64(buffers), CachedBytes: uint64(cached),
		SwapTotalBytes: mockSwapTotal, SwapUsedBytes: uint64(186*mib + 24*mib*fbm(63, t, 4*3600)),
	}

	snap.Load = s.loadAvg(t)

	snap.Disk = s.disk(t, &a)
	snap.Network = s.network(t, &a)

	// Heat lags power. Package power tracks CPU busy; the iGPU sits in uncore.
	pkgLag := lagged(s.packageWatts, t, 12)
	pkgTemp := 30 + 0.63*pkgLag + 0.6*fbm(71, t, 20)
	snap.Temperature = TemperatureData{Sensors: []TempSensorSample{
		{Sensor: "coretemp/Core 0", TempCelsius: pkgTemp - 2.5 + (coreBusy[0]-busy)*0.04 + 0.8*fbm(72, t, 6)},
		{Sensor: "coretemp/Core 1", TempCelsius: pkgTemp - 3.5 + (coreBusy[1]-busy)*0.04 + 0.8*fbm(73, t, 6)},
		{Sensor: "coretemp/Package id 0", TempCelsius: pkgTemp},
		{Sensor: "acpitz/temp1", TempCelsius: 27.8 + 0.05*lagged(s.packageWatts, t, 300) + 0.4*fbm(74, t, 3600)},
	}}
	igpuW := 0.4 + 0.11*s.igpuUtil(t)
	pkgW := s.packageWatts(t) + 0.8*fbm(75, t, 3)
	snap.Power = PowerData{Zones: []PowerZoneSample{
		{Zone: "package-0", Watts: pkgW},
		{Zone: "package-0/core", Watts: math.Max(0.5, 0.74*(pkgW-8.5-igpuW)+1.9)},
		{Zone: "package-0/uncore", Watts: igpuW},
	}}

	gu := s.igpuUtil(t)
	nu := s.nvidiaUtil(t)
	nvW := s.nvidiaWatts(t) + 4*fbm(76, t, 4)
	nvTemp := 31 + 0.115*lagged(s.nvidiaWatts, t, 35)
	nvFreq, nvMem := 210.0, 0.48e9
	if a.d.train > 0 {
		nvFreq = clamp(2520-95*(nvTemp-62)/15, 2235, 2520)
		nvMem = 21.4e9 + 0.3e9*fbm(77, t, 600)
	}
	snap.GPU = GPUData{GPUs: []GPUDeviceSample{
		{Name: "Intel UHD Graphics 770", Index: 0, Vendor: "intel", UtilizationPct: gu, PowerWatts: igpuW,
			FrequencyMHz: uint32(300 + gu*28), FrequencyMaxMHz: 1450, ThrottlePct: clamp(100-gu*1.6, 0, 100)},
		{Name: "NVIDIA GeForce RTX 4090", Index: 1, Vendor: "nvidia", UtilizationPct: nu,
			MemoryUsedBytes: uint64(nvMem), MemoryTotalBytes: 24 << 30, TempCelsius: nvTemp, PowerWatts: nvW,
			FrequencyMHz: uint32(nvFreq), FrequencyMaxMHz: 2520},
	}}
	snap.ECC = ECCData{Present: true}
	return snap
}

func (s *MockSim) disk(t float64, a *mockAgg) DiskData {
	// / fills slowly over the month (76% → 87% at daemon start), with a daily
	// log-rotation sawtooth, plus any staged backup packs.
	tt := math.Min(t, s.anchor)
	trend := 0.762 + 0.112*clamp((tt-(s.anchor-mockHistory))/mockHistory, 0, 1)
	lt := fromUnixSec(t)
	dayFrac := (float64(lt.Hour())*3600 + float64(lt.Minute())*60 + float64(lt.Second())) / 86400
	rootPct := trend + 0.003*dayFrac + 0.0004*fbm(81, t, 7200) + incidentStageFrac*a.d.stage
	homePct := 0.447 + 0.021*clamp((tt-(s.anchor-mockHistory))/mockHistory, 0, 1) + 0.0008*fbm(82, t, 86400)
	rootUsed := uint64(mockRootTotal * rootPct)
	homeUsed := uint64(mockHomeTotal * homePct)

	nvmeTemp := uint64(math.Round(37 + (a.rootR+a.rootW)/1e8*1.6 + 0.04*lagged(s.packageWatts, t, 120)))
	hours := uint64((t - s.anchor + 11843*3600) / 3600)
	nvme := SMARTInfo{Available: true, Healthy: true, Temperature: nvmeTemp, PowerOnHours: hours, PowerCycles: 214,
		ReadSectors: 81_200_000_000/512 + hours*41_000, WrittenSectors: 143_500_000_000/512 + hours*96_000,
		AvailableSpare: 100, PercentUsed: 3}
	hdd := SMARTInfo{Available: true, Healthy: true, Temperature: uint64(math.Round(33 + a.homeR/1e8*1.8)),
		PowerOnHours: hours + 14120, PowerCycles: 388, ReallocatedSectors: 0, ReadErrorRate: 0}

	data := DiskData{Mounts: []DiskMountSample{
		{Mount: "/", Device: "/dev/nvme0n1p2", Transport: "nvme",
			TotalBytes: uint64(mockRootTotal), UsedBytes: rootUsed, FreeBytes: uint64(mockRootTotal) - rootUsed,
			InodesTotal: 30_521_344, InodesFree: 30_521_344 - uint64(float64(rootUsed)/165e3),
			ReadBytesSec: a.rootR, WriteBytesSec: a.rootW, ReadIOPS: a.rootR / 24e3, WriteIOPS: a.rootW / 96e3,
			SMART: &nvme},
		{Mount: "/home", Device: "/dev/sda1", Transport: "sata",
			TotalBytes: uint64(mockHomeTotal), UsedBytes: homeUsed, FreeBytes: uint64(mockHomeTotal) - homeUsed,
			InodesTotal: 122_093_568, InodesFree: 122_093_568 - uint64(float64(homeUsed)/2.4e6),
			ReadBytesSec: a.homeR, WriteBytesSec: a.homeW, ReadIOPS: a.homeR / 512e3, WriteIOPS: a.homeW / 256e3,
			SMART: &hdd},
	}}
	data.SMART = []SMARTDevice{{Device: "/dev/nvme0n1", Info: nvme}, {Device: "/dev/sda", Info: hdd}}
	return data
}

func (s *MockSim) network(t float64, a *mockAgg) NetworkData {
	up := t - s.boot
	wgRx := 0.04e6 + 1.3e6*a.d.web*(1+0.6*fbm(91, t, 40))
	wgTx := 0.03e6 + 0.5e6*a.d.web*(1+0.6*fbm(92, t, 40))
	return NetworkData{Interfaces: []NetIfaceSample{
		{Interface: "eth0", RxBytesSec: a.rx, TxBytesSec: a.tx,
			RxPacketsSec: a.rx / 1180, TxPacketsSec: a.tx / 1320,
			RxDropped: uint64(up / 5400), TxDropped: 0},
		{Interface: "wg0", RxBytesSec: wgRx, TxBytesSec: wgTx,
			RxPacketsSec: wgRx / 610, TxPacketsSec: wgTx / 480,
			RxDropped: uint64(up / 86400 / 3)},
	}}
}

// Processes returns the snapshot's processes, busiest first, as the lightweight
// all-process view and as fully enriched samples.
func (snap MockSnapshot) Processes() ([]ProcessBasicInfo, []ProcessSample) {
	procs := append([]simProcState(nil), snap.procs...)
	sort.SliceStable(procs, func(i, j int) bool {
		return procs[i].user+procs[i].sys > procs[j].user+procs[j].sys
	})
	basic := make([]ProcessBasicInfo, len(procs))
	full := make([]ProcessSample, len(procs))
	for i, p := range procs {
		start := int64(p.start * 1e9)
		basic[i] = ProcessBasicInfo{PID: p.pid, Name: p.def.name, State: p.state, CPUPct: p.user + p.sys,
			RSSBytes: p.rss, NumThreads: p.threads, StartTime: start}
		vss := p.rss * 3
		if p.def.kernel {
			vss = 0
		}
		full[i] = ProcessSample{PID: p.pid, PPID: p.ppid, Name: p.def.name, Cmdline: p.def.cmdline,
			State: p.state, UID: p.def.uid, CPUUserPct: p.user, CPUSystemPct: p.sys,
			RSSBytes: p.rss, VSSBytes: vss, SharedBytes: p.rss / 5, NumFDs: p.fds, NumThreads: p.threads,
			StartTime: start, ReadBytesSec: p.rootR + p.homeR, WriteBytesSec: p.rootW + p.homeW,
			RxBytesSec: p.rx, TxBytesSec: p.tx}
	}
	return basic, full
}

// --- Load average ---

const loadTick = 5.0 // the kernel samples the run queue every 5 seconds

// runq is the run-queue length (running plus blocked in I/O) at grid point k,
// estimated from the drivers with the process table's CPU coefficients. Memoized:
// every load average averages hundreds of grid points, and consecutive samples
// share almost all of them.
func (s *MockSim) runq(k int64) float64 {
	s.runqMu.Lock()
	v, ok := s.runqMemo[k]
	s.runqMu.Unlock()
	if ok {
		return v
	}
	t := float64(k) * loadTick
	d := s.drivers(t)
	busy := 0.18 + 1.88*d.web + 1.96*d.train + 1.65*d.backup
	blocked := 0.5*d.backup + 0.8*d.ckpt + 0.1*d.web
	v = math.Max(0, (busy+blocked)*(1+0.18*fbm(46, t, 25))+0.35*fbm(47, t, 6))
	s.runqMu.Lock()
	if len(s.runqMemo) > 1<<20 { // bounded; the live working set is ~540 points
		clear(s.runqMemo)
	}
	s.runqMemo[k] = v
	s.runqMu.Unlock()
	return v
}

// loadAvg computes the 1/5/15-minute load averages as the kernel does: an
// exponential moving average of the run queue sampled every 5 seconds. It
// steps every 5s, like the real thing, and the longer averages respond slowly.
func (s *MockSim) loadAvg(t float64) LoadData {
	k0 := int64(math.Floor(t / loadTick))
	avg := func(tau float64) float64 {
		e := math.Exp(-loadTick / tau)
		n := int(3 * tau / loadTick)
		var sum, wsum, w float64 = 0, 0, 1 - e
		for j := 0; j < n; j++ {
			sum += w * s.runq(k0-int64(j))
			wsum += w
			w *= e
		}
		return sum / wsum
	}
	return LoadData{Load1: avg(60), Load5: avg(300), Load15: avg(900)}
}
