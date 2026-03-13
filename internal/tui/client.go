package tui

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ross/bewitch/internal/api"
)

// ErrNotModified is returned by Get* methods when the server responds with
// 304 Not Modified, indicating the data hasn't changed since the last request.
var ErrNotModified = errors.New("not modified")

// daemonClient defines the methods the TUI needs from the daemon API.
// DaemonClient (below) is the production implementation; tests can substitute a mock.
type daemonClient interface {
	GetStatus() (map[string]any, error)
	GetDashboard() (*api.DashboardData, error)
	GetCPU() ([]api.CPUCoreMetric, error)
	GetMemory() (*api.MemoryMetric, error)
	GetECC() (*api.ECCMetric, error)
	GetDisk() ([]api.DiskMetric, error)
	GetNetwork() ([]api.NetworkMetric, error)
	GetTemperature() ([]api.TemperatureMetric, error)
	GetPower() ([]api.PowerMetric, error)
	GetProcesses() (*api.ProcessResponse, error)
	GetAlerts() ([]api.AlertMetric, error)
	GetHistory(metric string, start, end time.Time) ([]api.TimeSeries, error)
	GetHistoryByName(metric string, start, end time.Time, names []string) ([]api.TimeSeries, error)
	GetAlertRules() ([]api.AlertRuleMetric, error)
	CreateAlertRule(rule api.AlertRuleMetric) error
	DeleteAlertRule(id int) error
	ToggleAlertRule(id int) error
	AckAlert(id int) error
	GetPreferences() (map[string]string, error)
	SetPreference(key, value string) error
	Compact() error
	TestNotifications(alert TestNotificationAlert) ([]NotifyTestResult, error)
}

// DaemonClient communicates with bewitchd over the unix socket or TCP API.
type DaemonClient struct {
	http    *http.Client
	baseURL string // "http://bewitch" for unix, "http://host:port" for TCP
	etagsMu sync.Mutex
	etags   map[string]string // path → last ETag value for change detection
}

// NewDaemonClient creates a client that connects via unix socket.
func NewDaemonClient(socketPath string) *DaemonClient {
	return &DaemonClient{
		baseURL: "http://bewitch",
		etags:   make(map[string]string),
		http: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", socketPath)
				},
			},
		},
	}
}

// NewDaemonClientTCP creates a client that connects via TCP to the given address.
// If tlsCfg is non-nil, the connection uses HTTPS with the given TLS configuration.
// If token is non-empty, an Authorization: Bearer header is injected on every request.
func NewDaemonClientTCP(addr string, tlsCfg *tls.Config, token string) *DaemonClient {
	scheme := "http"
	transport := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	}
	if tlsCfg != nil {
		scheme = "https"
		transport.TLSClientConfig = tlsCfg
	}
	var rt http.RoundTripper = transport
	if token != "" {
		rt = &api.AuthTransport{Base: transport, Token: token}
	}
	return &DaemonClient{
		baseURL: scheme + "://" + addr,
		etags:   make(map[string]string),
		http: &http.Client{
			Timeout:   5 * time.Second,
			Transport: rt,
		},
	}
}

func (c *DaemonClient) GetStatus() (map[string]any, error) {
	var resp api.StatusResponse
	if err := c.getJSON("/api/status", &resp); err != nil {
		return nil, err
	}
	result := map[string]any{
		"status":               resp.Status,
		"uptime_sec":           resp.UptimeSec,
		"default_interval":     resp.DefaultInterval,
		"collector_intervals":  resp.CollectorIntervals,
	}
	return result, nil
}

func (c *DaemonClient) GetDashboard() (*api.DashboardData, error) {
	var dash api.DashboardData
	if err := c.getJSON("/api/metrics/dashboard", &dash); err != nil {
		return nil, err
	}
	return &dash, nil
}

func (c *DaemonClient) GetCPU() ([]api.CPUCoreMetric, error) {
	var resp api.CPUResponse
	if err := c.getJSON("/api/metrics/cpu", &resp); err != nil {
		return nil, err
	}
	return resp.Cores, nil
}

func (c *DaemonClient) GetMemory() (*api.MemoryMetric, error) {
	var mem api.MemoryMetric
	if err := c.getJSON("/api/metrics/memory", &mem); err != nil {
		return nil, err
	}
	return &mem, nil
}

