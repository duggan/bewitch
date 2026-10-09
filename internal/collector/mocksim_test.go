package collector

import (
	"math"
	"reflect"
	"testing"
	"time"
)

var simAnchor = time.Date(2026, 10, 9, 15, 0, 0, 0, time.Local)

// simIncidentStart is when tests schedule the incident: as the daemon does,
// once seeding (which ends at the anchor) has finished.
var simIncidentStart = simAnchor.Add(25 * time.Second)

func newSim(t *testing.T, scenario string) *MockSim {
	t.Helper()
	s, err := NewMockSim(simAnchor, scenario)
	if err != nil {
		t.Fatal(err)
	}
	s.ScheduleScenario(simIncidentStart)
	return s
}

func busyPct(c CPUCoreSample) float64 { return 100 - c.IdlePct }

func pkgTemp(s MockSnapshot) float64 {
	for _, x := range s.Temperature.Sensors {
		if x.Sensor == "coretemp/Package id 0" {
			return x.TempCelsius
		}
	}
	return math.NaN()
}

func rootPct(s MockSnapshot) float64 {
	m := s.Disk.Mounts[0]
	return float64(m.UsedBytes) / float64(m.TotalBytes) * 100
}

func TestMockSimUnknownScenario(t *testing.T) {
	if _, err := NewMockSim(simAnchor, "meltdown"); err == nil {
		t.Fatal("expected an error for an unknown scenario")
	}
}

// The seeder and the live collectors are separate MockSims evaluated at
// different moments; they only agree if a reading depends on nothing but time.
func TestMockSimDeterministic(t *testing.T) {
	ts := simAnchor.Add(-37*time.Minute - 12*time.Second)
	a, b := newSim(t, ""), newSim(t, "")
	sa, sb := a.Snapshot(ts), b.Snapshot(ts)
	if !reflect.DeepEqual(sa, sb) {
		t.Fatal("two simulations disagree at the same instant")
	}
}

// Every panel of the TUI must tell the same story: the aggregate is the mean
// of the cores, the processes account for the aggregate, memory adds up.
func TestMockSimConsistent(t *testing.T) {
	s := newSim(t, MockScenarioIncident)
	check := func(ts time.Time) {
		snap := s.Snapshot(ts)
		agg := snap.CPU.Cores[0]
		if agg.Core != -1 || len(snap.CPU.Cores) != mockCores+1 {
			t.Fatalf("%v: want aggregate + %d cores, got %d rows", ts, mockCores, len(snap.CPU.Cores))
		}
		if agg.IdlePct < 0 || agg.IdlePct > 100 {
			t.Fatalf("%v: aggregate idle %.1f out of range", ts, agg.IdlePct)
		}
		var mean float64
		for _, c := range snap.CPU.Cores[1:] {
			if c.IdlePct < 0 || busyPct(c) > 100 {
				t.Fatalf("%v: core %d busy %.1f out of range", ts, c.Core, busyPct(c))
			}
			mean += c.UserPct + c.SystemPct
		}
		mean /= mockCores
		if d := math.Abs(mean - (agg.UserPct + agg.SystemPct)); d > 3 {
			t.Errorf("%v: per-core mean %.1f vs aggregate %.1f", ts, mean, agg.UserPct+agg.SystemPct)
		}
		all, full := snap.Processes()
		var procCPU float64
		for i, p := range all {
			procCPU += p.CPUPct
			if i > 0 && p.CPUPct > all[i-1].CPUPct {
				t.Fatalf("%v: processes not sorted by CPU", ts)
			}
			if full[i].PID != p.PID {
				t.Fatalf("%v: basic and full process lists out of step", ts)
			}
		}
		if d := procCPU/mockCores - (agg.UserPct + agg.SystemPct); math.Abs(d) > 8 {
			t.Errorf("%v: processes sum to %.1f%%, aggregate user+sys %.1f%%", ts, procCPU/mockCores, agg.UserPct+agg.SystemPct)
		}
		m := snap.Memory
		if m.UsedBytes+m.CachedBytes+m.BuffersBytes > m.TotalBytes {
			t.Errorf("%v: memory over-committed: used %d + cached %d + buffers %d > %d", ts, m.UsedBytes, m.CachedBytes, m.BuffersBytes, m.TotalBytes)
		}
		for _, v := range []float64{snap.Load.Load1, snap.Load.Load5, snap.Load.Load15, pkgTemp(snap),
			snap.Disk.Mounts[0].WriteBytesSec, snap.Network.Interfaces[0].TxBytesSec, snap.GPU.GPUs[1].PowerWatts} {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				t.Fatalf("%v: bad reading %v", ts, v)
			}
		}
	}
	for off := -30 * 24 * time.Hour; off < 0; off += 97 * time.Minute {
		check(simAnchor.Add(off))
	}
	for off := time.Duration(0); off < 3*time.Minute; off += 3 * time.Second {
		check(simAnchor.Add(off))
	}
}

