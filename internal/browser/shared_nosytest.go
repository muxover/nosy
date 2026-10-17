//go:build nosytest

package browser

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/chromedp/chromedp"

	"github.com/muxover/nosy/internal/guard"
)

// Shared is a browser that stays up between visits: one Chromium with the
// scanner's flags and a fresh profile, listening for DevTools on a port. A
// visit that sets Options.Remote to its Address gets a browser context of its
// own in it. Test builds only.
type Shared struct {
	Address string // DevTools address

	mu     sync.Mutex
	cmd    *exec.Cmd
	cancel func()
	starts int
}

// StartShared starts the browser and keeps it running until Stop.
func StartShared(p *guard.Proxy) (*Shared, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	profile, err := os.MkdirTemp("", "nosy-shared-")
	if err != nil {
		return nil, err
	}
	if err := writePreferences(profile); err != nil {
		os.RemoveAll(profile)
		return nil, err
	}
	s := &Shared{Address: fmt.Sprintf("http://127.0.0.1:%d", port)}
	opts := allocatorOptions(profile, port, Options{Proxy: p})
	opts = append(opts, chromedp.ModifyCmdFunc(func(c *exec.Cmd) {
		confine(c)
		s.cmd = c
	}))
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	bctx, cancelCtx := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(bctx); err != nil {
		cancelCtx()
		cancelAlloc()
		os.RemoveAll(profile)
		return nil, err
	}
	s.starts++
	s.cancel = func() {
		cancelCtx()
		cancelAlloc()
		killTree(s.cmd)
		os.RemoveAll(profile)
	}
	return s, nil
}

// Starts is how many times the browser has been started.
func (s *Shared) Starts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.starts
}

// Stop ends the browser for good.
func (s *Shared) Stop() { s.cancel() }
