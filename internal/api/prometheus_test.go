package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrometheusEndpoint(t *testing.T) {
	s := &Server{}
	s.SetMetricsSnapshot(
		[]CPUCoreMetric{{Core: -1, UserPct: 10, SystemPct: 5, IdlePct: 80, IOWaitPct: 2, StealPct: 3}},
		&MemoryMetric{TotalBytes: 1000, UsedBytes: 400},
		[]DiskMetric{{Mount: "/", Device: "/dev/sda1", TotalBytes: 100, UsedBytes: 50, FreeBytes: 50,
			InodesTotal: 1000, InodesFree: 900, SMARTAvailable: true, SMARTHealthy: true, SMARTTemperature: 40, SMARTReallocated: 2}},
		[]NetworkMetric{{Interface: "eth0", RxBytesSec: 100, RxDropped: 7}},
		[]TemperatureMetric{{Sensor: "coretemp/Core 0", TempCelsius: 55}},
		[]PowerMetric{{Zone: "package-0", Watts: 30}},
		&ECCMetric{Corrected: 1, Uncorrected: 0},
	)
	s.SetLoadSnapshot(&LoadMetric{Load1: 1.5, Load5: 1.2, Load15: 0.9})

	w := httptest.NewRecorder()
	s.handlePrometheus(w, httptest.NewRequest("GET", "/metrics", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain...", ct)
	}
	body := w.Body.String()

	want := []string{
		"# TYPE bewitch_load_average gauge",
		`bewitch_load_average{period="1m"} 1.5`,
		`bewitch_cpu_percent{core="-1",mode="steal"} 3`,
		`bewitch_memory_bytes{kind="total"} 1000`,
		`bewitch_disk_inodes{mount="/",kind="total"} 1000`,
		`bewitch_smart_healthy{device="/dev/sda1"} 1`,
		`bewitch_smart_reallocated_sectors{device="/dev/sda1"} 2`,
		`bewitch_network_dropped_total{interface="eth0",direction="rx"} 7`,
		`bewitch_temperature_celsius{sensor="coretemp/Core 0"} 55`,
		`bewitch_power_watts{zone="package-0"} 30`,
		`bewitch_ecc_errors_total{kind="corrected"} 1`,
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("output missing line:\n  %s\n--- full ---\n%s", w, body)
		}
	}
	// HELP/TYPE header emitted exactly once per family.
	if n := strings.Count(body, "# HELP bewitch_cpu_percent "); n != 1 {
		t.Errorf("bewitch_cpu_percent HELP appeared %d times, want 1", n)
	}
}

func TestPrometheusEndpointEmpty(t *testing.T) {
	// No cached metrics: must still return 200 with the right content type and no panic.
	s := &Server{}
	w := httptest.NewRecorder()
	s.handlePrometheus(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}
