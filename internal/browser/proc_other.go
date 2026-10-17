//go:build !linux

package browser

import (
	"os/exec"
	"time"
)

func confine(cmd *exec.Cmd) {}

func killTree(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func waitExit(cmd *exec.Cmd, d time.Duration) {}

func terminate(cmd *exec.Cmd) {}
