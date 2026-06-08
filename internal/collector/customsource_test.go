package collector

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/config"
)

func metricByName(d CustomSourceData, name string) (CustomMetricSample, bool) {
	for _, m := range d.Metrics {
		if m.Name == name {
			return m, true
		}
	}
	return CustomMetricSample{}, false
}

func TestCustomSourceCollectExtraction(t *testing.T) {
	const body = `{"dl_info_speed":1048576,"up_info_speed":524288,
		"connection_status":"connected","nested":{"deep":7},
		"list":[{"v":10},{"v":20}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	cfg := config.CustomSourceConfig{
		Name:    "pihole",
		BaseURL: srv.URL,
		Request: config.CustomRequestConfig{Path: "/info"},
		Metrics: []config.CustomMetricSpec{
			{Name: "dl", Path: "dl_info_speed", Unit: "bytes"},
			{Name: "deep", Path: "nested.deep", Unit: "count"},
			{Name: "second", Path: "list.1.v", Unit: "count"},
			{Name: "missing", Path: "does.not.exist", Unit: "count"},
		},
		Status: []config.CustomStatusSpec{
			{Label: "Connection", Path: "connection_status", Badges: map[string]string{"connected": "ok"}},
		},
	}
	c := NewCustomSourceCollector(cfg, 5*time.Second)
	sample, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	data, ok := sample.Data.(CustomSourceData)
	if !ok {
		t.Fatalf("Data is %T, want CustomSourceData", sample.Data)
	}
	if sample.Kind != "custom" || data.Source != "pihole" {
		t.Errorf("kind=%q source=%q", sample.Kind, data.Source)
	}
	if len(data.Metrics) != 3 {
		t.Fatalf("got %d metrics, want 3 (missing path skipped): %+v", len(data.Metrics), data.Metrics)
	}
	if m, _ := metricByName(data, "dl"); m.Value != 1048576 {
		t.Errorf("dl = %v, want 1048576", m.Value)
	}
	if m, _ := metricByName(data, "deep"); m.Value != 7 {
		t.Errorf("deep = %v, want 7", m.Value)
	}
	if m, _ := metricByName(data, "second"); m.Value != 20 {
		t.Errorf("second (array index) = %v, want 20", m.Value)
	}
	if _, ok := metricByName(data, "missing"); ok {
		t.Error("missing path should be skipped")
	}
	if len(data.Status) != 1 || data.Status[0].Value != "connected" || data.Status[0].Badge != "ok" {
		t.Errorf("status = %+v, want connected/ok", data.Status)
	}
}

func TestCustomSourceCollectAllMissingIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"unrelated":1}`))
	}))
	defer srv.Close()
	cfg := config.CustomSourceConfig{
		Name:    "x",
		BaseURL: srv.URL,
		Metrics: []config.CustomMetricSpec{{Name: "a", Path: "nope", Unit: "count"}},
	}
	c := NewCustomSourceCollector(cfg, 5*time.Second)
	if _, err := c.Collect(); err == nil {
		t.Fatal("expected error when no fields found")
	}
}

func TestCustomSourceCollectNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg := config.CustomSourceConfig{
		Name:    "x",
		BaseURL: srv.URL,
		Metrics: []config.CustomMetricSpec{{Name: "a", Path: "a", Unit: "count"}},
	}
	c := NewCustomSourceCollector(cfg, 5*time.Second)
	if _, err := c.Collect(); err == nil {
		t.Fatal("expected error on 500 status")
	}
}

func TestCustomSourceCollectTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Write([]byte(`{"a":1}`))
	}))
	defer srv.Close()
	cfg := config.CustomSourceConfig{
		Name:    "x",
		BaseURL: srv.URL,
		Metrics: []config.CustomMetricSpec{{Name: "a", Path: "a", Unit: "count"}},
	}
	// interval 300ms → timeout capped to ~100ms, well under the 500ms handler.
	c := NewCustomSourceCollector(cfg, 300*time.Millisecond)
	if _, err := c.Collect(); err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestCustomSourceAuthAndHeaders(t *testing.T) {
	var gotAuth, gotHeader, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotHeader = r.Header.Get("X-Api-Key")
		gotAccept = r.Header.Get("Accept")
		w.Write([]byte(`{"a":1}`))
	}))
	defer srv.Close()

	// Bearer + custom request header.
	cfg := config.CustomSourceConfig{
		Name:    "x",
		BaseURL: srv.URL,
		Request: config.CustomRequestConfig{Headers: map[string]string{"Accept": "application/json"}},
		Auth:    config.CustomAuthConfig{Type: "bearer", Token: "sekret"},
		Metrics: []config.CustomMetricSpec{{Name: "a", Path: "a", Unit: "count"}},
	}
	if _, err := NewCustomSourceCollector(cfg, 5*time.Second).Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if gotAuth != "Bearer sekret" {
		t.Errorf("Authorization = %q, want Bearer sekret", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept header = %q", gotAccept)
	}

	// Arbitrary header auth.
	cfg.Auth = config.CustomAuthConfig{Type: "header", HeaderName: "X-Api-Key", HeaderValue: "abc"}
	if _, err := NewCustomSourceCollector(cfg, 5*time.Second).Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if gotHeader != "abc" {
		t.Errorf("X-Api-Key = %q, want abc", gotHeader)
	}
}

func TestCustomSourceRedactedURL(t *testing.T) {
	cfg := config.CustomSourceConfig{
		Name:    "x",
		BaseURL: "http://user:pass@host:8080",
		Request: config.CustomRequestConfig{Path: "/api?token=deadbeef&plain=ok"},
		Metrics: []config.CustomMetricSpec{{Name: "a", Path: "a", Unit: "count"}},
	}
	c := NewCustomSourceCollector(cfg, 5*time.Second)
	red := c.redactedURL()
	if strings.Contains(red, "deadbeef") {
		t.Errorf("redactedURL leaked token: %q", red)
	}
	if strings.Contains(red, "pass") {
		t.Errorf("redactedURL leaked userinfo: %q", red)
	}
	if !strings.Contains(red, "plain=ok") {
		t.Errorf("redactedURL dropped non-secret param: %q", red)
	}
}

func TestCustomSourceUnixSocket(t *testing.T) {
	dir := t.TempDir()
	sock := dir + "/svc.sock"
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix socket unavailable: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/info" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"ContainersRunning":3}`))
	})}
	go srv.Serve(ln)
	defer srv.Close()

	cfg := config.CustomSourceConfig{
		Name:       "docker",
		BaseURL:    "http://unix",
		UnixSocket: sock,
		Request:    config.CustomRequestConfig{Path: "/info"},
		Metrics:    []config.CustomMetricSpec{{Name: "running", Path: "ContainersRunning", Unit: "count"}},
	}
	c := NewCustomSourceCollector(cfg, 5*time.Second)
	sample, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect over unix socket: %v", err)
	}
	data := sample.Data.(CustomSourceData)
	if m, ok := metricByName(data, "running"); !ok || m.Value != 3 {
		t.Errorf("running = %+v, want 3", m)
	}
}
