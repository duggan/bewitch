package api

import (
	"math"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBucketInterval(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{"30 minutes", 30 * time.Minute, "1 minute"},
		{"exactly 1 hour", time.Hour, "1 minute"},
		{"6 hours", 6 * time.Hour, "10 minutes"},
		{"exactly 24 hours", 24 * time.Hour, "10 minutes"},
		{"3 days", 3 * 24 * time.Hour, "1 hour"},
		{"exactly 7 days", 7 * 24 * time.Hour, "1 hour"},
		{"30 days", 30 * 24 * time.Hour, "6 hours"},
		{"1 year", 365 * 24 * time.Hour, "6 hours"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := now.Add(-tt.duration)
			got := bucketInterval(start, now)
			if got != tt.want {
				t.Errorf("bucketInterval(%v range) = %q, want %q", tt.duration, got, tt.want)
			}
		})
	}
}

func TestParseTimeRange(t *testing.T) {
	t.Run("no params defaults to 7 days", func(t *testing.T) {
		r, _ := http.NewRequest("GET", "/api/history/cpu", nil)
		start, end := parseTimeRange(r)
		// end should be close to now
		if time.Since(end) > time.Second {
			t.Error("end should be close to now")
		}
		// start should be ~7 days before end
		diff := end.Sub(start)
		expected := 7 * 24 * time.Hour
		if math.Abs(diff.Seconds()-expected.Seconds()) > 1 {
			t.Errorf("range = %v, want ~%v", diff, expected)
		}
	})

	t.Run("with start and end params", func(t *testing.T) {
		r, _ := http.NewRequest("GET", "/api/history/cpu?start=1700000000&end=1700003600", nil)
		start, end := parseTimeRange(r)
		if start.Unix() != 1700000000 {
			t.Errorf("start = %d, want 1700000000", start.Unix())
		}
		if end.Unix() != 1700003600 {
			t.Errorf("end = %d, want 1700003600", end.Unix())
		}
	})

	t.Run("invalid params use defaults", func(t *testing.T) {
		r, _ := http.NewRequest("GET", "/api/history/cpu?start=bad&end=invalid", nil)
		start, end := parseTimeRange(r)
		// Should fall back to defaults
		if time.Since(end) > time.Second {
			t.Error("end should be close to now")
		}
		diff := end.Sub(start)
		expected := 7 * 24 * time.Hour
		if math.Abs(diff.Seconds()-expected.Seconds()) > 1 {
			t.Errorf("range = %v, want ~%v", diff, expected)
		}
	})
}

func TestHistoryCacheKey(t *testing.T) {
	t.Run("no params returns path only", func(t *testing.T) {
		r, _ := http.NewRequest("GET", "/api/history/cpu", nil)
		key := historyCacheKey(r)
		if key != "/api/history/cpu" {
			t.Errorf("key = %q, want /api/history/cpu", key)
		}
	})

	t.Run("with start and end quantized", func(t *testing.T) {
		r1, _ := http.NewRequest("GET", "/api/history/cpu?start=1700000000&end=1700003600", nil)
		r2, _ := http.NewRequest("GET", "/api/history/cpu?start=1700000002&end=1700003602", nil)
		key1 := historyCacheKey(r1)
		key2 := historyCacheKey(r2)
		// Timestamps within 5s TTL should produce the same quantized key
		if key1 != key2 {
			t.Errorf("keys should match for timestamps within TTL: %q vs %q", key1, key2)
		}
	})

	t.Run("timestamps far apart produce different keys", func(t *testing.T) {
		r1, _ := http.NewRequest("GET", "/api/history/cpu?start=1700000000&end=1700003600", nil)
		r2, _ := http.NewRequest("GET", "/api/history/cpu?start=1700000010&end=1700003610", nil)
		key1 := historyCacheKey(r1)
		key2 := historyCacheKey(r2)
		if key1 == key2 {
			t.Error("keys should differ for timestamps 10s apart")
		}
	})

	t.Run("names param included in key", func(t *testing.T) {
		r1, _ := http.NewRequest("GET", "/api/history/process?start=1700000000&end=1700003600&names=nginx,redis", nil)
		r2, _ := http.NewRequest("GET", "/api/history/process?start=1700000000&end=1700003600", nil)
		key1 := historyCacheKey(r1)
		key2 := historyCacheKey(r2)
		if key1 == key2 {
			t.Error("keys should differ when names param present")
		}
	})
}

func TestGetQuerySource(t *testing.T) {
	// Create a temp archive dir with a dummy Parquet file so
	// hasAnyParquetFiles returns true for routing tests.
	archiveDir := t.TempDir()
	tableDir := filepath.Join(archiveDir, "cpu_metrics")
	os.MkdirAll(tableDir, 0755)
	os.WriteFile(filepath.Join(tableDir, "2024-01-01.parquet"), []byte{}, 0644)

	t.Run("no archive config returns DuckDB", func(t *testing.T) {
		s := &Server{}
		got := s.getQuerySource(time.Now().Add(-time.Hour), time.Now())
		if got != querySourceDuckDB {
			t.Errorf("got %d, want querySourceDuckDB", got)
		}
	})

	t.Run("no parquet files returns DuckDB", func(t *testing.T) {
		emptyDir := t.TempDir()
		s := &Server{
			archivePath:      emptyDir,
			archiveThreshold: 24 * time.Hour,
		}
		got := s.getQuerySource(
			time.Now().Add(-2*24*time.Hour),
			time.Now(),
		)
		if got != querySourceDuckDB {
			t.Errorf("got %d, want querySourceDuckDB", got)
		}
	})

	t.Run("start after cutoff returns DuckDB", func(t *testing.T) {
		s := &Server{
			archivePath:      archiveDir,
			archiveThreshold: 7 * 24 * time.Hour,
		}
		// Query last hour — well within the 7-day threshold
		got := s.getQuerySource(time.Now().Add(-time.Hour), time.Now())
		if got != querySourceDuckDB {
			t.Errorf("got %d, want querySourceDuckDB", got)
		}
	})

	t.Run("end before cutoff returns Parquet", func(t *testing.T) {
		s := &Server{
			archivePath:      archiveDir,
			archiveThreshold: 24 * time.Hour,
		}
		// Query 3 days ago to 2 days ago — all before the 1-day cutoff
		got := s.getQuerySource(
			time.Now().Add(-3*24*time.Hour),
			time.Now().Add(-2*24*time.Hour),
		)
		if got != querySourceParquet {
			t.Errorf("got %d, want querySourceParquet", got)
		}
	})

	t.Run("spanning cutoff returns Both", func(t *testing.T) {
		s := &Server{
			archivePath:      archiveDir,
			archiveThreshold: 24 * time.Hour,
		}
		// Query 2 days ago to now — spans the 1-day cutoff
		got := s.getQuerySource(
			time.Now().Add(-2*24*time.Hour),
			time.Now(),
		)
		if got != querySourceBoth {
			t.Errorf("got %d, want querySourceBoth", got)
		}
	})
}
