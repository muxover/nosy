//go:build !unix

package browser

import (
	"os/exec"
	"strconv"
	"time"
)

// confine makes the end of the browser's context end every process it started.
// Windows has no process groups to signal, so the tree is taken down by
// taskkill while the main process is still alive to name it.
func confine(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		killTree(cmd)
		return nil
	}
}

// killTree kills the browser and every process it started.
func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	_ = cmd.Process.Kill()
}

func waitExit(cmd *exec.Cmd, d time.Duration) {}

func terminate(cmd *exec.Cmd) {}
