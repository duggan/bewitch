//go:build !unix

package alert

import (
	"os/exec"
	"time"
)

func contain(cmd *exec.Cmd) { cmd.WaitDelay = 2 * time.Second }
