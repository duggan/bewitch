package api

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"
)

func TestIsJSONBody(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{"no header", "", false},
		{"json", "application/json", true},
		{"json with charset", "application/json; charset=utf-8", true},
		{"plain text", "text/plain", false},
		{"form data", "application/x-www-form-urlencoded", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest("POST", "/", nil)
			if tt.contentType != "" {
				r.Header.Set("Content-Type", tt.contentType)
			}
			if got := isJSONBody(r); got != tt.want {
				t.Errorf("isJSONBody() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCPUResponseRoundTrip(t *testing.T) {
	orig := CPUResponse{
		Cores: []CPUCoreMetric{
			{Core: -1, UserPct: 25.5, SystemPct: 10.3, IdlePct: 60.2, IOWaitPct: 4.0},
			{Core: 0, UserPct: 30.0, SystemPct: 5.0, IdlePct: 65.0, IOWaitPct: 0.0},
			{Core: 1, UserPct: 20.0, SystemPct: 15.0, IdlePct: 55.0, IOWaitPct: 10.0},
		},
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded CPUResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.Cores) != len(orig.Cores) {
		t.Fatalf("len(Cores) = %d, want %d", len(decoded.Cores), len(orig.Cores))
	}
	for i, want := range orig.Cores {
		got := decoded.Cores[i]
		if got.Core != want.Core {
			t.Errorf("[%d] Core = %d, want %d", i, got.Core, want.Core)
		}
		if math.Abs(got.UserPct-want.UserPct) > 0.01 {
			t.Errorf("[%d] UserPct = %f, want %f", i, got.UserPct, want.UserPct)
		}
		if math.Abs(got.SystemPct-want.SystemPct) > 0.01 {
			t.Errorf("[%d] SystemPct = %f, want %f", i, got.SystemPct, want.SystemPct)
		}
		if math.Abs(got.IdlePct-want.IdlePct) > 0.01 {
			t.Errorf("[%d] IdlePct = %f, want %f", i, got.IdlePct, want.IdlePct)
		}
		if math.Abs(got.IOWaitPct-want.IOWaitPct) > 0.01 {
			t.Errorf("[%d] IOWaitPct = %f, want %f", i, got.IOWaitPct, want.IOWaitPct)
		}
	}
}

func TestMemoryMetricRoundTrip(t *testing.T) {
	orig := MemoryMetric{
		TotalBytes:     16_000_000_000,
		UsedBytes:      8_000_000_000,
		AvailableBytes: 7_000_000_000,
		BuffersBytes:   500_000_000,
		CachedBytes:    3_000_000_000,
		SwapTotalBytes: 4_000_000_000,
		SwapUsedBytes:  100_000_000,
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded MemoryMetric
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.TotalBytes != orig.TotalBytes {
		t.Errorf("TotalBytes = %d, want %d", decoded.TotalBytes, orig.TotalBytes)
	}
	if decoded.UsedBytes != orig.UsedBytes {
		t.Errorf("UsedBytes = %d, want %d", decoded.UsedBytes, orig.UsedBytes)
	}
	if decoded.AvailableBytes != orig.AvailableBytes {
		t.Errorf("AvailableBytes = %d, want %d", decoded.AvailableBytes, orig.AvailableBytes)
	}
	if decoded.SwapTotalBytes != orig.SwapTotalBytes {
		t.Errorf("SwapTotalBytes = %d, want %d", decoded.SwapTotalBytes, orig.SwapTotalBytes)
	}
	if decoded.SwapUsedBytes != orig.SwapUsedBytes {
		t.Errorf("SwapUsedBytes = %d, want %d", decoded.SwapUsedBytes, orig.SwapUsedBytes)
	}
}

func TestHistoryResponseRoundTrip(t *testing.T) {
	orig := HistoryResponse{
		Series: []TimeSeries{
			{
				Label: "cpu_user",
				Points: []TimeSeriesPoint{
					{TimestampNS: 1000000000, Value: 25.5},
					{TimestampNS: 2000000000, Value: 30.0},
				},
			},
			{
				Label:  "cpu_system",
				Points: []TimeSeriesPoint{},
			},
		},
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded HistoryResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.Series) != 2 {
		t.Fatalf("len(Series) = %d, want 2", len(decoded.Series))
	}
	if decoded.Series[0].Label != "cpu_user" {
		t.Errorf("Series[0].Label = %q, want cpu_user", decoded.Series[0].Label)
	}
	if len(decoded.Series[0].Points) != 2 {
		t.Fatalf("len(Series[0].Points) = %d, want 2", len(decoded.Series[0].Points))
	}
	if decoded.Series[0].Points[0].TimestampNS != 1000000000 {
		t.Errorf("Points[0].TimestampNS = %d, want 1000000000", decoded.Series[0].Points[0].TimestampNS)
	}
	if math.Abs(decoded.Series[0].Points[0].Value-25.5) > 0.01 {
		t.Errorf("Points[0].Value = %f, want 25.5", decoded.Series[0].Points[0].Value)
	}
}

func TestEmptyCPUResponse(t *testing.T) {
	orig := CPUResponse{Cores: nil}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded CPUResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Cores) != 0 {
		t.Errorf("len(Cores) = %d, want 0", len(decoded.Cores))
	}
}
