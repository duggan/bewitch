package alert

import (
	"fmt"
	"strings"
	"time"

	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/router"
	"github.com/containrrr/shoutrrr/pkg/types"
)

// ShoutrrrNotifier delivers alerts via a Shoutrrr service URL, covering
// Discord/Telegram/ntfy/Slack/Pushover/generic-webhook/etc. from a single URL
// string. This is the "paste a Discord URL and you're done" path that homelab
// users expect; email + shell-command alone is a 2010 feature set.
type ShoutrrrNotifier struct {
	url    string
	sender *router.ServiceRouter
}

// NewShoutrrrNotifier parses and initializes the service for the given Shoutrrr
// URL. An invalid URL is rejected here so the engine can log and skip it rather
// than failing every send.
func NewShoutrrrNotifier(url string) (*ShoutrrrNotifier, error) {
	sender, err := shoutrrr.CreateSender(url)
	if err != nil {
		return nil, err
	}
	return &ShoutrrrNotifier{url: url, sender: sender}, nil
}

func (n *ShoutrrrNotifier) Name() string   { return "shoutrrr:" + redactShoutrrrURL(n.url) }
func (n *ShoutrrrNotifier) Method() string { return "shoutrrr" }

func (n *ShoutrrrNotifier) Send(a *Alert) NotifyResult {
	res := NotifyResult{Method: "shoutrrr", Dest: redactShoutrrrURL(n.url)}

	start := time.Now()
	errs := n.sender.Send(a.Message, &types.Params{"title": shoutrrrTitle(a)})
	res.Latency = time.Since(start)
	for _, e := range errs {
		if e != nil {
			res.Error = e.Error()
			break
		}
	}
	return res
}

// shoutrrrTitle builds the notification title, distinguishing a firing alert
// from a recovery (Resolved) all-clear.
func shoutrrrTitle(a *Alert) string {
	status := "FIRING"
	if a.Resolved {
		status = "RESOLVED"
	}
	return fmt.Sprintf("[bewitch] %s %s: %s", status, a.Severity, a.RuleName)
}

// redactShoutrrrURL returns only the scheme, dropping everything after "://".
// Shoutrrr URLs embed secrets (bot tokens) in the userinfo, host, or path
// depending on the service, so anything beyond the scheme is unsafe to surface
// in logs or the test-notification results. Keeping the scheme still identifies
// the service type (discord, telegram, ...).
func redactShoutrrrURL(raw string) string {
	scheme := raw
	if i := strings.Index(raw, "://"); i >= 0 {
		scheme = raw[:i]
	}
	return scheme + "://***"
}
