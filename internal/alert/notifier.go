package alert

import (
	"fmt"
	"time"

	"github.com/charmbracelet/log"
)

// NotifyResult holds the outcome of sending a single notification.
type NotifyResult struct {
	Method     string        `json:"method"`               // "webhook", "ntfy", "email", "gotify", "command"
	Dest       string        `json:"dest"`                 // URL, email address, command path
	StatusCode int           `json:"status_code,omitempty"` // HTTP status or 0 for non-HTTP
	Latency    time.Duration `json:"latency_ns"`
	Error      string        `json:"error,omitempty"`
	Body       string        `json:"body,omitempty"` // request body or command stdout
}

// Notifier sends alert notifications to a single destination.
type Notifier interface {
	// Name returns a human-readable identifier (e.g., "ntfy:my-alerts", "email:admin@x.com").
	Name() string

	// Method returns the notification type ("webhook", "ntfy", "email", "gotify", "command").
	Method() string

	// Send delivers the alert and returns the result. Must be safe for concurrent use.
	Send(a *Alert) NotifyResult
}

// sendNotifications sends the alert to all notifiers asynchronously (fire-and-forget).
func sendNotifications(notifiers []Notifier, a *Alert) {
	for _, n := range notifiers {
		go func(notifier Notifier) {
			r := notifier.Send(a)
			if r.Error != "" {
				log.Errorf("%s: %s", notifier.Name(), r.Error)
			}
		}(n)
	}
}

// SendTestNotifications sends the alert to all notifiers synchronously,
// returning per-destination results.
func SendTestNotifications(notifiers []Notifier, a *Alert) ([]NotifyResult, error) {
	if len(notifiers) == 0 {
		return nil, fmt.Errorf("no notification methods configured")
	}
	var results []NotifyResult
	for _, n := range notifiers {
		results = append(results, n.Send(a))
	}
	return results, nil
}
