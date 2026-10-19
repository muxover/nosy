//go:build nosytest && windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

// interruptable starts the command in a process group of its own, so that a
// console control event sent to the group reaches it and not the test.
func interruptable(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// sendInterrupt sends CTRL_BREAK_EVENT to the command's process group, which Go
// delivers to the program as os.Interrupt.
func sendInterrupt(cmd *exec.Cmd) error {
	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return err
	}
	proc, err := dll.FindProc("GenerateConsoleCtrlEvent")
	if err != nil {
		return err
	}
	r, _, callErr := proc.Call(uintptr(syscall.CTRL_BREAK_EVENT), uintptr(cmd.Process.Pid))
	if r == 0 {
		return fmt.Errorf("GenerateConsoleCtrlEvent: %w", callErr)
	}
	return nil
}
