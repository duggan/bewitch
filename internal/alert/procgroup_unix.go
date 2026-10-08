//go:build unix

package alert

import (
	"os/exec"
	"syscall"
	"time"
)

// contain makes a notifier command's timeout reach its whole process tree.
// exec.CommandContext only kills the direct child, so descendants survived:
// mail(1) → Postfix sendmail → postdrop kept retrying "Permission denied" under
// the sandbox long after the 10s timeout, accumulating until a restart, and
// because they still held our stdout/stderr pipes, Run() could block on them
// indefinitely. Run the command in its own process group, kill the group on
// timeout, and bound the wait for pipes held by anything that escapes.
func contain(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
}
