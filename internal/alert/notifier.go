package alert

import (
	"fmt"
	"time"

	"github.com/charmbracelet/log"
)

// NotifyResult holds the outcome of sending a single notification.
type NotifyResult struct {
	Method     string        `json:"method"`                // "email", "command"
	Dest       string        `json:"dest"`                  // email address, command path
	StatusCode int           `json:"status_code,omitempty"` // HTTP status or 0 for non-HTTP
	Latency    time.Duration `json:"latency_ns"`
	Error      string        `json:"error,omitempty"`
	Body       string        `json:"body,omitempty"` // request body or command stdout
}

// Notifier sends alert notifications to a single destination.
type Notifier interface {
	// Name returns a human-readable identifier (e.g., "email:admin@x.com", "command:/usr/local/bin/alert").
	Name() string

	// Method returns the notification type ("email", "command").
	Method() string

	// Send delivers the alert and returns the result. Must be safe for concurrent use.
	Send(a *Alert) NotifyResult
}

// notifySem bounds the number of concurrent notification goroutines. Notifiers
// can block for up to ~10s (SMTP, shell commands); without a cap, a burst of
// fired alerts could spawn an unbounded number of goroutines.
var notifySem = make(chan struct{}, 8)

// notifyRetryDelays are the waits before each retry of a failed delivery: a
// webhook or SMTP blip shouldn't lose the alert (or its all-clear). A package var
// so tests can shorten it.
var notifyRetryDelays = []time.Duration{15 * time.Second, time.Minute}

// sendNotifications sends the alert to all notifiers asynchronously, retrying a
// failed delivery per notifyRetryDelays. Only a delivery that fails every attempt
// counts toward notifyFailures. Pending retries are dropped when the engine stops.
func (e *Engine) sendNotifications(a *Alert) {
	for _, n := range e.notifiers {
		go func(notifier Notifier) {
			for attempt := 0; ; attempt++ {
				notifySem <- struct{}{}
				r := notifier.Send(a)
				<-notifySem // not held across the retry wait
				if r.Error == "" {
					if attempt > 0 {
						log.Infof("%s: delivered %s on attempt %d", notifier.Name(), a.RuleName, attempt+1)
					}
					return
				}
				if attempt >= len(notifyRetryDelays) {
					e.notifyFailures.Add(1)
					log.Errorf("%s: %s (gave up on %s after %d attempts)", notifier.Name(), r.Error, a.RuleName, attempt+1)
					return
				}
				delay := notifyRetryDelays[attempt]
				log.Warnf("%s: %s (retrying %s in %s)", notifier.Name(), r.Error, a.RuleName, delay)
				select {
				case <-time.After(delay):
				case <-e.stop:
					return
				}
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
