//go:build nosytest

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"github.com/muxover/nosy/internal/browser"
	"github.com/muxover/nosy/internal/guard"
)

// sharedBrowser starts a browser that stays up and is used by every scan of a
// test, each in a browser context of its own.
func sharedBrowser(t *testing.T) *browser.Shared {
	t.Helper()
	need(t)
	p, err := guard.StartProxy(&guard.Dialer{}, guard.MaxBytes, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	b, err := browser.StartShared(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Stop)
	return b
}

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

// visitRemote visits u in the shared browser and returns the capture.
func visitRemote(t *testing.T, remote, u, token string) string {
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
	c, err := browser.Visit(ctx, target, browser.Options{Proxy: proxy, Remote: remote, Token: token, Verbose: t.Logf})
	if err != nil {
		t.Fatalf("visit: %v", err)
	}
	return c.Page.Text
}

// probe visits the leak page in the shared browser and reads what the page saw.
func probe(t *testing.T, b *browser.Shared) probeResult {
	t.Helper()
	text := visitRemote(t, b.Address, "https://leak.test/", "nosyprobe")
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

// openContexts lists the browser contexts that exist besides the default one.
func openContexts(t *testing.T, address string) []cdp.BrowserContextID {
	t.Helper()
	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(context.Background(), address)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	var ids []cdp.BrowserContextID
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		ids, _, err = target.GetBrowserContexts().Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
		return err
	}))
	if err != nil {
		t.Fatalf("list browser contexts: %v", err)
	}
	return ids
}

func TestSharedBrowserLeaksNothingBetweenScans(t *testing.T) {
	t.Parallel()
	b := sharedBrowser(t)
	asked := fixture.Count("leak.cached")
	const scans = 4
	for i := 1; i <= scans; i++ {
		r := probe(t, b)
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
		if ids := openContexts(t, b.Address); len(ids) != 0 {
			t.Errorf("scan %d left %d browser context(s) open", i, len(ids))
		}
	}
	if n := b.Starts(); n != 1 {
		t.Errorf("the browser was started %d times for %d scans, want once", n, scans)
	}
}

// signature is everything about a scan that the engine itself decides: the
// companies, the cookies with their kinds, what fingerprinted, what captured
// typed text, and the score.
func signature(r *outcome) string {
	var parts []string
	for _, c := range r.Parties.Companies {
		parts = append(parts, fmt.Sprintf("company %s tracking=%v", c.Name, c.Tracking))
	}
	for _, c := range r.Cookies {
		parts = append(parts, fmt.Sprintf("cookie %s %s third=%v ident=%v", c.Name, c.Domain, c.ThirdParty, c.Identifier))
	}
	for _, f := range r.Techniques {
		parts = append(parts, fmt.Sprintf("fingerprint %s %v", f.Name, f.Scripts))
	}
	for _, h := range r.KeyHits {
		parts = append(parts, fmt.Sprintf("key %s third=%v", h.URL, h.ThirdParty))
	}
	parts = append(parts, fmt.Sprintf("fields %d recording %d status %s", r.FieldsTested, len(r.Recording), r.Status))
	if r.Score != nil {
		parts = append(parts, fmt.Sprintf("score %d", r.Score.Total))
	}
	sort.Strings(parts)
	return "    " + strings.Join(parts, "\n    ")
}

func TestSharedBrowserKeepsScansApart(t *testing.T) {
	t.Parallel()
	need(t)
	sites := []string{
		"http://trackers.test/", "http://control.test/", "http://worker.test/", "https://sw.test/", "http://frames.test/",
		"http://cookies.test/", "http://keyinput.test/", "http://canvas.test/", "http://replay.test/",
	}
	// each site in a browser of its own, then all of them beside each other in one
	scanAll := func(remote string) []string {
		sigs := make([]string, len(sites))
		var wg sync.WaitGroup
		for i := range sites {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, err := runGated(sites[i], config{remote: remote})
				if err != nil {
					t.Errorf("scan %s: %v", sites[i], err)
					return
				}
				sigs[i] = signature(r)
			}()
		}
		wg.Wait()
		return sigs
	}
	alone := scanAll("")
	b := sharedBrowser(t)
	together := scanAll(b.Address)
	for i, s := range sites {
		if together[i] != alone[i] {
			t.Errorf("%s scanned beside the others in one browser:\n  got\n%s\n  want\n%s", s, together[i], alone[i])
		}
	}
	if ids := openContexts(t, b.Address); len(ids) != 0 {
		t.Errorf("%d browser context(s) left open", len(ids))
	}
}

