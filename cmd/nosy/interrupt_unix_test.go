//go:build nosytest && !windows

package main

import (
	"os"
	"os/exec"
)

// interruptable starts the command the way Ctrl+C in a terminal would reach it.
func interruptable(cmd *exec.Cmd) {}

// sendInterrupt is Ctrl+C: SIGINT.
func sendInterrupt(cmd *exec.Cmd) error { return cmd.Process.Signal(os.Interrupt) }
