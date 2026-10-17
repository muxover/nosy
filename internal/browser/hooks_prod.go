//go:build !nosytest

package browser

import "github.com/chromedp/chromedp"

func testFlags() []chromedp.ExecAllocatorOption { return nil }
