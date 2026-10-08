package collector

import (
	"math"
	"time"

	"github.com/duggan/bewitch/internal/config"
)

// MockCustomSources returns synthetic custom-source definitions so the Services
// tab is exercisable in mock mode (macOS development, screenshots). Paths are
// irrelevant — the mock collector synthesizes values from the metric specs.
func MockCustomSources() []config.CustomSourceConfig {
	return []config.CustomSourceConfig{
		{
			Name:    "pihole",
			BaseURL: "http://mock",
			Metrics: []config.CustomMetricSpec{
				{Name: "queries", Path: "dns_queries_today", Unit: "count"},
				{Name: "blocked", Path: "ads_blocked_today", Unit: "count"},
				{Name: "block_pct", Path: "ads_percentage_today", Unit: "percent"},
			},
			Status: []config.CustomStatusSpec{
				{Label: "Blocking", Path: "status"},
			},
		},
		{
			Name:    "homeassistant",
			BaseURL: "http://mock",
			Metrics: []config.CustomMetricSpec{
				{Name: "entities", Path: "entities", Unit: "count"},
				{Name: "automations", Path: "automations", Unit: "count"},
			},
			Status: []config.CustomStatusSpec{
				{Label: "Connection", Path: "state"},
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

// MockCustomValue returns a plausible value for one of MockCustomSources'
// metrics at time t (unix seconds), so the live mock collector and the
// history seeder agree and the Services tab looks like the real services:
// Pi-hole's blocked count and percentage are consistent with its query count,
// Home Assistant's entity/automation counts are steady. ok is false for
// metrics it doesn't model.
func MockCustomValue(source, metric string, t float64) (v float64, ok bool) {
	wave := func(min, max, period, phase float64) float64 {
		return min + (max-min)*(math.Sin(t*2*math.Pi/period+phase)+1)/2
	}
	queries := math.Round(wave(18000, 46000, 86400, 0))
	blockedFrac := wave(0.18, 0.31, 21600, 1.3)
	switch source + "/" + metric {
	case "pihole/queries":
		return queries, true
	case "pihole/blocked":
		return math.Round(queries * blockedFrac), true
	case "pihole/block_pct":
		return math.Round(blockedFrac*1000) / 10, true
	case "homeassistant/entities":
		return math.Round(wave(181, 189, 7200, 0.4)), true
	case "homeassistant/automations":
		return math.Round(wave(23, 26, 43200, 2.1)), true
	}
	return 0, false
}

func (c *MockCustomSourceCollector) Collect() (Sample, error) {
	data := CustomSourceData{Source: c.cfg.Name}
	now := float64(time.Now().UnixNano()) / 1e9
	for i, m := range c.cfg.Metrics {
		if v, ok := MockCustomValue(c.cfg.Name, m.Name, now); ok {
			data.Metrics = append(data.Metrics, CustomMetricSample{Name: m.Name, Unit: m.Unit, Value: v})
			continue
		}
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
		case "pihole":
			val = "enabled"
		case "homeassistant":
			val = "connected"
		}
		data.Status = append(data.Status, CustomStatusSample{Label: st.Label, Value: val, Badge: badge})
	}
	return Sample{Timestamp: time.Now(), Kind: "custom", Data: data}, nil
}
