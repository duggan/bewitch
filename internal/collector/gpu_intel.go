package collector

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The Intel backend reads the GPU directly, needing no external tool and no
// privilege beyond what the daemon already holds:
//
//   - utilization from DRM fdinfo: since kernel 5.19 the i915 and xe drivers
//     publish each open DRM file's cumulative busy time per engine class in
//     /proc/<pid>/fdinfo/<fd> (Documentation/gpu/drm-usage-stats.rst). Reading
//     other users' fdinfo needs CAP_SYS_PTRACE, which the packaged unit grants
//     for per-process disk I/O;
//   - clocks and RC6 residency from world-readable sysfs;
//   - power from the card's hwmon energy counter (discrete) or the RAPL uncore
//     counter (integrated), which `bewitchd rapl-access` makes readable.
//
// It replaced a backend that kept intel_gpu_top running. That tool reads the
// i915 perf PMU, which Debian's default perf_event_paranoid=3 restricts to
// CAP_SYS_ADMIN, so on stock Debian it reported nothing.

// Package vars so tests can use fixture trees.
var (
	intelDRMGlob = "/sys/class/drm/card[0-9]*"
	intelProcDir = "/proc"
)

// intelClientRescan is how often /proc is scanned for processes with a DRM
// device open. Walking every process's fd table is the expensive part, so
// between scans only the known clients' fdinfo files are read; a new GPU
// client is picked up within this interval.
const intelClientRescan = time.Minute

type intelCard struct {
	name    string
	index   int
	pdev    string // PCI address, matched against fdinfo drm-pdev
	freqAct string // sysfs file paths; "" when absent
	freqMax string
	rc6     string
	energy  string // energy counter in µJ; "" when no source is readable
	wrapUJ  uint64 // counter range for wrap recovery; 0 = unknown
}

// intelEngineUse is one client's cumulative use of one engine class.
type intelEngineUse struct {
	busy  uint64 // i915: busy ns; xe: busy cycles
	total uint64 // xe: total GPU cycles over the same period; 0 for i915
}

type intelCardSnap struct {
	at     time.Time
	rc6ms  uint64
	energy uint64
	use    map[string]map[string]intelEngineUse // client id → engine class → use
	cap    map[string]uint64                    // engine class → engine count
	ok     bool
}

type intelGPUBackend struct {
	cards    []intelCard
	clients  []string // fdinfo paths of open /dev/dri files
	lastScan time.Time
	prev     map[int]intelCardSnap // by card index
	now      func() time.Time
}

func newIntelGPUBackend() *intelGPUBackend {
	cards := discoverIntelCards()
	if len(cards) == 0 {
		return nil
	}
	return &intelGPUBackend{cards: cards, prev: make(map[int]intelCardSnap), now: time.Now}
}

// discoverIntelCards finds DRM cards driven by i915 or xe.
func discoverIntelCards() []intelCard {
	matches, _ := filepath.Glob(intelDRMGlob)
	var cards []intelCard
	for _, card := range matches {
		if strings.Contains(filepath.Base(card), "-") { // connectors: card0-DP-1
			continue
		}
		dev := filepath.Join(card, "device")
		driverLink, err := os.Readlink(filepath.Join(dev, "driver"))
		if err != nil {
			continue
		}
		driver := filepath.Base(driverLink)
		if driver != "i915" && driver != "xe" {
			continue
		}
		c := intelCard{name: intelCardName(dev), index: len(cards)}
		if real, err := filepath.EvalSymlinks(dev); err == nil {
			c.pdev = filepath.Base(real)
		}
		if driver == "i915" {
			c.freqAct = existing(filepath.Join(card, "gt_act_freq_mhz"))
			c.freqMax = existing(filepath.Join(card, "gt_max_freq_mhz"))
			c.rc6 = existing(filepath.Join(card, "gt", "gt0", "rc6_residency_ms"), filepath.Join(card, "power", "rc6_residency_ms"))
		} else {
			gt := filepath.Join(dev, "tile0", "gt0")
			c.freqAct = existing(filepath.Join(gt, "freq0", "act_freq"))
			c.freqMax = existing(filepath.Join(gt, "freq0", "max_freq"))
			c.rc6 = existing(filepath.Join(gt, "gtidle", "idle_residency_ms"))
		}
		c.energy, c.wrapUJ = intelEnergySource(dev)
		cards = append(cards, c)
	}
	return cards
}

