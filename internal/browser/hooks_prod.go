//go:build !nosytest

package browser

import (
	"net/url"
	"os/exec"
	"time"

	"github.com/chromedp/chromedp"
)

func testFlags() []chromedp.ExecAllocatorOption { return nil }

func started(*exec.Cmd) {}

func budgetFor(_ *url.URL, budget time.Duration) time.Duration { return budget }
