//go:build nosytest

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/muxover/nosy/internal/browser"
	"github.com/muxover/nosy/internal/guard"
	"github.com/muxover/nosy/internal/scan"
)

type probeSeen struct {
	Cookie, Local, Session, IDB, Caches, SW, Controlled, CachedFile string
}

type probeResult struct{ First, Second probeSeen }

func (p *probeSeen) UnmarshalJSON(b []byte) error {
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*p = probeSeen{m["cookie"], m["local"], m["session"], m["idb"], m["caches"], m["sw"], m["controlled"], m["cachedfile"]}
	return nil
}

// visitFresh visits u in a browser started for the visit and returns the page's text.
func visitFresh(t *testing.T, u, token string) string {
	t.Helper()
	gate <- struct{}{}
	defer func() { <-gate }()
	proxy, err := guard.StartProxy(&guard.Dialer{}, guard.MaxBytes, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	target, _ := url.Parse(u)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := browser.Visit(ctx, target, browser.Options{Proxy: proxy, Token: token, Verbose: t.Logf})
	if err != nil {
		t.Fatalf("visit: %v", err)
	}
	return c.Page.Text
}

// probe visits the leak page and reads what the page saw.
func probe(t *testing.T) probeResult {
	t.Helper()
	text := visitFresh(t, "https://leak.test/", "nosyprobe")
	_, raw, ok := strings.Cut(text, "PROBE ")
	if !ok {
		t.Fatalf("the probe page did not finish: %q", text[min(len(text), 900):])
	}
	var r probeResult
	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	if err := dec.Decode(&struct {
		First  *probeSeen `json:"first"`
		Second *probeSeen `json:"second"`
	}{&r.First, &r.Second}); err != nil {
		t.Fatalf("probe result %q: %v", raw, err)
	}
	return r
}

func TestTwoScansShareNothing(t *testing.T) {
	t.Parallel()
	need(t)
	asked := fixture.Count("leak.cached")
	const scans = 4
	for i := 1; i <= scans; i++ {
		r := probe(t)
		// what the scan finds when it opens the page: nothing from any scan before it
		empty := probeSeen{Controlled: "no", CachedFile: r.First.CachedFile}
		if r.First != empty {
			t.Fatalf("scan %d found leftovers of an earlier scan: %+v", i, r.First)
		}
		// and the probe does keep state inside one scan, so finding none is meaningful
		s := r.Second
		if !strings.Contains(s.Cookie, "leak=1") || s.Local != "leak" || s.Session != "leak" || s.IDB != "leakdb" ||
			s.Caches != "leakcache" || !strings.Contains(s.SW, "https://leak.test") || s.Controlled != "yes" {
			t.Fatalf("scan %d: the probe could not keep its own state, so it proves nothing: %+v", i, s)
		}
		// the HTTP cache works inside the scan: the reload read the file it had fetched
		if r.First.CachedFile != s.CachedFile {
			t.Errorf("scan %d: the cached file changed inside one scan: %q then %q", i, r.First.CachedFile, s.CachedFile)
		}
		// and not across scans: every scan asked the server for it again
		if got := fixture.Count("leak.cached") - asked; got != i {
			t.Fatalf("after %d scans the server was asked for the file %d times: a later scan was served from the cache of an earlier one", i, got)
		}
	}
}

// The visit ends when the eleventh redirect is reported, but the browser follows
// a chain as fast as the server answers: on a slow machine it has asked for more
// by then. It never goes past its own limit of 20.
const maxFollowed = 21

func TestChainsAndLoops(t *testing.T) {
	t.Parallel()
	need(t)
	hits := func(host string) int { return len(fixture.Hits(host)) }
	chain := "n200.hops.test"

	t.Run("a loop", func(t *testing.T) {
		t.Parallel()
		before := hits("loop.test")
		r := scanTest(t, "http://loop.test/a")
		if r.Status != scan.StatusFailed || !strings.Contains(r.Reason, "too many times") {
			t.Errorf("status %s: %s", r.Status, r.Reason)
		}
		if n := hits("loop.test") - before; n > maxFollowed {
			t.Errorf("a loop got %d requests, the limit is %d redirects (the browser's own is 20)", n, guard.MaxRedirects)
		}
	})
	t.Run("a long chain", func(t *testing.T) {
		t.Parallel()
		before := hits(chain)
		r := scanTest(t, "http://n200.hops.test/")
		if r.Status != scan.StatusFailed || !strings.Contains(r.Reason, "too many times") {
			t.Errorf("status %s: %s", r.Status, r.Reason)
		}
		if n := hits(chain) - before; n > maxFollowed {
			t.Errorf("a chain of 200 got %d requests, the limit is %d redirects (the browser's own is 20)", n, guard.MaxRedirects)
		}
	})
	t.Run("a chain just inside the limit", func(t *testing.T) {
		t.Parallel()
		r := scanTest(t, fmt.Sprintf("http://n%d.hops.test/", guard.MaxRedirects))
		if r.Status != scan.StatusDone {
			t.Errorf("status %s: %s", r.Status, r.Reason)
		}
	})
	t.Run("a chain one past the limit", func(t *testing.T) {
		t.Parallel()
		r := scanTest(t, fmt.Sprintf("http://n%d.hops.test/", guard.MaxRedirects+1))
		if r.Status != scan.StatusFailed || !strings.Contains(r.Reason, "too many times") {
			t.Errorf("status %s: %s", r.Status, r.Reason)
		}
	})
	t.Run("a chain across sites", func(t *testing.T) {
		t.Parallel()
		r := scanTest(t, "http://hopa.test/")
		if r.Status != scan.StatusDone || r.Site != "preview.test" {
			t.Errorf("status %s site %s: %s", r.Status, r.Site, r.Reason)
		}
		for _, h := range []string{"hopa.test", "hopb.test", "hopc.test"} {
			if hits(h) == 0 {
				t.Errorf("%s was not visited", h)
			}
		}
	})
	t.Run("a chain that ends at a private address", func(t *testing.T) {
		t.Parallel()
		r := scanTest(t, "http://hopbad.test/")
		if r.Status != scan.StatusFailed || !strings.Contains(r.Reason, "private or local") {
			t.Errorf("status %s: %s", r.Status, r.Reason)
		}
		if hits("hopbad2.test") == 0 {
			t.Error("the chain never reached its last public hop")
		}
		if fixture.HopCanary != nil && fixture.HopCanary.Conns() != 0 {
			t.Errorf("the scanner connected to the end of the chain")
		}
	})
}

// Chromium stops a chain of redirects on a file the page asked for at 20. Every
// hop of it is a request the browser makes through the guarded proxy: the hops
// are in the capture, the fixture saw each of them, and a hop to a private
// address is turned away.
func TestRedirectsOfFilesStillGoThroughTheGuard(t *testing.T) {
	t.Parallel()
	need(t)
	t.Run("a long chain", func(t *testing.T) {
		t.Parallel()
		before := len(fixture.Hits("hopimg.test"))
		r := scanTest(t, "http://imgchain.test/")
		hits := len(fixture.Hits("hopimg.test")) - before
		if hits < 2 || hits > 22 {
			t.Errorf("the chain of 60 got %d requests, the browser's own limit is 20", hits)
		}
		inCapture := 0
		for _, q := range r.Requests {
			if q.Host == "hopimg.test" {
				inCapture++
			}
		}
		if inCapture != hits {
			t.Errorf("%d hops reached the site and %d are in the capture", hits, inCapture)
		}
		// the page itself is not harmed by a file that never loads
		if r.Status != scan.StatusDone {
			t.Errorf("status %s: %s", r.Status, r.Reason)
		}
	})
	t.Run("a chain that ends at a private address", func(t *testing.T) {
		t.Parallel()
		r := scanTest(t, "http://imgbad.test/")
		if fixture.HopCanary != nil && fixture.HopCanary.Conns() != 0 {
			t.Error("the scanner connected to the end of the chain")
		}
		if !hasNote(r, "safety guard refused") {
			t.Errorf("the refused hop is not in the capture: %v", r.Notes)
		}
		for _, h := range []string{"hopimgbad.test", "hopimgbad2.test"} {
			if len(fixture.Hits(h)) == 0 {
				t.Errorf("%s was not visited", h)
			}
		}
	})
}

// A visitor's page is shown and has focus. The scan's must be the same.
func TestThePageIsShownAndFocused(t *testing.T) {
	t.Parallel()
	text := visitFresh(t, "http://state.test/", "nosystate")
	_, raw, ok := strings.Cut(text, "STATE ")
	if !ok {
		t.Fatalf("the probe page did not run: %q", text[min(len(text), 900):])
	}
	seen, _, _ := strings.Cut(raw, "\n")
	if !strings.Contains(seen, `"visible":"visible"`) || !strings.Contains(seen, `"focused":true`) {
		t.Errorf("the page saw %s, want it shown and focused", strings.TrimSpace(seen))
	}
}
