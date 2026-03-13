package alert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ross/bewitch/internal/config"
)

// GotifyNotifier delivers alerts to a Gotify server.
type GotifyNotifier struct {
	cfg config.GotifyDest
}

func NewGotifyNotifier(cfg config.GotifyDest) *GotifyNotifier {
	return &GotifyNotifier{cfg: cfg}
}

func (n *GotifyNotifier) Name() string   { return "gotify:" + n.cfg.URL }
func (n *GotifyNotifier) Method() string { return "gotify" }

func (n *GotifyNotifier) Send(a *Alert) NotifyResult {
	url := strings.TrimRight(n.cfg.URL, "/") + "/message"
	result := NotifyResult{Method: "gotify", Dest: n.cfg.URL}

	payload := struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		Priority int    `json:"priority"`
	}{
		Title:    fmt.Sprintf("[%s] %s", a.Severity, a.RuleName),
		Message:  a.Message,
		Priority: gotifyPriority(a.Severity, n.cfg.Priority),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		result.Error = fmt.Sprintf("marshal: %v", err)
		return result
	}
	result.Body = string(body)

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		result.Error = fmt.Sprintf("request: %v", err)
		return result
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", n.cfg.Token)

	start := time.Now()
	resp, err := httpClient.Do(req)
	result.Latency = time.Since(start)
	if err != nil {
		result.Error = fmt.Sprintf("delivery: %v", err)
		return result
	}
	resp.Body.Close()
	result.StatusCode = resp.StatusCode
	if resp.StatusCode >= 400 {
		result.Error = resp.Status
	}
	return result
}

// gotifyPriority maps severity to Gotify priority (0-10).
// A non-zero config priority takes precedence over severity mapping.
func gotifyPriority(severity string, configPriority int) int {
	if configPriority > 0 {
		return configPriority
	}
	switch severity {
	case "critical":
		return 8
	case "warning":
		return 5
	default:
		return 4
	}
}
