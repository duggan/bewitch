package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/BurntSushi/toml"
)

// CustomSourceConfig declares an HTTP data source the daemon polls. Numeric
// fields ([[metric]]) become stored time-series; non-numeric fields ([[status]])
// are surfaced live-only in the TUI Services tab.
type CustomSourceConfig struct {
	Name       string              `toml:"name"`        // unique; [a-z0-9_-]; becomes the `source` column + Prometheus label + tab sub-section
	Enabled    *bool               `toml:"enabled"`     // defaults true (IsEnabled)
	Interval   string              `toml:"interval"`    // poll cadence; GetInterval(default); min 100ms
	Timeout    string              `toml:"timeout"`     // per-request timeout; GetTimeout(interval) caps it below the interval
	BaseURL    string              `toml:"base_url"`    // e.g. http://127.0.0.1:8080; required unless unix_socket is set
	UnixSocket string              `toml:"unix_socket"` // dial a unix socket (e.g. Docker /var/run/docker.sock) instead of TCP
	Request    CustomRequestConfig `toml:"request"`
	Auth       CustomAuthConfig    `toml:"auth"`
	Metrics    []CustomMetricSpec  `toml:"metric"`
	Status     []CustomStatusSpec  `toml:"status"`
}

// CustomRequestConfig describes the HTTP request issued each poll.
type CustomRequestConfig struct {
	Method  string            `toml:"method"` // default GET
	Path    string            `toml:"path"`
	Body    string            `toml:"body"` // optional request body (POST)
	Headers map[string]string `toml:"headers"`
}

// CustomAuthConfig holds optional authentication applied to each request.
// Secrets here are never logged (see the collector's redaction) and never
// echoed by the API.
type CustomAuthConfig struct {
	Type        string `toml:"type"` // none|bearer|basic|header
	Token       string `toml:"token"`
	Username    string `toml:"username"`
	Password    string `toml:"password"`
	HeaderName  string `toml:"header_name"`
	HeaderValue string `toml:"header_value"`
}

// CustomMetricSpec extracts one numeric value from the JSON response.
type CustomMetricSpec struct {
	Name string `toml:"name"` // metric key; (source, name) is the series identity
	Path string `toml:"path"` // gjson path into the response
	Unit string `toml:"unit"` // bytes|bits|percent|count|duration|raw (display hint)
}

// CustomStatusSpec extracts one non-numeric field rendered as a live status line.
type CustomStatusSpec struct {
	Label  string            `toml:"label"`
	Path   string            `toml:"path"`
	Badges map[string]string `toml:"badges"` // exact value -> ok|warn|crit (styling hint)
}

