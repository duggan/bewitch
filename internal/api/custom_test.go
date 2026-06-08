package api

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/db"
)

func TestHandleMetricsCustomCacheAndETag(t *testing.T) {
	s := &Server{}

	// Cold: empty arrays, no panic.
	rec := httptest.NewRecorder()
	s.handleMetricsCustom(rec, httptest.NewRequest("GET", "/api/metrics/custom", nil))
	if rec.Code != 200 {
		t.Fatalf("cold GET: %d", rec.Code)
	}
	var empty CustomResponse
	json.Unmarshal(rec.Body.Bytes(), &empty)
	if len(empty.Metrics) != 0 || len(empty.Status) != 0 {
		t.Errorf("cold response not empty: %+v", empty)
	}

	// Push two sources; getCachedCustom flattens them sorted by source.
	s.SetCustomSnapshot("plex", []CustomMetric{{Source: "plex", Name: "streams", Value: 2}}, nil)
	s.SetCustomSnapshot("qb", []CustomMetric{{Source: "qb", Name: "dl", Unit: "bytes", Value: 1024}},
		[]CustomStatus{{Source: "qb", Label: "Connection", Value: "connected", Badge: "ok"}})

	rec = httptest.NewRecorder()
	s.handleMetricsCustom(rec, httptest.NewRequest("GET", "/api/metrics/custom", nil))
	if rec.Code != 200 {
		t.Fatalf("warm GET: %d", rec.Code)
	}
	var resp CustomResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Metrics) != 2 {
		t.Fatalf("got %d metrics, want 2 (merged across sources): %+v", len(resp.Metrics), resp.Metrics)
	}
	// Sorted by source: plex before qb.
	if resp.Metrics[0].Source != "plex" || resp.Metrics[1].Source != "qb" {
		t.Errorf("metrics not source-sorted: %+v", resp.Metrics)
	}
	if len(resp.Status) != 1 || resp.Status[0].Badge != "ok" {
		t.Errorf("status = %+v", resp.Status)
	}

	// ETag 304 round-trip.
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	rec2 := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/metrics/custom", nil)
	req.Header.Set("If-None-Match", etag)
	s.handleMetricsCustom(rec2, req)
	if rec2.Code != 304 {
		t.Errorf("If-None-Match should give 304, got %d", rec2.Code)
	}
}

func TestHandleCustomCatalog(t *testing.T) {
	s := &Server{}
	// Cold: empty.
	rec := httptest.NewRecorder()
	s.handleCustomCatalog(rec, httptest.NewRequest("GET", "/api/custom/sources", nil))
	var cold CustomCatalogResponse
	json.Unmarshal(rec.Body.Bytes(), &cold)
	if len(cold.Sources) != 0 {
		t.Errorf("cold catalog not empty: %+v", cold)
	}

	s.SetCustomCatalog([]CustomSourceInfo{{
		Name:    "qb",
		Metrics: []CustomFieldInfo{{Name: "dl", Unit: "bytes"}},
		Status:  []CustomFieldInfo{{Name: "Connection"}},
	}})
	rec = httptest.NewRecorder()
	s.handleCustomCatalog(rec, httptest.NewRequest("GET", "/api/custom/sources", nil))
	var got CustomCatalogResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != 1 || got.Sources[0].Name != "qb" || got.Sources[0].Metrics[0].Unit != "bytes" {
		t.Errorf("catalog = %+v", got)
	}
}

func TestPrometheusIncludesCustom(t *testing.T) {
	s := &Server{}
	s.SetCustomSnapshot("qb",
		[]CustomMetric{{Source: "qb", Name: "dl", Unit: "bytes", Value: 1024}},
		[]CustomStatus{{Source: "qb", Label: "Connection", Value: "connected"}})

	rec := httptest.NewRecorder()
	s.handlePrometheus(rec, httptest.NewRequest("GET", "/metrics", nil))
	out := rec.Body.String()
	if !strings.Contains(out, `bewitch_custom_value{source="qb",metric="dl"} 1024`) {
		t.Errorf("missing custom gauge in /metrics:\n%s", out)
	}
	// Status strings must NOT be exported (unbounded cardinality).
	if strings.Contains(out, "connected") {
		t.Errorf("status value leaked into /metrics")
	}
}

func TestHandleHistoryCustom(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "custom.duckdb"), "", "")
	if err != nil {
		t.Fatalf("open migrated db: %v", err)
	}
	defer database.Close()
	s := &Server{dbFn: func() *sql.DB { return database }, historyCache: map[string]*historyCacheEntry{}}

	now := time.Now()
	for i := 0; i < 5; i++ {
		if _, err := database.Exec(
			`INSERT INTO custom_metrics (ts, source, metric, value) VALUES (?, 'qb', 'dl', ?)`,
			now.Add(-time.Duration(i)*time.Minute), float64(100+i)); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// A different metric that must NOT appear in the qb/dl query.
	database.Exec(`INSERT INTO custom_metrics (ts, source, metric, value) VALUES (?, 'qb', 'up', 999)`, now)

	start := strconv.FormatInt(now.Add(-time.Hour).Unix(), 10)
	end := strconv.FormatInt(now.Add(time.Minute).Unix(), 10)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET",
		"/api/history/custom?source=qb&metric=dl&start="+start+"&end="+end, nil)
	s.handleHistoryCustom(rec, req)
	if rec.Code != 200 {
		t.Fatalf("history: %d %s", rec.Code, rec.Body.String())
	}
	var resp HistoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Series) != 1 || resp.Series[0].Label != "dl" {
		t.Fatalf("series = %+v", resp.Series)
	}
	if len(resp.Series[0].Points) == 0 {
		t.Fatal("no points returned")
	}
	for _, p := range resp.Series[0].Points {
		if p.Value >= 999 {
			t.Errorf("query leaked the 'up' metric: %v", p.Value)
		}
	}

	// Missing params → 400.
	rec = httptest.NewRecorder()
	s.handleHistoryCustom(rec, httptest.NewRequest("GET", "/api/history/custom", nil))
	if rec.Code != 400 {
		t.Errorf("missing params should be 400, got %d", rec.Code)
	}
}
