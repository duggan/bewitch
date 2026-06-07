package store

import (
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/collector"
)

// TestProcessDiskIOPersisted verifies per-process disk I/O rates survive the
// appender write path (WriteBatch) and read back — the columns added in migration
// 000013. Guards the appender column count matching the post-migration schema.
func TestProcessDiskIOPersisted(t *testing.T) {
	s := newPruneTestStore(t)

	sample := collector.Sample{
		Timestamp: time.Now(),
		Kind:      "process",
		Data: collector.ProcessData{Processes: []collector.ProcessSample{
			{PID: 4242, StartTime: 1000, Name: "writer", State: "R",
				CPUUserPct: 1, RSSBytes: 2048, NumFDs: 3, NumThreads: 2,
				ReadBytesSec: 1048576, WriteBytesSec: 524288},
		}},
	}
	if err := s.WriteBatch([]collector.Sample{sample}); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}

	var read, write float64
	if err := s.db.QueryRow(
		"SELECT read_bytes_sec, write_bytes_sec FROM process_metrics WHERE pid = 4242").Scan(&read, &write); err != nil {
		t.Fatalf("read disk I/O: %v", err)
	}
	if read != 1048576 || write != 524288 {
		t.Errorf("disk I/O = %v/%v, want 1048576/524288", read, write)
	}
}

// TestProcessDiskIONullable confirms a legacy-shape row (no I/O columns) reads back
// NULL rather than 0 — so historical rows aren't misread as "zero I/O".
func TestProcessDiskIONullable(t *testing.T) {
	s := newPruneTestStore(t)
	if _, err := s.db.Exec(
		`INSERT INTO process_metrics (ts, pid, start_time, state, cpu_user_pct, cpu_system_pct, rss_bytes, num_fds, num_threads)
		 VALUES (?, 1, 1000, 'S', 0, 0, 1024, 1, 1)`, time.Now()); err != nil {
		t.Fatalf("insert legacy-shape row: %v", err)
	}
	var read *float64
	if err := s.db.QueryRow("SELECT read_bytes_sec FROM process_metrics WHERE pid = 1").Scan(&read); err != nil {
		t.Fatalf("read: %v", err)
	}
	if read != nil {
		t.Errorf("read_bytes_sec = %v, want NULL for a legacy row", *read)
	}
}
