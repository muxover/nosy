//go:build !windows

package chrome

func allowSandboxedProcesses(string) error { return nil }
