package collector

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"github.com/duggan/bewitch/internal/config"
)

// CustomMetricSample is one numeric value extracted from a custom source's
// response. Stored as a time-series and exported to Prometheus.
type CustomMetricSample struct {
	Name  string
	Unit  string // display hint: bytes|bits|percent|count|duration|raw
	Value float64
}

// CustomStatusSample is one non-numeric field extracted from a custom source's
// response. Live-only — never stored, never exported.
type CustomStatusSample struct {
	Label string
	Value string
	Badge string // ""|ok|warn|crit
}

// CustomSourceData is one poll's worth of a single source's extracted fields.
type CustomSourceData struct {
	Source  string
	Metrics []CustomMetricSample
	Status  []CustomStatusSample
}

const (
	// maxCustomBody caps how much of a response body we buffer for extraction.
	// Mirrors the API server's maxRequestBody so a misbehaving endpoint can't
	// OOM the (memory-capped) daemon.
	maxCustomBody = 1 << 20 // 1 MiB
	// customDialTimeout bounds connection establishment independent of the
	// overall request timeout.
	customDialTimeout = 3 * time.Second
)

// CustomSourceCollector polls one user-defined HTTP source and extracts metrics
// and status fields. One collector per source, so each gets its own interval and
// independent exponential backoff (see the daemon's scheduler).
type CustomSourceCollector struct {
	cfg     config.CustomSourceConfig
	client  *http.Client
	reqURL  string
	timeout time.Duration
}

// NewCustomSourceCollector builds a collector for a single source. interval is
// the resolved poll cadence, used only to cap the per-request timeout below it.
func NewCustomSourceCollector(cfg config.CustomSourceConfig, interval time.Duration) *CustomSourceCollector {
	timeout := cfg.GetTimeout(interval)

	transport := &http.Transport{
		DialContext: (&net.Dialer{Timeout: customDialTimeout}).DialContext,
		// Bound time spent waiting for response headers as a second line of
		// defence alongside Client.Timeout (which also covers body reads).
		ResponseHeaderTimeout: timeout,
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if cfg.UnixSocket != "" {
		// Dial the unix socket regardless of the request host; the URL host is
		// just a placeholder Go requires.
		sock := cfg.UnixSocket
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: customDialTimeout}).DialContext(ctx, "unix", sock)
		}
		if base == "" {
			base = "http://unix"
		}
	}

	path := cfg.Request.Path
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return &CustomSourceCollector{
		cfg:    cfg,
		reqURL: base + path,
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
			// Disable redirects so a target can't 30x-pivot the privileged
			// daemon to an internal/metadata endpoint.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		timeout: timeout,
	}
}

func (c *CustomSourceCollector) Name() string { return "custom:" + c.cfg.Name }

func (c *CustomSourceCollector) Collect() (Sample, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	var body io.Reader
	if c.cfg.Request.Body != "" {
		body = strings.NewReader(c.cfg.Request.Body)
	}
	req, err := http.NewRequestWithContext(ctx, c.cfg.Method(), c.reqURL, body)
	if err != nil {
		return Sample{}, fmt.Errorf("%s: build request: %w", c.Name(), err)
	}
	for k, v := range c.cfg.Request.Headers {
		req.Header.Set(k, v)
	}
	c.applyAuth(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return Sample{}, fmt.Errorf("%s %s: %w", c.Name(), c.redactedURL(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Sample{}, fmt.Errorf("%s %s: status %d", c.Name(), c.redactedURL(), resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxCustomBody))
	if err != nil {
		return Sample{}, fmt.Errorf("%s %s: read body: %w", c.Name(), c.redactedURL(), err)
	}

	data := CustomSourceData{Source: c.cfg.Name}
	found := 0
	for _, m := range c.cfg.Metrics {
		res := gjson.GetBytes(raw, m.Path)
		if !res.Exists() {
			continue
		}
		data.Metrics = append(data.Metrics, CustomMetricSample{Name: m.Name, Unit: m.Unit, Value: res.Float()})
		found++
	}
	for _, st := range c.cfg.Status {
		res := gjson.GetBytes(raw, st.Path)
		if !res.Exists() {
			continue
		}
		val := res.String()
		data.Status = append(data.Status, CustomStatusSample{Label: st.Label, Value: val, Badge: st.Badges[val]})
		found++
	}
	// Every path missing means the response shape changed or the endpoint is
	// wrong — surface it as an error so backoff + the WARN engage rather than
	// silently storing nothing forever.
	if found == 0 {
		return Sample{}, fmt.Errorf("%s %s: none of the configured fields were found in the response", c.Name(), c.redactedURL())
	}

	return Sample{Timestamp: time.Now(), Kind: "custom", Data: data}, nil
}

func (c *CustomSourceCollector) applyAuth(req *http.Request) {
	switch c.cfg.Auth.Type {
	case "bearer":
		if c.cfg.Auth.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.cfg.Auth.Token)
		}
	case "basic":
		req.SetBasicAuth(c.cfg.Auth.Username, c.cfg.Auth.Password)
	case "header":
		if c.cfg.Auth.HeaderName != "" {
			req.Header.Set(c.cfg.Auth.HeaderName, c.cfg.Auth.HeaderValue)
		}
	}
}

// redactedURL returns only the scheme and host of the request URL, safe for logs
// (precedent: shoutrrr URL redaction). API keys are commonly embedded in the path
// (e.g. https://host/v1/<apikey>/states) or query string, so scrubbing only
// userinfo/known-secret params would still leak them on a collector error — drop
// the path and query entirely. Auth headers/tokens/passwords are never logged.
func (c *CustomSourceCollector) redactedURL() string {
	u, err := url.Parse(c.reqURL)
	if err != nil || u.Host == "" {
		return c.cfg.Name
	}
	return u.Scheme + "://" + u.Host
}
