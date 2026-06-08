package store

import (
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/collector"
)

// TestCustomMetricsPersisted verifies custom-source numeric metrics survive the
// appender write path (WriteBatch) and read back — migration 000015. Status
// fields are live-only and must NOT be written.
func TestCustomMetricsPersisted(t *testing.T) {
	s := newPruneTestStore(t)

	ts := time.Now()
	sample := collector.Sample{
		Timestamp: ts,
		Kind:      "custom",
		Data: collector.CustomSourceData{
			Source: "qbittorrent",
			Metrics: []collector.CustomMetricSample{
				{Name: "dl_speed", Unit: "bytes", Value: 1048576},
				{Name: "up_speed", Unit: "bytes", Value: 524288},
			},
			Status: []collector.CustomStatusSample{
				{Label: "Connection", Value: "connected", Badge: "ok"},
			},
		},
	}
	if err := s.WriteBatch([]collector.Sample{sample}); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}

	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM custom_metrics WHERE source = 'qbittorrent'").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("got %d custom_metrics rows, want 2 (status not stored)", n)
	}

	var v float64
	if err := s.db.QueryRow(
		"SELECT value FROM custom_metrics WHERE source = 'qbittorrent' AND metric = 'dl_speed'").Scan(&v); err != nil {
		t.Fatalf("read dl_speed: %v", err)
	}
	if v != 1048576 {
		t.Errorf("dl_speed = %v, want 1048576", v)
	}
}

// TestCustomMetricsEmpty confirms a sample with no numeric metrics writes nothing
// (a status-only poll) rather than erroring.
func TestCustomMetricsEmpty(t *testing.T) {
	s := newPruneTestStore(t)
	sample := collector.Sample{
		Timestamp: time.Now(),
		Kind:      "custom",
		Data: collector.CustomSourceData{
			Source: "statusonly",
			Status: []collector.CustomStatusSample{{Label: "S", Value: "ok"}},
		},
	}
	if err := s.WriteBatch([]collector.Sample{sample}); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	var n int
	s.db.QueryRow("SELECT count(*) FROM custom_metrics").Scan(&n)
	if n != 0 {
		t.Errorf("got %d rows, want 0 for a status-only poll", n)
	}
}