func TestChainsAndLoops(t *testing.T) {
	t.Parallel()
	need(t)
	hits := func(host string) int { return len(fixture.Hits(host)) }
	chain := "n200.hops.test"

	t.Run("a loop", func(t *testing.T) {
		t.Parallel()
		before := hits("loop.test")
		r := scanTest(t, "http://loop.test/a")
		if r.Status != statusFailed || !strings.Contains(r.Reason, "too many times") {
			t.Errorf("status %s: %s", r.Status, r.Reason)
		}
		if n := hits("loop.test") - before; n > guard.MaxRedirects+3 {
			t.Errorf("a loop got %d requests, the limit is %d redirects (the browser's own is 20)", n, guard.MaxRedirects)
		}
	})
	t.Run("a long chain", func(t *testing.T) {
		t.Parallel()
		before := hits(chain)
		r := scanTest(t, "http://n200.hops.test/")
		if r.Status != statusFailed || !strings.Contains(r.Reason, "too many times") {
			t.Errorf("status %s: %s", r.Status, r.Reason)
		}
		if n := hits(chain) - before; n > guard.MaxRedirects+3 {
			t.Errorf("a chain of 200 got %d requests, the limit is %d redirects (the browser's own is 20)", n, guard.MaxRedirects)
		}
	})
	t.Run("a chain just inside the limit", func(t *testing.T) {
		t.Parallel()
		r := scanTest(t, fmt.Sprintf("http://n%d.hops.test/", guard.MaxRedirects))
		if r.Status != statusDone {
			t.Errorf("status %s: %s", r.Status, r.Reason)
		}
	})
	t.Run("a chain one past the limit", func(t *testing.T) {
		t.Parallel()
		r := scanTest(t, fmt.Sprintf("http://n%d.hops.test/", guard.MaxRedirects+1))
		if r.Status != statusFailed || !strings.Contains(r.Reason, "too many times") {
			t.Errorf("status %s: %s", r.Status, r.Reason)
		}
	})
	t.Run("a chain across sites", func(t *testing.T) {
		t.Parallel()
		r := scanTest(t, "http://hopa.test/")
		if r.Status != statusDone || r.Site != "preview.test" {
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
		if r.Status != statusFailed || !strings.Contains(r.Reason, "private or local") {
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
		if r.Status != statusDone {
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

// Every kind of fingerprinting the engine credits is still credited when the
// page runs in a context of its own in a browser that stays up.
func TestFingerprintPlantsInASharedBrowser(t *testing.T) {
	t.Parallel()
	b := sharedBrowser(t)
	cases := []struct{ host, technique string }{
		{"canvas.test", "canvas"}, {"webgl.test", "webgl"}, {"audio.test", "audio"},
		{"fonts.test", "fonts"}, {"fontsdom.test", "fonts"}, {"frames.test", "canvas"},
	}
	for _, c := range cases {
		t.Run(c.host, func(t *testing.T) {
			t.Parallel()
			r := scanTest(t, "http://"+c.host+"/", func(cf *config) { cf.remote = b.Address })
			if got := techniques(r); len(got[c.technique]) == 0 {
				t.Errorf("%s: %s was not credited, got %v", c.host, c.technique, got)
			}
		})
	}
}

// A visitor's page is shown and has focus. The scan's must be the same, in a
// browser that stays up and in one started for the scan.
func TestThePageIsShownAndFocused(t *testing.T) {
	t.Parallel()
	b := sharedBrowser(t)
	for name, remote := range map[string]string{"a browser that stays up": b.Address, "a browser for one scan": ""} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			text := visitRemote(t, remote, "http://state.test/", "nosystate")
			_, raw, ok := strings.Cut(text, "STATE ")
			if !ok {
				t.Fatalf("the probe page did not run: %q", text[min(len(text), 900):])
			}
			seen, _, _ := strings.Cut(raw, "\n")
			if !strings.Contains(seen, `"visible":"visible"`) || !strings.Contains(seen, `"focused":true`) {
				t.Errorf("the page saw %s, want it shown and focused", strings.TrimSpace(seen))
			}
		})
	}
}
