//go:build unix && !linux

package browser

import "syscall"

func exitWithParent(*syscall.SysProcAttr) {}
