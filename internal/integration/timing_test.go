//go:build nosytest && !windows

package integration

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

// TestTimingOfOneRealScan scans the public page in NOSY_TIMING_URL once, alone,
// and logs the wall time and the CPU seconds it cost, for comparison with the
// previous engine. It makes the same requests a visitor's browser would for
// that page's home page and nothing else.
func TestTimingOfOneRealScan(t *testing.T) {
	need(t)
	target := os.Getenv("NOSY_TIMING_URL")
	if target == "" {
		t.Skip("set NOSY_TIMING_URL to a public page to time one scan of it")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	self0 := selfCPU()
	stop, done := sampleBrowserCPU()
	start := time.Now()
	o, err := run(ctx, target, config{verbose: t.Logf})
	wall := time.Since(start)
	stop()
	browserCPU, measured := <-done, true
	if browserCPU < 0 {
		browserCPU, measured = 0, false
	}
	if err != nil {
		t.Fatalf("scan %s: %v", target, err)
	}
	engine := selfCPU() - self0

	t.Logf("%s: status %s, final %s, %d requests, %d third-party companies", target, o.Status, o.FinalURL, len(o.Requests), len(o.Parties.Companies))
	t.Logf("wall time %.1f s (the visit itself: %.1f s)", wall.Seconds(), o.Capture.Page.Duration.Seconds())
	if measured {
		t.Logf("CPU: browser %.1f s + engine and test server %.1f s = %.1f CPU-s", browserCPU, engine, browserCPU+engine)
	} else {
		t.Logf("CPU: engine and test server %.1f s; the browser's CPU is measured on Linux only", engine)
	}
}

// selfCPU is the CPU seconds this process has used so far.
func selfCPU() float64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return float64(ru.Utime.Sec+ru.Stime.Sec) + float64(ru.Utime.Usec+ru.Stime.Usec)/1e6
}
