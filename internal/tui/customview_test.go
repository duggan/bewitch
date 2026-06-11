package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/api"
	"github.com/duggan/bewitch/internal/config"
)

func mockClientWithSources() *mockClient {
	mc := newMockClient()
	mc.customSources = []api.CustomSourceInfo{
		{
			Name:    "homeassistant",
			Metrics: []api.CustomFieldInfo{{Name: "throughput", Unit: "bytes"}, {Name: "automations", Unit: "count"}},
			Status:  []api.CustomFieldInfo{{Name: "Connection"}},
		},
		{
			Name:    "pihole",
			Metrics: []api.CustomFieldInfo{{Name: "blocked", Unit: "count"}},
		},
	}
	mc.customMetrics = []api.CustomMetric{
		{Source: "homeassistant", Name: "throughput", Unit: "bytes", Value: 1048576},
		{Source: "homeassistant", Name: "automations", Unit: "count", Value: 24},
		{Source: "pihole", Name: "blocked", Unit: "count", Value: 9102},
	}
	mc.customStatus = []api.CustomStatus{
		{Source: "homeassistant", Label: "Connection", Value: "connected", Badge: "ok"},
	}
	return mc
}

func TestServicesTabVisibility(t *testing.T) {
	// No sources → no Services tab.
	mEmpty := NewModel(newMockClient(), time.Second, config.DefaultHistoryRanges, DefaultCaptureSettings(), false)
	for _, v := range mEmpty.visibleTabs {
		if v == viewServices {
			t.Fatal("Services tab should be hidden when no custom sources configured")
		}
	}

	// With sources → Services tab present, and last (so 1-8 stay fixed).
	m := NewModel(mockClientWithSources(), time.Second, config.DefaultHistoryRanges, DefaultCaptureSettings(), false)
	if m.visibleTabs[len(m.visibleTabs)-1] != viewServices {
		t.Fatalf("Services tab should be last, got %v", m.visibleTabs)
	}
	if len(m.customSources) != 2 {
		t.Fatalf("expected 2 custom sources, got %d", len(m.customSources))
	}
}

func TestRenderCustomView(t *testing.T) {
	mc := mockClientWithSources()
	out := renderCustomView(mc.customSources, mc.customMetrics, mc.customStatus, 120, "", 0, 0)

	// Active source (homeassistant) status + metrics render; the chart is empty.
	for _, want := range []string{"homeassistant", "pihole", "Connection", "connected", "throughput", "automations"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderCustomView missing %q:\n%s", want, out)
		}
	}
	// 1 MiB formatted via the bytes unit.
	if !strings.Contains(out, "1.0M") {
		t.Errorf("throughput not formatted as bytes:\n%s", out)
	}
}

// TestRenderCustomViewLongMetricNoWrap guards against the shared labelStyle's
// Width(14) wrapping a long, unselected metric name (e.g. containers_running)
// across two lines and breaking the metrics-list layout.
func TestRenderCustomViewLongMetricNoWrap(t *testing.T) {
	sources := []api.CustomSourceInfo{{
		Name:    "docker",
		Metrics: []api.CustomFieldInfo{{Name: "containers_running", Unit: "count"}, {Name: "images", Unit: "count"}},
	}}
	metrics := []api.CustomMetric{
		{Source: "docker", Name: "containers_running", Unit: "count", Value: 3},
		{Source: "docker", Name: "images", Unit: "count", Value: 16},
	}
	// Render with the long name unselected (cursor on "images") — the case that
	// previously wrapped "containers_running" into "containers_run" / "ning".
	out := renderCustomView(sources, metrics, nil, 190, "", 0, 1)
	if !strings.Contains(out, "containers_running") {
		t.Errorf("long metric name wrapped (not intact on one line):\n%s", out)
	}
}

