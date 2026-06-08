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
			Name:    "qbittorrent",
			Metrics: []api.CustomFieldInfo{{Name: "dl_speed", Unit: "bytes"}, {Name: "up_speed", Unit: "bytes"}},
			Status:  []api.CustomFieldInfo{{Name: "Connection"}},
		},
		{
			Name:    "plex",
			Metrics: []api.CustomFieldInfo{{Name: "streams", Unit: "count"}},
		},
	}
	mc.customMetrics = []api.CustomMetric{
		{Source: "qbittorrent", Name: "dl_speed", Unit: "bytes", Value: 1048576},
		{Source: "qbittorrent", Name: "up_speed", Unit: "bytes", Value: 524288},
		{Source: "plex", Name: "streams", Unit: "count", Value: 3},
	}
	mc.customStatus = []api.CustomStatus{
		{Source: "qbittorrent", Label: "Connection", Value: "connected", Badge: "ok"},
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

	// Active source (qbittorrent) status + metrics render; the chart is empty.
	for _, want := range []string{"qbittorrent", "plex", "Connection", "connected", "dl_speed", "up_speed"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderCustomView missing %q:\n%s", want, out)
		}
	}
	// 1 MiB formatted via the bytes unit.
	if !strings.Contains(out, "1.0M") {
		t.Errorf("dl_speed not formatted as bytes:\n%s", out)
	}
}

func TestSelectedCustomSeries(t *testing.T) {
	m := NewModel(mockClientWithSources(), time.Second, config.DefaultHistoryRanges, DefaultCaptureSettings(), false)
	m.current = viewServices
	// Default: section 0 (qbittorrent), metric cursor 0 (dl_speed).
	src, metric, unit := m.selectedCustomSeries()
	if src != "qbittorrent" || metric != "dl_speed" || unit != "bytes" {
		t.Fatalf("selectedCustomSeries = %q/%q/%q", src, metric, unit)
	}
	if !m.hasHistory() {
		t.Error("Services with a metric should report hasHistory")
	}

	// Move metric cursor to up_speed.
	m.servicesMetricCursor = 1
	_, metric, _ = m.selectedCustomSeries()
	if metric != "up_speed" {
		t.Errorf("metric after cursor move = %q, want up_speed", metric)
	}

	// Switch to plex sub-section.
	m.servicesSection = 1
	m.servicesMetricCursor = 0
	src, metric, unit = m.selectedCustomSeries()
	if src != "plex" || metric != "streams" || unit != "count" {
		t.Errorf("plex series = %q/%q/%q", src, metric, unit)
	}

	// A status-only source has no chart, so no history controls.
	m.customSources = []api.CustomSourceInfo{{Name: "status-only", Status: []api.CustomFieldInfo{{Name: "S"}}}}
	m.servicesSection = 0
	if m.hasHistory() {
		t.Error("status-only Services source should not report hasHistory")
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
