package store

import (
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/collector"
)

// TestCPUStealPersisted verifies steal_pct survives the appender write path
// (WriteBatch, the production path) and is readable back — the column added in
// migration 000012 so cpu.steal alerts have data.
func TestCPUStealPersisted(t *testing.T) {
	s := newPruneTestStore(t)

	sample := collector.Sample{
		Timestamp: time.Now(),
		Kind:      "cpu",
		Data: collector.CPUData{Cores: []collector.CPUCoreSample{
			{Core: -1, UserPct: 10, SystemPct: 5, IdlePct: 60, IOWaitPct: 5, StealPct: 20},
		}},
	}
	if err := s.WriteBatch([]collector.Sample{sample}); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}

	var steal float64
	if err := s.db.QueryRow("SELECT steal_pct FROM cpu_metrics WHERE core = -1").Scan(&steal); err != nil {
		t.Fatalf("read steal_pct: %v", err)
	}
	if steal != 20 {
		t.Errorf("steal_pct = %v, want 20", steal)
	}
}

// TestCPUStealColumnNullable confirms a row inserted without steal_pct (the
// pre-migration shape) reads back NULL, so an AVG over a steal-less window yields
// NULL (no fire) rather than 0.
func TestCPUStealColumnNullable(t *testing.T) {
	s := newPruneTestStore(t)
	if _, err := s.db.Exec(
		"INSERT INTO cpu_metrics (ts, core, user_pct, system_pct, idle_pct, iowait_pct) VALUES (?, -1, 10, 5, 80, 5)",
		time.Now()); err != nil {
		t.Fatalf("insert legacy-shape row: %v", err)
	}
	var avg *float64
	if err := s.db.QueryRow("SELECT AVG(steal_pct) FROM cpu_metrics WHERE core = -1").Scan(&avg); err != nil {
		t.Fatalf("avg steal_pct: %v", err)
	}
	if avg != nil {
		t.Errorf("AVG(steal_pct) over a steal-less window = %v, want NULL", *avg)
	}
}
