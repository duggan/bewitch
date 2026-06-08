package collector

import (
	"time"

	"github.com/duggan/bewitch/internal/config"
)

// MockCustomSources returns synthetic custom-source definitions so the Services
// tab is exercisable in mock mode (macOS development, screenshots). Paths are
// irrelevant — the mock collector synthesizes values from the metric specs.
func MockCustomSources() []config.CustomSourceConfig {
	return []config.CustomSourceConfig{
		{
			Name:    "qbittorrent",
			BaseURL: "http://mock",
			Metrics: []config.CustomMetricSpec{
				{Name: "dl_speed", Path: "dl_info_speed", Unit: "bytes"},
				{Name: "up_speed", Path: "up_info_speed", Unit: "bytes"},
				{Name: "active_torrents", Path: "active", Unit: "count"},
			},
			Status: []config.CustomStatusSpec{
				{Label: "Connection", Path: "connection_status"},
			},
		},
		{
			Name:    "plex",
			BaseURL: "http://mock",
			Metrics: []config.CustomMetricSpec{
				{Name: "streams", Path: "size", Unit: "count"},
				{Name: "bandwidth", Path: "bandwidth", Unit: "bits"},
			},
			Status: []config.CustomStatusSpec{
				{Label: "Server", Path: "version"},
			},
		},
	}
}

// MockCustomSourceCollector emits synthetic values matching a source's metric
// and status specs.
type MockCustomSourceCollector struct {
	cfg config.CustomSourceConfig
}

func NewMockCustomSourceCollector(cfg config.CustomSourceConfig) *MockCustomSourceCollector {
	return &MockCustomSourceCollector{cfg: cfg}
}

func (c *MockCustomSourceCollector) Name() string { return "custom:" + c.cfg.Name }

func (c *MockCustomSourceCollector) Collect() (Sample, error) {
	data := CustomSourceData{Source: c.cfg.Name}
	for i, m := range c.cfg.Metrics {
		phase := float64(i) * 0.7
		var v float64
		switch m.Unit {
		case "bytes":
			v = smoothWave(1<<20, 12<<20, 50, phase)
		case "bits":
			v = smoothWave(2e6, 8e7, 60, phase)
		case "percent":
			v = smoothWave(5, 95, 45, phase)
		case "count":
			v = smoothWave(0, 12, 90, phase)
		default:
			v = smoothWave(10, 100, 60, phase)
		}
		data.Metrics = append(data.Metrics, CustomMetricSample{Name: m.Name, Unit: m.Unit, Value: v})
	}
	for _, st := range c.cfg.Status {
		val, badge := "ok", "ok"
		switch c.cfg.Name {
		case "qbittorrent":
			val = "connected"
		case "plex":
			val, badge = "1.40.2", ""
		}
		data.Status = append(data.Status, CustomStatusSample{Label: st.Label, Value: val, Badge: badge})
	}
	return Sample{Timestamp: time.Now(), Kind: "custom", Data: data}, nil
}