func (c *DaemonClient) GetECC() (*api.ECCMetric, error) {
	var resp api.ECCResponse
	if err := c.getJSON("/api/metrics/ecc", &resp); err != nil {
		return nil, err
	}
	if resp.ECC == nil {
		return &api.ECCMetric{}, nil
	}
	return resp.ECC, nil
}

func (c *DaemonClient) GetDisk() ([]api.DiskMetric, error) {
	var resp api.DiskResponse
	if err := c.getJSON("/api/metrics/disk", &resp); err != nil {
		return nil, err
	}
	return resp.Disks, nil
}

func (c *DaemonClient) GetNetwork() ([]api.NetworkMetric, error) {
	var resp api.NetworkResponse
	if err := c.getJSON("/api/metrics/network", &resp); err != nil {
		return nil, err
	}
	return resp.Interfaces, nil
}

func (c *DaemonClient) GetTemperature() ([]api.TemperatureMetric, error) {
	var resp api.TemperatureResponse
	if err := c.getJSON("/api/metrics/temperature", &resp); err != nil {
		return nil, err
	}
	return resp.Sensors, nil
}

func (c *DaemonClient) GetPower() ([]api.PowerMetric, error) {
	var resp api.PowerResponse
	if err := c.getJSON("/api/metrics/power", &resp); err != nil {
		return nil, err
	}
	return resp.Zones, nil
}