// Seeded history ends where live collection begins; there must be no jump.
func TestMockSimNoSeam(t *testing.T) {
	s := newSim(t, "")
	before, after := s.Snapshot(simAnchor.Add(-2*time.Second)), s.Snapshot(simAnchor.Add(2*time.Second))
	if d := math.Abs(busyPct(before.CPU.Cores[0]) - busyPct(after.CPU.Cores[0])); d > 12 {
		t.Errorf("CPU jumps %.1f points across the seam", d)
	}
	if d := math.Abs(rootPct(before) - rootPct(after)); d > 0.1 {
		t.Errorf("root disk jumps %.2f points across the seam", d)
	}
	if d := math.Abs(pkgTemp(before) - pkgTemp(after)); d > 4 {
		t.Errorf("package temperature jumps %.1f°C across the seam", d)
	}
}

// The demo alert rules (package temperature max > 85 over 1m, root disk max >
// 90 over 1m, memory variance) must never fire on the baseline, and the two
// threshold rules must fire and then clear during the incident — whatever time
// of day the demo runs, since load follows the clock.
func TestMockSimIncidentTripsAndRecovers(t *testing.T) {
	for _, hour := range []int{3, 9, 15, 23} {
		anchor := time.Date(2026, 10, 9, hour, 30, 0, 0, time.Local)
		base, err := NewMockSim(anchor, "")
		if err != nil {
			t.Fatal(err)
		}
		for off := -30 * 24 * time.Hour; off < 10*time.Minute; off += 7 * time.Minute {
			snap := base.Snapshot(anchor.Add(off))
			if v := pkgTemp(snap); v > 83 {
				t.Fatalf("%02d:30: baseline package temperature %.1f°C at %v would trip the demo alert", hour, v, off)
			}
			if v := rootPct(snap); v > 89 {
				t.Fatalf("%02d:30: baseline root disk %.1f%% at %v would trip the demo alert", hour, v, off)
			}
		}
		// Memory variance (5% swings, 10 in 30m): consecutive 1s samples must not
		// swing 5% on the baseline.
		prev := -1.0
		for off := -30 * time.Minute; off < 0; off += time.Second {
			m := base.Snapshot(anchor.Add(off)).Memory
			pct := float64(m.UsedBytes) / float64(m.TotalBytes) * 100
			if prev >= 0 && math.Abs(pct-prev) >= 5 {
				t.Fatalf("%02d:30: memory swings %.1f%% → %.1f%% in one second", hour, prev, pct)
			}
			prev = pct
		}

		s, _ := NewMockSim(anchor, MockScenarioIncident)
		start := anchor.Add(25 * time.Second)
		s.ScheduleScenario(start)
		if v := pkgTemp(s.Snapshot(start.Add(-time.Second))); v > 80 {
			t.Fatalf("%02d:30: incident running early: %.1f°C before it starts", hour, v)
		}
		var hot, full, restic time.Duration
		for off := time.Duration(0); off < 90*time.Second; off += time.Second {
			snap := s.Snapshot(start.Add(off))
			if pkgTemp(snap) > 86.5 {
				hot += time.Second
			}
			if rootPct(snap) > 91 {
				full += time.Second
			}
			if all, _ := snap.Processes(); all[0].Name == "restic" {
				restic += time.Second
			}
		}
		if hot < 15*time.Second || full < 10*time.Second || restic < 30*time.Second {
			t.Fatalf("%02d:30: incident too weak: >86.5°C for %v, root >91%% for %v, restic on top for %v", hour, hot, full, restic)
		}
		after := s.Snapshot(start.Add(150 * time.Second))
		if pkgTemp(after) > 72 || rootPct(after) > 89 {
			t.Fatalf("%02d:30: no recovery: %.1f°C, root %.1f%%", hour, pkgTemp(after), rootPct(after))
		}
	}
}

