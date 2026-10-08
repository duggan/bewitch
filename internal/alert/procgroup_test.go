//go:build unix

package alert

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/config"
)

// TestNotifierTimeoutKillsProcessTree reproduces the postdrop leak: a notifier
// command whose descendant outlives it and holds the output pipe. The timeout
// must return promptly (not block on the pipe) and take the descendant with it.
func TestNotifierTimeoutKillsProcessTree(t *testing.T) {
	old := notifyCmdTimeout
	// Long enough for sh to start and record its child even on a loaded CI box.
	notifyCmdTimeout = 2 * time.Second
	t.Cleanup(func() { notifyCmdTimeout = old })

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild.pid")
	script := filepath.Join(dir, "notify.sh")
	// The background loop inherits stdout/stderr, like postdrop under sendmail.
	body := "#!/bin/sh\n(while :; do sleep 1; done) &\necho $! > " + pidFile + "\nsleep 30\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	res := NewCommandNotifier(config.CommandDest{Cmd: script}).Send(&Alert{RuleName: "r", Severity: "warning"})
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("Send blocked %v after the %v timeout (waiting on a pipe held by a descendant)", elapsed, notifyCmdTimeout)
	}
	if !strings.Contains(res.Error, "timed out") {
		t.Errorf("error = %q, want a timeout", res.Error)
	}

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("script never recorded its descendant: %v", err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("descendant %d survived the notifier timeout (leaked)", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
