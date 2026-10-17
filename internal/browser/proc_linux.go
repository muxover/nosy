//go:build linux

package browser

import "syscall"

func exitWithParent(a *syscall.SysProcAttr) { a.Pdeathsig = syscall.SIGKILL }
