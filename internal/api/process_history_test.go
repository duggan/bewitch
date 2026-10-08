package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fetchProcessHistory(t *testing.T, s *Server, start, end time.Time, names string) []TimeSeries {
	t.Helper()
	url := fmt.Sprintf("/api/history/process?start=%d&end=%d", start.Unix(), end.Unix())
	if names != "" {
		url += "&names=" + names
	}
	rec := httptest.NewRecorder()
	s.handleHistoryProcess(rec, httptest.NewRequest("GET", url, nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp HistoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Series
}

func seriesByLabel(series []TimeSeries) map[string]TimeSeries {
	m := map[string]TimeSeries{}
	for _, s := range series {
		m[s.Label] = s
	}
	return m
}

// seedProcesses writes one hour of samples (two per minute) starting at t0:
//   - "steady" (pid 100) at 40% in every sample
//   - "worker" as two pids (300, 301) at 15% each in every sample → 30% combined
//   - 20 one-off "burst-N" pids at 100% in a single sample each
//
// plus a reused-PID trap: process_info also has (pid 100, start_time 2,
// "imposter", seen later), which a pid-only name lookup would pick.
func seedProcesses(t *testing.T, d *sql.DB, t0 time.Time) {
	t.Helper()
	info := func(pid, start int, name string, seen time.Time) {
		exec(t, d, `INSERT INTO process_info (pid, start_time, name, first_seen) VALUES (?, ?, ?, ?)`, pid, start, name, seen)
	}
	info(100, 1, "steady", t0)
	info(100, 2, "imposter", t0.Add(30*time.Minute))
	info(300, 1, "worker", t0)
	info(301, 1, "worker", t0)
	for i := 0; i < 20; i++ {
		info(200+i, 1, fmt.Sprintf("burst-%d", i), t0)
	}
	metric := func(ts time.Time, pid int, user, sys float64) {
		exec(t, d, `INSERT INTO process_metrics (ts, pid, start_time, cpu_user_pct, cpu_system_pct) VALUES (?, ?, 1, ?, ?)`, ts, pid, user, sys)
	}
	for i := 0; i < 120; i++ {
		ts := t0.Add(time.Duration(i) * 30 * time.Second)
		metric(ts, 100, 30, 10)
		metric(ts, 300, 10, 5)
		metric(ts, 301, 10, 5)
		if i < 20 {
			metric(ts, 200+i, 90, 10)
		}
	}
}

func TestProcessHistoryRanking(t *testing.T) {
	s, d, _ := newArchiveSchemaServer(t) // no Parquet files → DuckDB-only path
	t0 := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Hour)
	seedProcesses(t, d, t0)
	end := t0.Add(time.Hour - time.Second)

	series := fetchProcessHistory(t, s, t0, end, "")
	if len(series) != 10 {
		t.Fatalf("got %d series, want 10", len(series))
	}
	// The handler orders series by total CPU: steady (40%×60) then worker (30%×60);
	// each burst contributes a single 50% bucket point (100% in one of two samples).
	if series[0].Label != "steady" || series[1].Label != "worker" {
		t.Errorf("top two = %q, %q; want steady, worker (one-off bursts must not outrank them)", series[0].Label, series[1].Label)
	}
	by := seriesByLabel(series)
	if _, ok := by["imposter"]; ok {
		t.Error(`pid 100 named "imposter": names must resolve on (pid, start_time)`)
	}
	w := by["worker"]
	if len(w.Points) != 60 {
		t.Errorf("worker has %d points, want 60 (one per 1-minute bucket, both pids summed)", len(w.Points))
	}
	for _, p := range w.Points {
		if math.Abs(p.Value-30) > 1e-9 {
			t.Fatalf("worker bucket value %v, want 30 (15%% + 15%%)", p.Value)
		}
	}
	if b, ok := by["burst-0"]; ok {
		if len(b.Points) != 1 || math.Abs(b.Points[0].Value-50) > 1e-9 {
			t.Errorf("burst-0 = %+v, want one point of 50 (absent sample counts as 0)", b.Points)
		}
	}

	t.Run("names filter", func(t *testing.T) {
		got := fetchProcessHistory(t, s, t0, end, "worker,nonexistent")
		if len(got) != 1 || got[0].Label != "worker" || len(got[0].Points) != 60 {
			t.Errorf("filtered series = %+v", got)
		}
	})
}

func TestProcessHistoryBothSourcesNoDoubleCount(t *testing.T) {
	s, d, archive := newArchiveSchemaServer(t)
	// Archived day (older than the 24h threshold) for steady; process_info lives
	// both in the live table and the archived snapshot, as it does in practice.
	old := time.Now().UTC().AddDate(0, 0, -3).Truncate(time.Hour)
	exec(t, d, `INSERT INTO process_info (pid, start_time, name, first_seen) VALUES (100, 1, 'steady', ?)`, old)
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	exec(t, d, fmt.Sprintf(`COPY process_info TO '%s' (FORMAT parquet)`, filepath.Join(archive, "process_info.parquet")))
	archiveRows(t, d, "process_metrics", filepath.Join(archive, "process_metrics", old.Format("2006-01-02")+".parquet"), "",
		`INSERT INTO process_metrics (ts, pid, start_time, cpu_user_pct, cpu_system_pct) VALUES (?, 100, 1, 30, 10)`, old)
	// And a live sample within the threshold.
	exec(t, d, `INSERT INTO process_metrics (ts, pid, start_time, cpu_user_pct, cpu_system_pct) VALUES (?, 100, 1, 30, 10)`, time.Now().UTC().Add(-time.Hour))

	series := fetchProcessHistory(t, s, old.Add(-time.Hour), time.Now(), "")
	by := seriesByLabel(series)
	st, ok := by["steady"]
	if !ok || len(st.Points) != 2 {
		t.Fatalf("steady = %+v, want 2 points (one archived, one live)", st)
	}
	for _, p := range st.Points {
		if math.Abs(p.Value-40) > 1e-9 {
			t.Errorf("steady point %v, want 40 (not double-counted via duplicate process_info)", p.Value)
		}
	}
}

// TestHistoryWindowNonUTC guards against the history window shifting by the
// host's UTC offset. ts is a naive TIMESTAMP holding UTC; the handlers used to
// compare it against to_timestamp(?) (a TIMESTAMPTZ), which makes DuckDB read
// ts in the session's local zone. On a UTC+1 host "last hour" missed the last
// hour entirely (memory returned 0 points while cpu — which bound Go times —
// fine). CI runs in UTC, so force zones on both sides of UTC here.
func TestHistoryWindowNonUTC(t *testing.T) {
	for _, tz := range []string{"Europe/Dublin", "America/Los_Angeles", "Asia/Tokyo"} {
		t.Run(tz, func(t *testing.T) {
			s, d, _ := newArchiveSchemaServer(t)
			exec(t, d, "SET GLOBAL TimeZone = '"+tz+"'")
			now := time.Now().UTC()
			exec(t, d, `INSERT INTO memory_metrics (ts, total_bytes, used_bytes) VALUES (?, 100, 50)`, now.Add(-10*time.Minute))
			exec(t, d, `INSERT INTO memory_metrics (ts, total_bytes, used_bytes) VALUES (?, 100, 50)`, now.Add(-3*time.Hour))
			exec(t, d, `INSERT INTO process_info (pid, start_time, name, first_seen) VALUES (1, 1, 'p', ?)`, now)
			exec(t, d, `INSERT INTO process_metrics (ts, pid, start_time, cpu_user_pct, cpu_system_pct) VALUES (?, 1, 1, 5, 5)`, now.Add(-10*time.Minute))

			rec := httptest.NewRecorder()
			s.handleHistoryMemory(rec, httptest.NewRequest("GET",
				fmt.Sprintf("/api/history/memory?start=%d&end=%d", now.Add(-time.Hour).Unix(), now.Unix()), nil))
			var resp HistoryResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
			if len(resp.Series) == 0 || len(resp.Series[0].Points) != 1 {
				t.Errorf("memory last hour: %+v, want exactly the -10m sample", resp.Series)
			}
			if got := fetchProcessHistory(t, s, now.Add(-time.Hour), now, ""); len(got) != 1 || len(got[0].Points) != 1 {
				t.Errorf("process last hour: %+v, want one point", got)
			}
		})
	}
}