func TestSelectedCustomSeries(t *testing.T) {
	m := NewModel(mockClientWithSources(), time.Second, config.DefaultHistoryRanges, DefaultCaptureSettings(), false)
	m.current = viewServices
	// Default: section 0 (homeassistant), metric cursor 0 (throughput).
	src, metric, unit := m.selectedCustomSeries()
	if src != "homeassistant" || metric != "throughput" || unit != "bytes" {
		t.Fatalf("selectedCustomSeries = %q/%q/%q", src, metric, unit)
	}
	if !m.hasHistory() {
		t.Error("Services with a metric should report hasHistory")
	}

	// Move metric cursor to automations.
	m.servicesMetricCursor = 1
	_, metric, _ = m.selectedCustomSeries()
	if metric != "automations" {
		t.Errorf("metric after cursor move = %q, want automations", metric)
	}

	// Switch to pihole sub-section.
	m.servicesSection = 1
	m.servicesMetricCursor = 0
	src, metric, unit = m.selectedCustomSeries()
	if src != "pihole" || metric != "blocked" || unit != "count" {
		t.Errorf("pihole series = %q/%q/%q", src, metric, unit)
	}

	// A status-only source has no chart, so no history controls.
	m.customSources = []api.CustomSourceInfo{{Name: "status-only", Status: []api.CustomFieldInfo{{Name: "S"}}}}
	m.servicesSection = 0
	if m.hasHistory() {
		t.Error("status-only Services source should not report hasHistory")
	}
}

// TestServicesChartReselectFromCache guards the bug where selecting a Services
// metric blanked the chart: a 304 (no body) on reselection left the just-cleared
// chart slot empty. The series is now cached per source+metric and re-rendered on
// selection, and a 304 keeps it.
func TestServicesChartReselectFromCache(t *testing.T) {
	m := NewModel(mockClientWithSources(), time.Second, config.DefaultHistoryRanges, DefaultCaptureSettings(), false)
	m.current = viewServices
	m.width, m.height, m.ready = 120, 40, true

	now := time.Now()
	series := func(v float64) []api.TimeSeries {
		return []api.TimeSeries{{Label: "v", Points: []api.TimeSeriesPoint{
			{TimestampNS: now.Add(-time.Minute).UnixNano(), Value: v},
			{TimestampNS: now.UnixNano(), Value: v + 1},
		}}}
	}

	// Full fetch for throughput (cursor 0) → chart rendered + cached.
	mAny, _ := m.Update(historyResultMsg{
		forView: viewServices, source: "homeassistant", metric: "throughput",
		series: series(10), start: now.Add(-time.Hour), end: now,
	})
	m = mAny.(Model)
	if m.cachedHistoryCharts[viewServices] == "" {
		t.Fatal("chart empty after full fetch")
	}
	if _, ok := m.servicesHist[servicesHistKey("homeassistant", "throughput")]; !ok {
		t.Fatal("throughput not cached in servicesHist")
	}

	// Select automations (uncached) → chart blanks pending fetch.
	m.servicesMetricCursor = 1
	m.showSelectedCustomChart()
	if m.cachedHistoryCharts[viewServices] != "" {
		t.Fatal("chart should blank when selecting an uncached metric")
	}

	// Reselect throughput → chart re-renders instantly from cache (the regression).
	m.servicesMetricCursor = 0
	m.showSelectedCustomChart()
	if m.cachedHistoryCharts[viewServices] == "" {
		t.Fatal("chart should re-render from cache on reselect, not stay blank")
	}

	// A 304 for the selected metric must not blank the chart.
	before := m.cachedHistoryCharts[viewServices]
	mAny, _ = m.Update(historyResultMsg{
		forView: viewServices, source: "homeassistant", metric: "throughput",
		err: ErrNotModified, start: now.Add(-time.Hour), end: now,
	})
	m = mAny.(Model)
	if m.cachedHistoryCharts[viewServices] != before {
		t.Fatal("304 should leave the chart intact")
	}
}

func TestFormatCustomValue(t *testing.T) {
	tests := []struct {
		v    float64
		unit string
		want string
	}{
		{1048576, "bytes", "1.0M"},
		{2_000_000, "bits", "2.0Mb"},
		{42.5, "percent", "42.5%"},
		{7, "count", "7"},
		{90, "duration", "1m30s"},
		{3.14, "raw", "3.14"},
	}
	for _, tt := range tests {
		if got := formatCustomValue(tt.v, tt.unit); got != tt.want {
			t.Errorf("formatCustomValue(%v, %q) = %q, want %q", tt.v, tt.unit, got, tt.want)
		}
	}
}
