//go:build !nosytest

package browser

import (
	"os/exec"

	"github.com/chromedp/chromedp"
)

func testFlags() []chromedp.ExecAllocatorOption { return nil }

func started(*exec.Cmd) {}