func intelCardName(dev string) string {
	if label := readString(filepath.Join(dev, "label")); label != "" {
		return label
	}
	if device := readString(filepath.Join(dev, "device")); device != "" && readString(filepath.Join(dev, "vendor")) == "0x8086" {
		return "Intel GPU " + device
	}
	return "Intel GPU"
}

// existing returns the first path that exists, or "".
func existing(paths ...string) string {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// intelEnergySource picks the GPU's energy counter: a discrete card's own hwmon
// energy1_input, else the RAPL "uncore" domain that covers an integrated GPU.
// Only a readable counter is returned.
func intelEnergySource(dev string) (string, uint64) {
	if hw, _ := filepath.Glob(filepath.Join(dev, "hwmon", "hwmon*", "energy1_input")); len(hw) > 0 {
		if _, err := os.ReadFile(hw[0]); err == nil {
			return hw[0], 0
		}
	}
	names, _ := filepath.Glob(filepath.Join(powercapRoot, "intel-rapl:*", "intel-rapl:*:*", "name"))
	for _, n := range names {
		if readString(n) != "uncore" {
			continue
		}
		dir := filepath.Dir(n)
		p := filepath.Join(dir, "energy_uj")
		if _, err := os.ReadFile(p); err == nil {
			return p, readUint(filepath.Join(dir, "max_energy_range_uj"))
		}
	}
	return "", 0
}

func (b *intelGPUBackend) read() ([]GPUDeviceSample, error) {
	now := b.now()
	if b.clients == nil || now.Sub(b.lastScan) >= intelClientRescan {
		b.clients = scanDRMClients()
		b.lastScan = now
	}
	fdinfo := b.readClients()

	var out []GPUDeviceSample
	for _, c := range b.cards {
		cur := intelCardSnap{at: now, use: map[string]map[string]intelEngineUse{}, cap: map[string]uint64{}, ok: true}
		for _, info := range fdinfo {
			if info.pdev != "" && c.pdev != "" && info.pdev != c.pdev {
				continue // another card's client
			}
			cur.use[info.id] = info.use
			for class, n := range info.cap {
				cur.cap[class] = n
			}
		}
		if c.rc6 != "" {
			cur.rc6ms = readUint(c.rc6)
		}
		if c.energy != "" {
			cur.energy = readUint(c.energy)
		}

		prev := b.prev[c.index]
		b.prev[c.index] = cur
		if !prev.ok {
			continue // first reading is the baseline for every rate
		}

		s := GPUDeviceSample{
			Name:            c.name,
			Index:           c.index,
			Vendor:          "intel",
			UtilizationPct:  intelBusiestEngine(prev, cur),
			FrequencyMHz:    uint32(readUint(c.freqAct)),
			FrequencyMaxMHz: uint32(readUint(c.freqMax)),
		}
		dt := cur.at.Sub(prev.at)
		if dt > 0 && c.rc6 != "" && cur.rc6ms >= prev.rc6ms {
			s.ThrottlePct = min(100, float64(cur.rc6ms-prev.rc6ms)/(float64(dt)/float64(time.Millisecond))*100)
		}
		if dt > 0 && c.energy != "" {
			d := cur.energy - prev.energy
			if cur.energy < prev.energy {
				if c.wrapUJ == 0 {
					d = 0
				} else {
					d = cur.energy + c.wrapUJ - prev.energy
				}
			}
			s.PowerWatts = float64(d) / 1e6 / dt.Seconds()
		}
		out = append(out, s)
	}
	return out, nil
}

// intelBusiestEngine returns the busiest engine class's utilization % between
// two snapshots, summing the clients present in both (a client that opened or
// closed in between has no delta to count).
func intelBusiestEngine(prev, cur intelCardSnap) float64 {
	dtNS := float64(cur.at.Sub(prev.at).Nanoseconds())
	if dtNS <= 0 {
		return 0
	}
	busy := map[string]float64{} // engine class → fraction of one engine
	for id, classes := range cur.use {
		before, ok := prev.use[id]
		if !ok {
			continue
		}
		for class, u := range classes {
			p, ok := before[class]
			if !ok || u.busy < p.busy {
				continue
			}
			switch {
			case u.total > 0: // xe: cycles over GPU cycles
				if u.total > p.total {
					busy[class] += float64(u.busy-p.busy) / float64(u.total-p.total)
				}
			default: // i915: busy ns over wall time
				busy[class] += float64(u.busy-p.busy) / dtNS
			}
		}
	}
	var top float64
	for class, f := range busy {
		n := cur.cap[class]
		if n == 0 {
			n = 1
		}
		top = max(top, min(100, f/float64(n)*100))
	}
	return top
}

// scanDRMClients returns the fdinfo path of every open i915/xe DRM file.
//
// It lists /proc/<pid>/fdinfo and looks for the drm-driver line rather than
// resolving /proc/<pid>/fd symlinks: another user's fd directory needs file
// ownership or CAP_DAC_READ_SEARCH, while fdinfo only needs the ptrace read
// check that CAP_SYS_PTRACE passes. The drm-* lines follow a few short header
// lines (pos, flags, mnt_id, ino), so reading the start of each file is enough.
func scanDRMClients() []string {
	clients := []string{}
	head := make([]byte, 1024)
	procs, _ := os.ReadDir(intelProcDir)
	for _, p := range procs {
		if _, err := strconv.Atoi(p.Name()); err != nil {
			continue
		}
		dir := filepath.Join(intelProcDir, p.Name(), "fdinfo")
		fds, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			path := filepath.Join(dir, fd.Name())
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			n, _ := f.Read(head)
			f.Close()
			if driver := drmDriver(head[:n]); driver == "i915" || driver == "xe" {
				clients = append(clients, path)
			}
		}
	}
	return clients
}

