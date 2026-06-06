package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/config"
)

// TestHandleStatsIncludesSelf verifies the daemon self-health block is composed
// into /api/stats when a provider is registered, and omitted otherwise so older
// clients/daemons tolerate its absence.
func TestHandleStatsIncludesSelf(t *testing.T) {
	get := func(s *Server) StatsResponse {
		t.Helper()
		w := httptest.NewRecorder()
		s.handleStats(w, httptest.NewRequest("GET", "/api/stats", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
		}
		var resp StatsResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp
	}

	t.Run("present when provider set", func(t *testing.T) {
		s := &Server{cfg: &config.Config{}, startTime: time.Now()}
		s.SetSelfStatsFunc(func() SelfStats {
			return SelfStats{
				DroppedWriteBatches:  4,
				ProcInfoCacheEntries: 9,
				WriteQueueCap:        8,
				CollectorFails:       map[string]int{"gpu": 1},
			}
		})
		resp := get(s)
		if resp.Self == nil {
			t.Fatal("resp.Self = nil, want populated")
		}
		if resp.Self.DroppedWriteBatches != 4 {
			t.Errorf("DroppedWriteBatches = %d, want 4", resp.Self.DroppedWriteBatches)
		}
		if resp.Self.ProcInfoCacheEntries != 9 {
			t.Errorf("ProcInfoCacheEntries = %d, want 9", resp.Self.ProcInfoCacheEntries)
		}
		if resp.Self.CollectorFails["gpu"] != 1 {
			t.Errorf("CollectorFails[gpu] = %d, want 1", resp.Self.CollectorFails["gpu"])
		}
	})

	t.Run("omitted when no provider", func(t *testing.T) {
		s := &Server{cfg: &config.Config{}, startTime: time.Now()}
		if resp := get(s); resp.Self != nil {
			t.Errorf("resp.Self = %+v, want nil when no provider registered", resp.Self)
		}
	})
}