// IsEnabled reports whether the source is enabled (defaults to true), mirroring
// the temperature/power collector convention.
func (c *CustomSourceConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// GetInterval returns the poll interval, falling back to defaultInterval (min 100ms).
func (c *CustomSourceConfig) GetInterval(defaultInterval time.Duration) time.Duration {
	return collectorInterval(c.Interval, defaultInterval)
}

// Method returns the configured HTTP method, defaulting to GET.
func (c *CustomSourceConfig) Method() string {
	if c.Request.Method == "" {
		return "GET"
	}
	return c.Request.Method
}

// defaultCustomTimeout is used when a source doesn't specify one.
const defaultCustomTimeout = 5 * time.Second

// GetTimeout returns the per-request timeout, hard-capped below the poll interval
// so a hung endpoint can never outlast its own tick budget (collectors share a
// per-tick WaitGroup; an unbounded poll would stall the whole cycle).
func (c *CustomSourceConfig) GetTimeout(interval time.Duration) time.Duration {
	t := defaultCustomTimeout
	if c.Timeout != "" {
		if d, err := ParseDuration(c.Timeout); err == nil && d > 0 {
			t = d
		}
	}
	// Leave headroom below the interval; fall back to the interval itself for
	// sub-200ms intervals where the headroom would go negative.
	maxT := interval - 200*time.Millisecond
	if maxT < 100*time.Millisecond {
		maxT = interval
	}
	if t > maxT {
		t = maxT
	}
	return t
}

var (
	customNameRe   = regexp.MustCompile(`^[a-z0-9_-]+$`)
	validUnits     = map[string]bool{"": true, "bytes": true, "bits": true, "percent": true, "count": true, "duration": true, "raw": true}
	validAuthTypes = map[string]bool{"": true, "none": true, "bearer": true, "basic": true, "header": true}
)

// Validate checks a single source for structural correctness.
func (c *CustomSourceConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	if !customNameRe.MatchString(c.Name) {
		return fmt.Errorf("name %q must match [a-z0-9_-]+", c.Name)
	}
	if c.BaseURL == "" && c.UnixSocket == "" {
		return fmt.Errorf("base_url or unix_socket is required")
	}
	if c.UnixSocket == "" {
		u, err := url.Parse(c.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("base_url %q must be a valid http(s) URL", c.BaseURL)
		}
	}
	if len(c.Metrics) == 0 && len(c.Status) == 0 {
		return fmt.Errorf("at least one [[metric]] or [[status]] is required")
	}
	if c.Interval != "" {
		if _, err := ParseDuration(c.Interval); err != nil {
			return fmt.Errorf("invalid interval %q: %w", c.Interval, err)
		}
	}
	if c.Timeout != "" {
		if _, err := ParseDuration(c.Timeout); err != nil {
			return fmt.Errorf("invalid timeout %q: %w", c.Timeout, err)
		}
	}
	if !validAuthTypes[c.Auth.Type] {
		return fmt.Errorf("invalid auth.type %q (want none|bearer|basic|header)", c.Auth.Type)
	}
	seen := make(map[string]bool, len(c.Metrics))
	for _, m := range c.Metrics {
		if m.Name == "" {
			return fmt.Errorf("metric name is required")
		}
		if !customNameRe.MatchString(m.Name) {
			return fmt.Errorf("metric name %q must match [a-z0-9_-]+", m.Name)
		}
		if m.Path == "" {
			return fmt.Errorf("metric %q: path is required", m.Name)
		}
		if !validUnits[m.Unit] {
			return fmt.Errorf("metric %q: invalid unit %q", m.Name, m.Unit)
		}
		if seen[m.Name] {
			return fmt.Errorf("duplicate metric name %q", m.Name)
		}
		seen[m.Name] = true
	}
	for _, st := range c.Status {
		if st.Label == "" {
			return fmt.Errorf("status label is required")
		}
		if st.Path == "" {
			return fmt.Errorf("status %q: path is required", st.Label)
		}
	}
	return nil
}

// LoadSources merges inline [[custom_source]] definitions with drop-in *.toml
// files under dir, deduping by name (last-wins; later files and drop-ins override
// earlier/inline definitions). It returns the enabled, validated sources plus
// non-fatal warnings (e.g. name collisions). A malformed or invalid source is a
// fatal error so the operator notices before the daemon comes up half-configured.
// A missing dir is not an error.
func LoadSources(inline []CustomSourceConfig, dir string) (sources []CustomSourceConfig, warnings []string, err error) {
	type entry struct {
		src    CustomSourceConfig
		origin string
	}
	var all []entry
	for _, s := range inline {
		all = append(all, entry{s, "config"})
	}

	if dir != "" {
		files, globErr := filepath.Glob(filepath.Join(dir, "*.toml"))
		if globErr != nil {
			return nil, warnings, fmt.Errorf("scanning sources dir %s: %w", dir, globErr)
		}
		sort.Strings(files)
		for _, f := range files {
			data, readErr := os.ReadFile(f)
			if readErr != nil {
				return nil, warnings, fmt.Errorf("reading %s: %w", f, readErr)
			}
			var doc struct {
				CustomSources []CustomSourceConfig `toml:"custom_source"`
			}
			if uErr := toml.Unmarshal(data, &doc); uErr != nil {
				return nil, warnings, fmt.Errorf("parsing %s: %w", f, uErr)
			}
			base := filepath.Base(f)
			for _, s := range doc.CustomSources {
				all = append(all, entry{s, base})
			}
		}
	}

	// Dedup by name, last definition wins (deterministic: inline first, then
	// drop-ins in sorted filename order).
	idx := make(map[string]int)
	var merged []entry
	for _, e := range all {
		if i, ok := idx[e.src.Name]; ok {
			warnings = append(warnings, fmt.Sprintf("custom source %q in %s overrides earlier definition in %s",
				e.src.Name, e.origin, merged[i].origin))
			merged[i] = e
			continue
		}
		idx[e.src.Name] = len(merged)
		merged = append(merged, e)
	}

	for _, e := range merged {
		if !e.src.IsEnabled() {
			continue
		}
		if vErr := e.src.Validate(); vErr != nil {
			return nil, warnings, fmt.Errorf("custom source %q (%s): %w", e.src.Name, e.origin, vErr)
		}
		sources = append(sources, e.src)
	}
	return sources, warnings, nil
}

// DefaultSourcesDir returns the drop-in directory for custom-source files,
// defaulting to <configdir>/sources.d relative to the main config file when
// [daemon] sources_dir is unset.
func (c *DaemonConfig) DefaultSourcesDir(configPath string) string {
	if c.SourcesDir != "" {
		return c.SourcesDir
	}
	if configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), "sources.d")
}
