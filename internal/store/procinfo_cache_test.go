package store

import (
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/collector"
)

// TestEvictStaleProcInfoCache verifies the cache is bounded by recency even when
// retention/compaction (the only other paths that rebuild it) are never run. This
// is the always-on safeguard against the unbounded growth that drove bewitchd's
// RSS leak on memory-constrained hosts.
func TestEvictStaleProcInfoCache(t *testing.T) {
	s := newPruneTestStore(t)

	now := time.Now()
	// Two stale entries (last seen long ago) and two fresh ones.
	s.procInfoCacheMu.Lock()
	s.procInfoCache[processKey{pid: 1, startTime: 1000}] = now.Add(-30 * time.Minute)
	s.procInfoCache[processKey{pid: 2, startTime: 2000}] = now.Add(-20 * time.Minute)
	s.procInfoCache[processKey{pid: 3, startTime: 3000}] = now.Add(-1 * time.Minute)
	s.procInfoCache[processKey{pid: 4, startTime: 4000}] = now
	s.procInfoCacheMu.Unlock()

	if got := s.ProcInfoCacheLen(); got != 4 {
		t.Fatalf("precondition: cache size = %d, want 4", got)
	}

	evicted := s.EvictStaleProcInfoCache(15 * time.Minute)
	if evicted != 2 {
		t.Errorf("evicted = %d, want 2", evicted)
	}
	if got := s.ProcInfoCacheLen(); got != 2 {
		t.Errorf("cache size after eviction = %d, want 2", got)
	}
	for _, pid := range []int32{1, 2} {
		if _, ok := s.procInfoCache[processKey{pid: pid, startTime: int64(pid) * 1000}]; ok {
			t.Errorf("stale pid %d should have been evicted", pid)
		}
	}
	for _, pid := range []int32{3, 4} {
		if _, ok := s.procInfoCache[processKey{pid: pid, startTime: int64(pid) * 1000}]; !ok {
			t.Errorf("fresh pid %d should have survived eviction", pid)
		}
	}
}

// TestPrepareProcessInfoRefreshesLastSeen verifies that processes still present
// in a later cycle have their last-seen timestamp refreshed, so a long-running
// process is never evicted while alive.
func TestPrepareProcessInfoRefreshesLastSeen(t *testing.T) {
	s := newPruneTestStore(t)

	t0 := time.Now().Add(-1 * time.Hour)
	sample := collector.Sample{
		Timestamp: t0,
		Kind:      "process",
		Data: collector.ProcessData{
			Processes: []collector.ProcessSample{
				{PID: 100, StartTime: 5000, Name: "long-runner"},
			},
		},
	}
	s.prepareProcessInfo(sample, sample.Data.(collector.ProcessData))

	key := processKey{pid: 100, startTime: 5000}
	s.procInfoCacheMu.RLock()
	first := s.procInfoCache[key]
	s.procInfoCacheMu.RUnlock()
	if !first.Equal(t0) {
		t.Fatalf("initial last-seen = %v, want %v", first, t0)
	}

	// Same process seen again later — should refresh, not duplicate.
	t1 := time.Now()
	sample.Timestamp = t1
	s.prepareProcessInfo(sample, sample.Data.(collector.ProcessData))

	if got := s.ProcInfoCacheLen(); got != 1 {
		t.Errorf("cache size = %d, want 1 (no duplicate entry)", got)
	}
	s.procInfoCacheMu.RLock()
	second := s.procInfoCache[key]
	s.procInfoCacheMu.RUnlock()
	if !second.Equal(t1) {
		t.Errorf("last-seen after second cycle = %v, want refreshed to %v", second, t1)
	}

	// A subsequent eviction with a short window must NOT remove the live process,
	// because its last-seen was refreshed.
	if evicted := s.EvictStaleProcInfoCache(1 * time.Minute); evicted != 0 {
		t.Errorf("evicted %d live processes, want 0", evicted)
	}
}
