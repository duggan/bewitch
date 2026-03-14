package tui

import (
	"sync"
	"time"

	"github.com/duggan/bewitch/internal/api"
)

// mockClient implements daemonClient with canned responses for testing.
type mockClient struct {
	mu sync.Mutex

	cpu    []api.CPUCoreMetric
	mem    *api.MemoryMetric
	ecc    *api.ECCMetric
	disk   []api.DiskMetric
	net    []api.NetworkMetric
	temp   []api.TemperatureMetric
	power  []api.PowerMetric
	procs  *api.ProcessResponse
	dash   *api.DashboardData
	alerts []api.AlertMetric
	rules  []api.AlertRuleMetric
	prefs  map[string]string
	hist   []api.TimeSeries
	status map[string]any

	// Track SetPreference calls
	prefSets []struct{ key, value string }
}

func (m *mockClient) GetStatus() (map[string]any, error) {
	if m.status != nil {
		return m.status, nil
	}
	return map[string]any{
		"status":           "ok",
		"uptime_sec":       int64(3600),
		"default_interval": "5s",
	}, nil
}

func (m *mockClient) GetDashboard() (*api.DashboardData, error) {
	if m.dash != nil {
		return m.dash, nil
	}
	return &api.DashboardData{
		CPU:    m.cpu,
		Memory: m.mem,
		Disks:  m.disk,
	}, nil
}

func (m *mockClient) GetCPU() ([]api.CPUCoreMetric, error) { return m.cpu, nil }

func (m *mockClient) GetMemory() (*api.MemoryMetric, error) { return m.mem, nil }

func (m *mockClient) GetECC() (*api.ECCMetric, error) {
	if m.ecc != nil {
		return m.ecc, nil
	}
	return &api.ECCMetric{}, nil
}

func (m *mockClient) GetDisk() ([]api.DiskMetric, error) { return m.disk, nil }

func (m *mockClient) GetNetwork() ([]api.NetworkMetric, error) { return m.net, nil }

func (m *mockClient) GetTemperature() ([]api.TemperatureMetric, error) { return m.temp, nil }

func (m *mockClient) GetPower() ([]api.PowerMetric, error) { return m.power, nil }

func (m *mockClient) GetProcesses() (*api.ProcessResponse, error) {
	if m.procs != nil {
		return m.procs, nil
	}
	return &api.ProcessResponse{}, nil
}

func (m *mockClient) GetAlerts() ([]api.AlertMetric, error) { return m.alerts, nil }

func (m *mockClient) GetHistory(_ string, _, _ time.Time) ([]api.TimeSeries, error) {
	return m.hist, nil
}

func (m *mockClient) GetHistoryByName(_ string, _, _ time.Time, _ []string) ([]api.TimeSeries, error) {
	return m.hist, nil
}

func (m *mockClient) GetAlertRules() ([]api.AlertRuleMetric, error) { return m.rules, nil }

func (m *mockClient) CreateAlertRule(_ api.AlertRuleMetric) error { return nil }

func (m *mockClient) DeleteAlertRule(_ int) error { return nil }

func (m *mockClient) ToggleAlertRule(_ int) error { return nil }

func (m *mockClient) AckAlert(_ int) error { return nil }

func (m *mockClient) GetPreferences() (map[string]string, error) {
	if m.prefs != nil {
		return m.prefs, nil
	}
	return map[string]string{}, nil
}

func (m *mockClient) SetPreference(key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prefSets = append(m.prefSets, struct{ key, value string }{key, value})
	return nil
}

func (m *mockClient) Compact() error { return nil }

func (m *mockClient) TestNotifications(_ TestNotificationAlert) ([]NotifyTestResult, error) {
	return nil, nil
}

// newMockClient returns a mockClient populated with representative sample data.
func newMockClient() *mockClient {
	return &mockClient{
		cpu: []api.CPUCoreMetric{
			{Core: -1, UserPct: 25.0, SystemPct: 10.0, IdlePct: 60.0, IOWaitPct: 5.0},
			{Core: 0, UserPct: 30.0, SystemPct: 8.0, IdlePct: 57.0, IOWaitPct: 5.0},
			{Core: 1, UserPct: 20.0, SystemPct: 12.0, IdlePct: 63.0, IOWaitPct: 5.0},
		},
		mem: &api.MemoryMetric{
			TotalBytes:     16_000_000_000,
			UsedBytes:      8_000_000_000,
			AvailableBytes: 7_000_000_000,
			CachedBytes:    3_000_000_000,
			SwapTotalBytes: 4_000_000_000,
			SwapUsedBytes:  100_000_000,
		},
		disk: []api.DiskMetric{
			{
				Mount: "/", Device: "nvme0n1p2", Transport: "nvme",
				TotalBytes: 500_000_000_000, UsedBytes: 200_000_000_000, FreeBytes: 300_000_000_000,
			},
		},
		net: []api.NetworkMetric{
			{Interface: "eth0", RxBytesSec: 125_000, TxBytesSec: 50_000},
		},
		temp: []api.TemperatureMetric{
			{Sensor: "coretemp/0", TempCelsius: 55.0},
			{Sensor: "coretemp/1", TempCelsius: 52.0},
		},
		power: []api.PowerMetric{
			{Zone: "package-0", Watts: 45.5},
		},
		procs: &api.ProcessResponse{
			TotalProcs:   150,
			RunningProcs: 3,
			ActiveProcs:  10,
			TotalCPUPct:  35.0,
			Processes: []api.ProcessMetric{
				{PID: 1, Name: "systemd", State: "S", CPUUserPct: 0.1, RSSBytes: 10_000_000, NumThreads: 1, Enriched: true},
				{PID: 100, Name: "nginx", State: "S", CPUUserPct: 5.0, RSSBytes: 50_000_000, NumThreads: 4, NumFDs: 120, Enriched: true, Cmdline: "nginx: master process"},
				{PID: 200, Name: "postgres", State: "S", CPUUserPct: 15.0, RSSBytes: 200_000_000, NumThreads: 8, NumFDs: 300, Enriched: true, Cmdline: "postgres -D /var/lib/postgresql"},
				{PID: 300, Name: "redis-server", State: "S", CPUUserPct: 2.0, RSSBytes: 30_000_000, NumThreads: 4, Enriched: false},
			},
		},
		prefs: map[string]string{},
	}
}