func (c *DaemonClient) GetProcesses() (*api.ProcessResponse, error) {
	var resp api.ProcessResponse
	if err := c.getJSON("/api/metrics/process", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *DaemonClient) GetAlerts() ([]api.AlertMetric, error) {
	var resp api.AlertsResponse
	if err := c.getJSON("/api/alerts", &resp); err != nil {
		return nil, err
	}
	return resp.Alerts, nil
}

func (c *DaemonClient) GetHistory(metric string, start, end time.Time) ([]api.TimeSeries, error) {
	// Quantize timestamps to 10-second boundaries so the URL (and therefore
	// the ETag cache key) stays stable across rolling TUI ticks, enabling
	// 304 Not Modified responses from the server's matching quantized cache.
	const q int64 = 10
	qs := start.Unix() / q * q
	qe := end.Unix() / q * q
	path := fmt.Sprintf("/api/history/%s?start=%d&end=%d", metric, qs, qe)
	var resp api.HistoryResponse
	if err := c.getJSON(path, &resp); err != nil {
		return nil, err
	}
	return resp.Series, nil
}

func (c *DaemonClient) GetHistoryByName(metric string, start, end time.Time, names []string) ([]api.TimeSeries, error) {
	encoded := make([]string, len(names))
	for i, n := range names {
		encoded[i] = url.QueryEscape(n)
	}
	// Quantize to match server-side cache boundaries (see GetHistory).
	const q int64 = 10
	qs := start.Unix() / q * q
	qe := end.Unix() / q * q
	path := fmt.Sprintf("/api/history/%s?start=%d&end=%d&names=%s",
		metric, qs, qe, strings.Join(encoded, ","))
	var resp api.HistoryResponse
	if err := c.getJSON(path, &resp); err != nil {
		return nil, err
	}
	return resp.Series, nil
}

func (c *DaemonClient) AckAlert(id int) error {
	path := fmt.Sprintf("/api/alerts/%d/ack", id)
	resp, err := c.http.Post(c.baseURL+path, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return extractPostError("POST", path, resp)
	}
	return nil
}

func (c *DaemonClient) Compact() error {
	resp, err := c.http.Post(c.baseURL+"/api/compact", "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return extractPostError("POST", "/api/compact", resp)
	}
	return nil
}

func (c *DaemonClient) Archive() error {
	client := &http.Client{
		Timeout:   5 * time.Minute,
		Transport: c.http.Transport,
	}
	resp, err := client.Post(c.baseURL+"/api/archive", "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return extractPostError("POST", "/api/archive", resp)
	}
	return nil
}

func (c *DaemonClient) Unarchive() error {
	client := &http.Client{
		Timeout:   5 * time.Minute,
		Transport: c.http.Transport,
	}
	resp, err := client.Post(c.baseURL+"/api/unarchive", "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return extractPostError("POST", "/api/unarchive", resp)
	}
	return nil
}

func (c *DaemonClient) Snapshot(path string, withSystemTables bool) (*api.SnapshotResponse, error) {
	body, err := json.Marshal(api.SnapshotRequest{Path: path, WithSystemTables: withSystemTables})
	if err != nil {
		return nil, err
	}
	// Use a longer timeout for snapshots (large DBs may take a while)
	client := &http.Client{
		Timeout:   5 * time.Minute,
		Transport: c.http.Transport,
	}
	resp, err := client.Post(c.baseURL+"/api/snapshot", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	var sr api.SnapshotResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if sr.Error != "" {
		return nil, fmt.Errorf("%s", sr.Error)
	}
	return &sr, nil
}

func (c *DaemonClient) GetPreferences() (map[string]string, error) {
	var resp api.PreferencesResponse
	if err := c.getJSON("/api/preferences", &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func (c *DaemonClient) SetPreference(key, value string) error {
	body, err := json.Marshal(struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}{Key: key, Value: value})
	if err != nil {
		return err
	}
	resp, err := c.http.Post(c.baseURL+"/api/preferences", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return extractPostError("POST", "/api/preferences", resp)
	}
	return nil
}

func (c *DaemonClient) GetAlertRules() ([]api.AlertRuleMetric, error) {
	var resp api.AlertRulesResponse
	if err := c.getJSON("/api/alert-rules", &resp); err != nil {
		return nil, err
	}
	return resp.Rules, nil
}

func (c *DaemonClient) CreateAlertRule(rule api.AlertRuleMetric) error {
	body, err := json.Marshal(rule)
	if err != nil {
		return err
	}
	resp, err := c.http.Post(c.baseURL+"/api/alert-rules", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return extractPostError("POST", "/api/alert-rules", resp)
	}
	return nil
}

func (c *DaemonClient) DeleteAlertRule(id int) error {
	path := fmt.Sprintf("/api/alert-rules/%d", id)
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return extractPostError("DELETE", path, resp)
	}
	return nil
}

func (c *DaemonClient) ToggleAlertRule(id int) error {
	path := fmt.Sprintf("/api/alert-rules/%d/toggle", id)
	req, err := http.NewRequest(http.MethodPut, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return extractPostError("PUT", path, resp)
	}
	return nil
}

// NotifyTestResult holds the outcome of a single notification test delivery.
type NotifyTestResult struct {
	Method     string `json:"method"`
	Dest       string `json:"dest"`
	StatusCode int    `json:"status_code"`
	LatencyNs  int64  `json:"latency_ns"`
	Error      string `json:"error,omitempty"`
	Body       string `json:"body,omitempty"`
}

// TestNotificationAlert describes the alert payload to send when testing notifications.
type TestNotificationAlert struct {
	RuleName string `json:"rule_name"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

func (c *DaemonClient) TestNotifications(alert TestNotificationAlert) ([]NotifyTestResult, error) {
	payload, _ := json.Marshal(alert)
	resp, err := c.http.Post(c.baseURL+"/api/test-notifications", "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, extractPostError("POST", "/api/test-notifications", resp)
	}
	var result struct {
		Results []NotifyTestResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	return result.Results, nil
}

// getJSON performs a GET request with ETag-based change detection and
// decodes the JSON response into v. Returns ErrNotModified on 304.
func (c *DaemonClient) getJSON(path string, v any) error {
	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return err
	}
	c.etagsMu.Lock()
	if etag, ok := c.etags[path]; ok {
		req.Header.Set("If-None-Match", etag)
	}
	c.etagsMu.Unlock()
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return ErrNotModified
	}
	if etag := resp.Header.Get("ETag"); etag != "" {
		c.etagsMu.Lock()
		c.etags[path] = etag
		c.etagsMu.Unlock()
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		detail := extractErrorDetail(body)
		if detail != "" {
			return fmt.Errorf("GET %s: %s: %s", path, resp.Status, detail)
		}
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// extractPostError reads the response body and returns a formatted error
// including any error detail from the JSON response.
func extractPostError(method, path string, resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	detail := extractErrorDetail(body)
	if detail != "" {
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, detail)
	}
	return fmt.Errorf("%s %s: %s", method, path, resp.Status)
}

// extractErrorDetail tries to read an error message from a JSON
// GenericResponse body, falling back to the raw body text.
func extractErrorDetail(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var gr api.GenericResponse
	if json.Unmarshal(body, &gr) == nil {
		if gr.Error != "" {
			return gr.Error
		}
		if gr.Status != "" {
			return gr.Status
		}
	}
	// Fall back to raw body (might be plain text error)
	s := string(body)
	if len(s) > 200 {
		s = s[:200]
	}
	return strings.TrimSpace(s)
}
