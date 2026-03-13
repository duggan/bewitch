package alert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ross/bewitch/internal/config"
)

type webhookPayload struct {
	RuleName  string `json:"rule_name"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// WebhookNotifier delivers alerts to an HTTP webhook endpoint.
type WebhookNotifier struct {
	dest config.WebhookDest
}

func NewWebhookNotifier(dest config.WebhookDest) *WebhookNotifier {
	return &WebhookNotifier{dest: dest}
}

func (n *WebhookNotifier) Name() string   { return "webhook:" + n.dest.URL }
func (n *WebhookNotifier) Method() string { return "webhook" }

func (n *WebhookNotifier) Send(a *Alert) NotifyResult {
	result := NotifyResult{Method: "webhook", Dest: n.dest.URL}

	payload := webhookPayload{
		RuleName:  a.RuleName,
		Severity:  a.Severity,
		Message:   a.Message,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		result.Error = fmt.Sprintf("marshal: %v", err)
		return result
	}
	result.Body = string(body)

	req, err := http.NewRequest("POST", n.dest.URL, bytes.NewReader(body))
	if err != nil {
		result.Error = fmt.Sprintf("request: %v", err)
		return result
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range n.dest.Headers {
		req.Header.Set(k, v)
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