func TestMockSimScheduleScenario(t *testing.T) {
	s := newSim(t, MockScenarioIncident)
	start, ok := s.ScenarioStart()
	if !ok || !start.Equal(simIncidentStart) {
		t.Fatalf("scenario start = %v, %v", start, ok)
	}
	if !s.ScheduleScenario(simIncidentStart.Add(time.Hour)) {
		t.Fatal("ScheduleScenario should report the configured scenario")
	}
	if again, _ := s.ScenarioStart(); !again.Equal(start) {
		t.Fatal("a second ScheduleScenario moved the incident")
	}
	if newSim(t, "").ScheduleScenario(simIncidentStart) {
		t.Fatal("ScheduleScenario with no scenario configured should report false")
	}
}

// The load averages must behave like the kernel's: the 1-minute figure reacts
// to the incident, the 15-minute one barely moves.
func TestMockSimLoadAverage(t *testing.T) {
	s := newSim(t, MockScenarioIncident)
	start, _ := s.ScenarioStart()
	before := s.Snapshot(start.Add(-5 * time.Second)).Load
	peak := s.Snapshot(start.Add(60 * time.Second)).Load
	if peak.Load1-before.Load1 < 1.5 {
		t.Errorf("load1 barely reacted: %.2f → %.2f", before.Load1, peak.Load1)
	}
	if peak.Load15-before.Load15 > (peak.Load1-before.Load1)/3 {
		t.Errorf("load15 reacted too fast: %.2f → %.2f (load1 %.2f → %.2f)", before.Load15, peak.Load15, before.Load1, peak.Load1)
	}
}

func TestMockProcessData(t *testing.T) {
	s := newSim(t, "")
	all, full := s.Snapshot(simAnchor).Processes()
	data := MockProcessData(all, full, 5, []string{"nginx"})
	var nginx int
	for i, p := range data.Processes {
		if p.Name == "nginx" {
			nginx++
		} else if i >= 5+nginx {
			t.Errorf("unpinned %s enriched beyond the top 5", p.Name)
		}
	}
	if nginx != 5 {
		t.Errorf("pinned nginx: got %d of 5 processes enriched", nginx)
	}
	if data.TotalProcs != int32(len(all)) {
		t.Errorf("TotalProcs = %d, want %d", data.TotalProcs, len(all))
	}
}

// The GPU is part of the demo: a training job must be running whenever the
// daemon starts, including right at a job-slot boundary.
func TestMockSimTrainingAtStart(t *testing.T) {
	base := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	for _, off := range []time.Duration{0, time.Minute, 5 * time.Minute, 2*time.Hour + 17*time.Minute, 5*time.Hour + 58*time.Minute, 6 * time.Hour} {
		anchor := base.Add(off)
		s, _ := NewMockSim(anchor, "")
		for _, after := range []time.Duration{3 * time.Minute, 10 * time.Minute} {
			if u := s.Snapshot(anchor.Add(after)).GPU.GPUs[1].UtilizationPct; u < 30 {
				t.Errorf("start %v +%v: GPU idle (%.0f%%)", off, after, u)
			}
		}
	}
}