// drmDriver returns the value of an fdinfo's drm-driver line, or "".
func drmDriver(fdinfo []byte) string {
	for _, line := range strings.Split(string(fdinfo), "\n") {
		if v, ok := strings.CutPrefix(line, "drm-driver:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

type drmClientInfo struct {
	id   string // drm-client-id, unique per open DRM file (dup'd and inherited fds share it)
	pdev string
	use  map[string]intelEngineUse
	cap  map[string]uint64
}

// readClients parses the known clients' fdinfo, dropping files that are gone,
// and returns one entry per DRM client.
func (b *intelGPUBackend) readClients() map[string]drmClientInfo {
	byID := map[string]drmClientInfo{}
	kept := b.clients[:0]
	for _, path := range b.clients {
		info, err := parseDRMFdinfo(path)
		if err != nil {
			continue // process exited or closed the fd
		}
		kept = append(kept, path)
		if info.id == "" {
			continue
		}
		byID[info.pdev+"/"+info.id] = info
	}
	b.clients = kept
	return byID
}

// parseDRMFdinfo reads the drm-* keys of one fdinfo file:
//
//	drm-driver:            i915 | xe
//	drm-pdev:              0000:00:02.0
//	drm-client-id:         7
//	drm-engine-render:     123456 ns     (i915)
//	drm-engine-capacity-video: 2         (i915, when >1)
//	drm-cycles-rcs:        12345         (xe)
//	drm-total-cycles-rcs:  67890         (xe)
func parseDRMFdinfo(path string) (drmClientInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return drmClientInfo{}, err
	}
	defer f.Close()
	info := drmClientInfo{use: map[string]intelEngineUse{}, cap: map[string]uint64{}}
	driver := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok || !strings.HasPrefix(key, "drm-") {
			continue
		}
		val = strings.TrimSpace(val)
		num := func() uint64 {
			v, _ := strconv.ParseUint(strings.Fields(val + " x")[0], 10, 64)
			return v
		}
		switch {
		case key == "drm-driver":
			driver = val
		case key == "drm-pdev":
			info.pdev = val
		case key == "drm-client-id":
			info.id = val
		case strings.HasPrefix(key, "drm-engine-capacity-"):
			info.cap[strings.TrimPrefix(key, "drm-engine-capacity-")] = num()
		case strings.HasPrefix(key, "drm-engine-"):
			class := strings.TrimPrefix(key, "drm-engine-")
			u := info.use[class]
			u.busy = num()
			info.use[class] = u
		case strings.HasPrefix(key, "drm-total-cycles-"):
			class := strings.TrimPrefix(key, "drm-total-cycles-")
			u := info.use[class]
			u.total = num()
			info.use[class] = u
		case strings.HasPrefix(key, "drm-cycles-"):
			class := strings.TrimPrefix(key, "drm-cycles-")
			u := info.use[class]
			u.busy = num()
			info.use[class] = u
		}
	}
	if err := sc.Err(); err != nil {
		return drmClientInfo{}, err
	}
	if driver != "i915" && driver != "xe" {
		return drmClientInfo{}, nil // another GPU's client: keep the path, count nothing
	}
	return info, nil
}

func (b *intelGPUBackend) stop() {}
