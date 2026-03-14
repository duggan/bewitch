package alert

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/duggan/bewitch/internal/config"
)

// CommandNotifier delivers alerts by executing a shell command.
// Alert details are passed as environment variables.
type CommandNotifier struct {
	cfg config.CommandDest
}

func NewCommandNotifier(cfg config.CommandDest) *CommandNotifier {
	return &CommandNotifier{cfg: cfg}
}

func (n *CommandNotifier) Name() string   { return "command:" + n.cfg.Cmd }
func (n *CommandNotifier) Method() string { return "command" }

func (n *CommandNotifier) Send(a *Alert) NotifyResult {
	result := NotifyResult{Method: "command", Dest: n.cfg.Cmd}

	args := strings.Fields(n.cfg.Cmd)
	if len(args) == 0 {
		result.Error = "empty command"
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = append(os.Environ(),
		"BEWITCH_RULE="+a.RuleName,
		"BEWITCH_SEVERITY="+a.Severity,
		"BEWITCH_MESSAGE="+a.Message,
		"BEWITCH_TIMESTAMP="+time.Now().UTC().Format(time.RFC3339),
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	result.Latency = time.Since(start)

	output := stdout.String()
	if errOut := stderr.String(); errOut != "" {
		if output != "" {
			output += "\n"
		}
		output += errOut
	}
	result.Body = output

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.Error = "command timed out (10s)"
		} else {
			result.Error = fmt.Sprintf("exit: %v", err)
		}
	}
	return result
}
