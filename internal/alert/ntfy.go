package alert

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ross/bewitch/internal/config"
)

// NtfyNotifier delivers alerts via ntfy (https://ntfy.sh or self-hosted).
type NtfyNotifier struct {
	cfg config.NtfyDest
}

func NewNtfyNotifier(cfg config.NtfyDest) *NtfyNotifier {
	return &NtfyNotifier{cfg: cfg}
}

func (n *NtfyNotifier) Name() string   { return "ntfy:" + n.cfg.Topic }
func (n *NtfyNotifier) Method() string { return "ntfy" }

func (n *NtfyNotifier) Send(a *Alert) NotifyResult {
	url := strings.TrimRight(n.cfg.URL, "/") + "/" + n.cfg.Topic
	result := NotifyResult{Method: "ntfy", Dest: url}

	body := []byte(a.Message)
	result.Body = string(body)

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		result.Error = fmt.Sprintf("request: %v", err)
		return result
	}

	req.Header.Set("Title", fmt.Sprintf("[%s] %s", a.Severity, a.RuleName))
	req.Header.Set("Priority", ntfyPriority(a.Severity))
	req.Header.Set("Tags", ntfyTags(a.Severity))

	if n.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+n.cfg.Token)
	}

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

func ntfyPriority(severity string) string {
	switch severity {
	case "critical":
		return "5" // urgent
	case "warning":
		return "3" // default
	default:
		return "3"
	}
}

func ntfyTags(severity string) string {
	switch severity {
	case "critical":
		return "rotating_light"
	case "warning":
		return "warning"
	default:
		return "information_source"
	}
}
