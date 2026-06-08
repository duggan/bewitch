package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func boolPtr(b bool) *bool { return &b }

func validSource(name string) CustomSourceConfig {
	return CustomSourceConfig{
		Name:    name,
		BaseURL: "http://127.0.0.1:8080",
		Metrics: []CustomMetricSpec{{Name: "m1", Path: "a.b", Unit: "bytes"}},
	}
}

func TestCustomSourceValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*CustomSourceConfig)
		wantErr bool
	}{
		{"valid", func(*CustomSourceConfig) {}, false},
		{"empty name", func(c *CustomSourceConfig) { c.Name = "" }, true},
		{"bad name chars", func(c *CustomSourceConfig) { c.Name = "Bad Name!" }, true},
		{"no url or socket", func(c *CustomSourceConfig) { c.BaseURL = "" }, true},
		{"unix socket no url ok", func(c *CustomSourceConfig) { c.BaseURL = ""; c.UnixSocket = "/var/run/x.sock" }, false},
		{"bad url scheme", func(c *CustomSourceConfig) { c.BaseURL = "ftp://x" }, true},
		{"no fields", func(c *CustomSourceConfig) { c.Metrics = nil; c.Status = nil }, true},
		{"status only ok", func(c *CustomSourceConfig) {
			c.Metrics = nil
			c.Status = []CustomStatusSpec{{Label: "S", Path: "v"}}
		}, false},
		{"bad unit", func(c *CustomSourceConfig) { c.Metrics[0].Unit = "furlongs" }, true},
		{"bad metric name", func(c *CustomSourceConfig) { c.Metrics[0].Name = "Bad!" }, true},
		{"metric no path", func(c *CustomSourceConfig) { c.Metrics[0].Path = "" }, true},
		{"dup metric name", func(c *CustomSourceConfig) {
			c.Metrics = append(c.Metrics, CustomMetricSpec{Name: "m1", Path: "c", Unit: "count"})
		}, true},
		{"bad auth type", func(c *CustomSourceConfig) { c.Auth.Type = "oauth" }, true},
		{"bearer auth ok", func(c *CustomSourceConfig) { c.Auth.Type = "bearer"; c.Auth.Token = "x" }, false},
		{"bad interval", func(c *CustomSourceConfig) { c.Interval = "soon" }, true},
		{"status no label", func(c *CustomSourceConfig) { c.Status = []CustomStatusSpec{{Path: "v"}} }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validSource("svc")
			tt.mutate(&c)
			err := c.Validate()
			if tt.wantErr != (err != nil) {
				t.Fatalf("Validate() err=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestCustomSourceHelpers(t *testing.T) {
	c := validSource("svc")
	if !c.IsEnabled() {
		t.Error("unset Enabled should default true")
	}
	c.Enabled = boolPtr(false)
	if c.IsEnabled() {
		t.Error("Enabled=false should be disabled")
	}

	c2 := validSource("svc")
	c2.Interval = "10s"
	if got := c2.GetInterval(5 * time.Second); got != 10*time.Second {
		t.Errorf("GetInterval = %v, want 10s", got)
	}
	cdef := validSource("x")
	if got := cdef.GetInterval(7 * time.Second); got != 7*time.Second {
		t.Errorf("GetInterval fallback = %v, want 7s", got)
	}

	// Timeout is capped below the interval.
	c3 := validSource("svc")
	c3.Timeout = "30s"
	if got := c3.GetTimeout(10 * time.Second); got >= 10*time.Second {
		t.Errorf("GetTimeout = %v, must be < interval 10s", got)
	}
	// Default timeout when unset, still capped.
	c4 := validSource("x")
	if got := c4.GetTimeout(2 * time.Second); got > 2*time.Second {
		t.Errorf("GetTimeout default = %v, must be <= interval 2s", got)
	}
	// Method defaults to GET.
	c5 := validSource("x")
	if c5.Method() != "GET" {
		t.Error("Method() should default to GET")
	}
}

func TestLoadSourcesMergeAndValidate(t *testing.T) {
	dir := t.TempDir()
	// Drop-in file defining two sources; one overrides an inline source.
	dropin := `
[[custom_source]]
name = "homeassistant"
base_url = "http://127.0.0.1:8123"
  [[custom_source.metric]]
  name = "entities"
  path = "size"
  unit = "count"

[[custom_source]]
name = "pihole"
base_url = "http://127.0.0.1:9999"
  [[custom_source.metric]]
  name = "blocked"
  path = "ads_blocked_today"
  unit = "count"
`
	if err := os.WriteFile(filepath.Join(dir, "services.toml"), []byte(dropin), 0o644); err != nil {
		t.Fatal(err)
	}

	inline := []CustomSourceConfig{validSource("pihole")} // base_url 8080, overridden by drop-in 9999

	sources, warnings, err := LoadSources(inline, dir)
	if err != nil {
		t.Fatalf("LoadSources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("got %d sources, want 2 (pihole merged): %+v", len(sources), sources)
	}
	if len(warnings) != 1 {
		t.Errorf("expected 1 override warning, got %d: %v", len(warnings), warnings)
	}
	byName := map[string]CustomSourceConfig{}
	for _, s := range sources {
		byName[s.Name] = s
	}
	if byName["pihole"].BaseURL != "http://127.0.0.1:9999" {
		t.Errorf("drop-in should override inline: got %q", byName["pihole"].BaseURL)
	}
	if _, ok := byName["homeassistant"]; !ok {
		t.Error("homeassistant source missing")
	}
}

func TestLoadSourcesSkipsDisabled(t *testing.T) {
	off := validSource("off")
	off.Enabled = boolPtr(false)
	on := validSource("on")
	sources, _, err := LoadSources([]CustomSourceConfig{off, on}, "")
	if err != nil {
		t.Fatalf("LoadSources: %v", err)
	}
	if len(sources) != 1 || sources[0].Name != "on" {
		t.Fatalf("disabled source should be skipped, got %+v", sources)
	}
}

func TestLoadSourcesInvalidIsFatal(t *testing.T) {
	bad := validSource("bad")
	bad.Metrics[0].Unit = "furlongs"
	if _, _, err := LoadSources([]CustomSourceConfig{bad}, ""); err == nil {
		t.Fatal("expected error for invalid source unit")
	}
}

func TestLoadSourcesMissingDirOK(t *testing.T) {
	sources, _, err := LoadSources([]CustomSourceConfig{validSource("x")}, "/nonexistent/sources.d")
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("got %d sources, want 1", len(sources))
	}
}

func TestDefaultSourcesDir(t *testing.T) {
	d := &DaemonConfig{}
	if got := d.DefaultSourcesDir("/etc/bewitch.toml"); got != "/etc/sources.d" {
		t.Errorf("DefaultSourcesDir = %q, want /etc/sources.d", got)
	}
	d.SourcesDir = "/custom/dir"
	if got := d.DefaultSourcesDir("/etc/bewitch.toml"); got != "/custom/dir" {
		t.Errorf("explicit SourcesDir = %q, want /custom/dir", got)
	}
}
