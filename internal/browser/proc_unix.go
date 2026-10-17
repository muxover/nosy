//go:build unix

package browser

import (
	"os/exec"
	"syscall"
	"time"
)

// confine puts the browser in its own process group, so that killTree reaches
// every helper it started, and where the system can, has the kernel kill it
// if the scanner dies.
func confine(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	exitWithParent(cmd.SysProcAttr)
}

// killTree kills the browser and every process it started.
func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_, _ = cmd.Process.Wait()
}

// waitExit waits up to d for the browser process to be gone.
func waitExit(cmd *exec.Cmd, d time.Duration) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if syscall.Kill(cmd.Process.Pid, 0) != nil {
			return
		}
	}
}

// terminate asks the browser to exit on its own.
func terminate(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(cmd.Process.Pid, syscall.SIGTERM)
	}
}
