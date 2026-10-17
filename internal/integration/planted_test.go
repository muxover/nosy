//go:build nosytest

package integration

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/muxover/nosy/internal/guard"
)

func TestControlStaysSilent(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://control.test/")
	if r.Status != statusDone {
		t.Fatalf("status %s: %s", r.Status, r.Reason)
	}
	if r.Score.Total != 0 || r.Score.Label != "Not nosy" {
		t.Errorf("score = %d %s, want 0 Not nosy; parts %+v", r.Score.Total, r.Score.Label, r.Score.Parts)
	}
	if len(r.Techniques) != 0 {
		t.Errorf("fingerprinting on the control: %+v", r.Techniques)
	}
	if len(r.Recording) != 0 || len(r.KeyHits) != 0 {
		t.Errorf("replay %v / key capture %v on the control", r.Recording, r.KeyHits)
	}
	if r.FieldsTested < 1 {
		t.Error("the control has a search field and it was not typed into")
	}
	for _, c := range r.Cookies {
		if c.Identifier {
			t.Errorf("identifier cookie on the control: %+v", c)
		}
	}
	if len(r.Cookies) != 2 {
		t.Errorf("cookies = %+v, want sid and theme", r.Cookies)
	}
	if found, tracking := company(r, "CDN Co"); !found || tracking {
		t.Errorf("CDN listed = %v tracking = %v, want listed and not tracking", found, tracking)
	}
	if found, _ := company(r, "assets.ownco.test"); found {
		t.Error("a domain owned by the site's own company was listed as a third party")
	}
	if !strings.Contains(r.Finding, "no tracking companies") {
		t.Errorf("finding = %q", r.Finding)
	}
}

func TestFingerprintPlants(t *testing.T) {
	cases := []struct{ host, technique, script string }{
		{"canvas.test", "canvas", "http://fpvendor.test/canvas.js"},
		{"webgl.test", "webgl", "http://fpvendor.test/webgl.js"},
		{"audio.test", "audio", "http://fpvendor.test/audio.js"},
		{"fonts.test", "fonts", "http://fpvendor.test/fonts.js"},
		{"fontsdom.test", "fonts", "http://fpvendor.test/fontsdom.js"},
	}
	for _, c := range cases {
		t.Run(c.host, func(t *testing.T) {
			t.Parallel()
			r := scanTest(t, "http://"+c.host+"/")
			got := techniques(r)
			if len(got) != 1 {
				t.Fatalf("techniques = %v, want only %s", got, c.technique)
			}
			scripts, ok := got[c.technique]
			if !ok || len(scripts) != 1 || scripts[0] != c.script {
				t.Errorf("%s credited to %v, want %s", c.technique, scripts, c.script)
			}
			if r.Score.Parts.Fingerprinting != 5 {
				t.Errorf("fingerprinting points = %v, want 5", r.Score.Parts.Fingerprinting)
			}
			if !strings.Contains(r.Finding, "fingerprinting") {
				t.Errorf("finding = %q", r.Finding)
			}
		})
	}
}

func TestFingerprintInsideIframe(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://frames.test/")
	if s := techniques(r)["canvas"]; len(s) != 1 || s[0] != "http://fpvendor.test/canvas.js" {
		t.Errorf("canvas from the iframe = %v", s)
	}
	if found, tracking := company(r, "AdNet"); !found || !tracking {
		t.Errorf("the tracker requested from inside the iframe was not seen: %+v", r.Parties.Companies)
	}
}

func TestKeyCapturePlants(t *testing.T) {
	for _, host := range []string{"keyinput.test", "keydown.test", "keyb64.test"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			r := scanTest(t, "http://"+host+"/")
			if r.FieldsTested < 2 {
				t.Errorf("fields tested = %d, want 2", r.FieldsTested)
			}
			if len(r.KeyHits) == 0 {
				t.Fatalf("typed text was sent to collector.test and not caught; requests: %s", requestURLs(r))
			}
			for _, h := range r.KeyHits {
				if !h.ThirdParty || !strings.Contains(h.URL, "collector.test") {
					t.Errorf("hit = %+v, want a third-party request to collector.test", h)
				}
			}
			if r.Score.Parts.KeyCapture != 10 {
				t.Errorf("key capture points = %v, want 10", r.Score.Parts.KeyCapture)
			}
			if !strings.Contains(r.Finding, "typed into a form") {
				t.Errorf("finding = %q", r.Finding)
			}
		})
	}
	t.Run("keysubmit.test", func(t *testing.T) {
		t.Parallel()
		r := scanTest(t, "http://keysubmit.test/")
		if r.FieldsTested < 2 || len(r.KeyHits) != 0 {
			t.Errorf("fields %d hits %+v, want typed into fields and nothing sent before submit", r.FieldsTested, r.KeyHits)
		}
	})
}

func TestSessionReplayPlant(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://replay.test/")
	if len(r.Recording) != 1 || !strings.Contains(r.Requests[r.Recording[0]].URL, "replayvendor.test/recorder.js") {
		t.Errorf("session recording = %+v", r.Recording)
	}
	if r.Score.Parts.Recording != 10 || !strings.Contains(r.Finding, "session recording") {
		t.Errorf("recording points %v, finding %q", r.Score.Parts.Recording, r.Finding)
	}
	// the analytics script on the same page is not a replay script
	if found, tracking := company(r, "Stats"); !found || !tracking {
		t.Error("analytics company missing")
	}
}

func TestTrackersScoreByHand(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://trackers.test/")
	want := map[string]bool{"AdNet": true, "Stats": true, "Social": true, "CDN Co": false, "unlisted.test": false}
	for name, tracking := range want {
		if found, got := company(r, name); !found || got != tracking {
			t.Errorf("%s: listed %v tracking %v, want tracking %v", name, found, got, tracking)
		}
	}
	if found, _ := company(r, "assets.ownco.test"); found {
		t.Error("own-company domain listed as third party")
	}
	// 3 tracking companies: 40 x log2(4) / log2(33) = 15.86, plus 10 for ad tech = 25.86
	if r.Score.Total != 26 || r.Score.Label != "A little nosy" {
		t.Errorf("score = %d %s, parts %+v", r.Score.Total, r.Score.Label, r.Score.Parts)
	}
	if r.Finding != "trackers.test contacted 3 tracking companies before you clicked anything" {
		t.Errorf("finding = %q", r.Finding)
	}
}

func TestIdentifierCookiePlant(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://cookies.test/")
	ids := map[string]cookie{}
	for _, c := range r.Cookies {
		if c.Identifier {
			ids[c.Name] = c
		}
	}
	for _, name := range []string{"uid", "_ga", "vid", "_fbp", "tid"} {
		if _, ok := ids[name]; !ok {
			t.Errorf("%s not counted as identifier; cookies: %+v %s", name, r.Cookies, requestURLs(r))
		}
	}
	if len(ids) != 5 {
		t.Errorf("identifier cookies = %d, want 5: %v", len(ids), ids)
	}
	if !ids["tid"].ThirdParty || ids["uid"].ThirdParty {
		t.Errorf("party wrong: tid %+v uid %+v", ids["tid"], ids["uid"])
	}
	if !strings.Contains(ids["uid"].SetBy, "cookies.test") || !strings.Contains(ids["_fbp"].SetBy, "cookies.test") {
		t.Errorf("setters: uid %q _fbp %q", ids["uid"].SetBy, ids["_fbp"].SetBy)
	}
	// 15 x log2(1 + 5) / 5 = 7.75
	if p := r.Score.Parts.Cookies; p < 7.74 || p > 7.76 {
		t.Errorf("cookie points = %v, want 7.75", p)
	}
}

// the consent API is seen; consent platforms found by technology rules are
// not ported yet
func TestConsentAPI(t *testing.T) {
	t.Parallel()
	c := scanTest(t, "http://consent.test/")
	if !c.Consent.Present || len(c.Consent.Via) != 1 || c.Consent.Via[0] != "__tcfapi" {
		t.Errorf("consent = %+v, want __tcfapi", c.Consent)
	}
	if r := scanTest(t, "http://tech.test/"); r.Consent.Present {
		t.Errorf("consent reported on a page without one: %+v", r.Consent)
	}
}

func TestBlockedAndFailedPages(t *testing.T) {
	blocked := map[string]string{
		"challenge.test": "bot check",
		"error.test":     "error page",
		"notfound.test":  "error page",
		"empty.test":     "empty",
	}
	for host, why := range blocked {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			r := scanTest(t, "http://"+host+"/")
			if r.Status != statusBlocked || !strings.Contains(r.Reason, why) {
				t.Errorf("status %s reason %q, want blocked: %s", r.Status, r.Reason, why)
			}
			if r.Score != nil || r.Finding != "" {
				t.Errorf("a blocked scan has a score: %+v %q", r.Score, r.Finding)
			}
		})
	}
	t.Run("down.test", func(t *testing.T) {
		t.Parallel()
		r := scanTest(t, "http://down.test/")
		if r.Status != statusFailed || r.Score != nil {
			t.Errorf("status %s score %+v, want failed without a score", r.Status, r.Score)
		}
	})
}

func TestRedirectLandsOnFinalSite(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://redirect.test/")
	if r.FinalURL != "http://final.test/" || r.Site != "final.test" {
		t.Errorf("final URL %q site %q", r.FinalURL, r.Site)
	}
	if !strings.HasPrefix(r.Finding, "final.test ") {
		t.Errorf("finding = %q", r.Finding)
	}
}

func TestNeverQuietPageFinishesInTime(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://slow.test/")
	if d := r.Capture.Page.Duration; d > 20*time.Second {
		t.Errorf("scan took %s", d)
	}
	if !r.Capture.Page.NeverQuiet || !hasNote(r, "did not go quiet") {
		t.Errorf("never quiet = %v, notes = %v", r.Capture.Page.NeverQuiet, r.Notes)
	}
	if r.Status != statusDone {
		t.Errorf("status %s: %s", r.Status, r.Reason)
	}
}

func TestSizeLimit(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://big.test/", func(c *config) { c.maxBytes = 3 << 20 })
	if !hasNote(r, "download size limit") {
		t.Errorf("notes = %v", r.Notes)
	}
	if r.Status == statusDone && r.Bytes > 4<<20 {
		t.Errorf("downloaded %d bytes against a 3 MB limit", r.Bytes)
	}
}

// not parallel: it looks for browser processes and profiles left behind, which
// the other tests' browsers would be mistaken for
func TestHardStopKillsEverything(t *testing.T) {
	start := time.Now()
	r := scanTest(t, "http://hang.test/")
	if d := time.Since(start); d > 24*time.Second {
		t.Errorf("scan took %s, the hard stop is 20 s", d)
	}
	if r.Status != statusFailed || !strings.Contains(r.Reason, "20 seconds") {
		t.Errorf("status %s reason %q", r.Status, r.Reason)
	}
	time.Sleep(500 * time.Millisecond)
	if runtime.GOOS == "linux" {
		if left := browserProcesses(); len(left) > 0 {
			t.Errorf("browser processes left behind: %v", left)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(os.TempDir(), "nosy-profile-*")); len(left) > 0 {
		t.Errorf("browser profiles left behind: %v", left)
	}
}

// browserProcesses lists processes started with a scan profile.
func browserProcesses() []string {
	var out []string
	files, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), "nosy-profile-") {
			out = append(out, strings.ReplaceAll(strings.TrimRight(string(b), "\x00"), "\x00", " "))
		}
	}
	return out
}

// a site that resolves to a private address is refused before a browser starts
func TestRefusesBeforeScanning(t *testing.T) {
	t.Parallel()
	need(t)
	inputs := []string{
		"http://127.0.0.1/", "http://localhost/", "http://[::1]/", "http://10.0.0.1/", "http://169.254.169.254/latest/meta-data/",
		"http://[fd00:ec2::254]/", "http://[::ffff:10.0.0.1]/", "http://192.168.1.1/", "http://100.64.0.1/",
		"http://rebind.example/", "http://metadata.example/", "http://v6.example/", "http://mixed.example/",
	}
	for _, in := range inputs {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		o, err := run(ctx, in, config{})
		cancel()
		if err == nil {
			t.Errorf("%s was scanned: %+v", in, o.Status)
		}
	}
}

func TestPrivateAddressesNeverReached(t *testing.T) {
	t.Parallel()
	need(t)
	before, beforePort := fixture.Canary.Conns(), int64(0)
	if fixture.CanaryPort != nil {
		beforePort = fixture.CanaryPort.Conns()
	}
	r := scanTest(t, "http://ssrf.test/")
	if r.Status != statusDone {
		t.Fatalf("status %s: %s", r.Status, r.Reason)
	}
	if n := fixture.Canary.Conns() - before; n != 0 {
		t.Errorf("the loopback canary was reached %d times", n)
	}
	if fixture.CanaryPort != nil {
		if n := fixture.CanaryPort.Conns() - beforePort; n != 0 {
			t.Errorf("the loopback port 80 canary was reached %d times", n)
		}
	} else {
		t.Log("loopback port 80 is not available here, that canary was skipped")
	}
	for _, q := range r.Requests {
		if strings.HasPrefix(q.URL, "data:") || strings.Contains(q.Host, "ssrf.test") {
			continue
		}
		if q.Status >= 200 && q.Status < 400 && !q.Refused {
			t.Errorf("a private destination answered: %s %d", q.URL, q.Status)
		}
	}
	if !hasNote(r, "safety guard refused") {
		t.Errorf("notes = %v", r.Notes)
	}
	for _, blocked := range []string{"127.0.0.1", "169.254.169.254", "10.0.0.5", "metadata.google.internal"} {
		found := false
		for _, n := range r.Notes {
			if strings.Contains(n, blocked) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is not in the refused list: %v", blocked, r.Notes)
		}
	}
}

func TestRedirectsToPrivateAddressAreDropped(t *testing.T) {
	for _, host := range []string{"topredir.test", "topredir6.test", "topmeta.test"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			need(t)
			var before int64
			if fixture.CanaryPort != nil {
				before = fixture.CanaryPort.Conns()
			}
			r := scanTest(t, "http://"+host+"/")
			if r.Status != statusFailed || !strings.Contains(r.Reason, "private or local") {
				t.Errorf("status %s reason %q, want failed because the redirect went to a private address", r.Status, r.Reason)
			}
			if fixture.CanaryPort != nil && fixture.CanaryPort.Conns() != before {
				t.Error("canary reached")
			}
		})
	}
	t.Run("subredir.test", func(t *testing.T) {
		t.Parallel()
		r := scanTest(t, "http://subredir.test/")
		if r.Status != statusDone || !hasNote(r, "169.254.169.254") {
			t.Errorf("status %s notes %v", r.Status, r.Notes)
		}
		for _, q := range r.Requests {
			if q.Host == "169.254.169.254" && q.Status == 200 {
				t.Errorf("metadata address answered: %+v", q)
			}
		}
	})
}

// with the guard off the canary must be reachable, or the tests above prove
// nothing
func TestCanaryPositiveControl(t *testing.T) {
	need(t)
	if fixture.CanaryPort == nil {
		t.Skip("loopback port 80 is not available here")
	}
	gate <- struct{}{}
	defer func() { <-gate }()
	guard.DisableGuard(true)
	defer guard.DisableGuard(false)
	before := fixture.CanaryPort.Conns()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := run(ctx, "http://ssrf.test/", config{dialer: &guard.Dialer{}}); err != nil {
		t.Fatal(err)
	}
	if fixture.CanaryPort.Conns() == before {
		t.Error("with the guard off, the canary was still never reached: the safety tests cannot fail")
	}
}

func TestRepeatScansAgree(t *testing.T) {
	t.Parallel()
	need(t)
	scores := make([]int, 3)
	var wg sync.WaitGroup
	for i := range scores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := runGated("http://trackers.test/", config{})
			if err != nil {
				t.Errorf("scan: %v", err)
				return
			}
			scores[i] = r.Score.Total
		}()
	}
	wg.Wait()
	for _, s := range scores {
		if s != scores[0] {
			t.Errorf("scores differ between runs: %v", scores)
		}
	}
}

// a server that answers a script request with JSON is still contacted, even
// though the browser blocks the page from reading the answer
func TestAnswerBlockedByBrowserStillCounts(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://orb.test/")
	var blocked bool
	for _, q := range r.Requests {
		if strings.HasSuffix(q.URL, "/data.json") && q.Answered && q.Failed != "" {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("the JSON request was not blocked by the browser as expected:\n%s", requestURLs(r))
	}
	if found, tracking := company(r, "AdNet"); !found || !tracking {
		t.Errorf("the company that answered was not counted: %+v", r.Parties.Companies)
	}
}

// traffic from a web worker is part of what the page does
func TestWorkerRequestsAreCaptured(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://worker.test/")
	for _, name := range []string{"AdNet", "Stats"} {
		if found, tracking := company(r, name); !found || !tracking {
			t.Errorf("%s requested from the worker was not counted:\n%s", name, requestURLs(r))
		}
	}
}

func TestSharedAndServiceWorkerRequestsAreCaptured(t *testing.T) {
	for _, in := range []string{"http://shared.test/", "https://sw.test/"} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			r := scanTest(t, in)
			for _, name := range []string{"AdNet", "Stats"} {
				if found, tracking := company(r, name); !found || !tracking {
					t.Errorf("%s requested from the worker was not counted:\n%s", name, requestURLs(r))
				}
			}
		})
	}
}

// the site's own search suggestions receive the typed text; that is listed
// but not scored
func TestOwnSearchSuggestionsAreNotScored(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://keyfirst.test/")
	if len(r.KeyHits) == 0 || r.KeyHits[0].ThirdParty {
		t.Fatalf("hits = %+v, want a first-party hit", r.KeyHits)
	}
	if r.Score.Parts.KeyCapture != 0 || r.Score.Total != 0 {
		t.Errorf("score %d parts %+v, want 0", r.Score.Total, r.Score.Parts)
	}
}

func TestReplayListHitAddonAndLabel(t *testing.T) {
	t.Run("recorder", func(t *testing.T) {
		t.Parallel()
		r := scanTest(t, "http://replay.test/")
		if len(r.Recording) != 1 || r.Score.Parts.Recording != 10 {
			t.Errorf("recording %+v points %v", r.Recording, r.Score.Parts.Recording)
		}
	})
	t.Run("add-on main tag without the recorder", func(t *testing.T) {
		t.Parallel()
		r := scanTest(t, "http://addonmain.test/")
		if len(r.Recording) != 0 || r.Score.Parts.Recording != 0 {
			t.Errorf("the add-on's main tag counted: %+v", r.Recording)
		}
	})
	t.Run("add-on recorder", func(t *testing.T) {
		t.Parallel()
		r := scanTest(t, "http://addonrec.test/")
		if len(r.Recording) != 1 || !strings.HasSuffix(r.Requests[r.Recording[0]].URL, "/recorder.js") {
			t.Errorf("recording %+v", r.Recording)
		}
	})
	t.Run("replay label on a shared CDN hostname", func(t *testing.T) {
		t.Parallel()
		r := scanTest(t, "http://labelled.test/")
		if len(r.Recording) != 0 || r.Score.Total != 0 {
			t.Errorf("score %d recording %+v, want 0 and none", r.Score.Total, r.Recording)
		}
		if found, tracking := company(r, "Shared Hosting"); !found || tracking {
			t.Errorf("shared hosting listed %v tracking %v, want listed and not tracking", found, tracking)
		}
	})
}

func TestCDNLabelledDomainCountsOnlyForBlocklistedPaths(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://cdntrack.test/")
	found, tracking := company(r, "Shared Hosting")
	if !found || !tracking {
		t.Errorf("shared hosting listed %v tracking %v, want a blocklisted path on a CDN domain to count", found, tracking)
	}
	if r.Score.Parts.Tracking == 0 {
		t.Errorf("score parts %+v, want the company counted", r.Score.Parts)
	}
}

func TestThinPagesAreBlockedAfterOneRetry(t *testing.T) {
	for host, why := range map[string]string{
		"thin.test":       "nearly empty",
		"thincookie.test": "anti-bot cookies",
		"wall.test":       "bot check",
	} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			r := scanTest(t, "http://"+host+"/")
			if r.Status != statusBlocked || !strings.Contains(r.Reason, why) {
				t.Fatalf("status %s reason %q, want blocked: %s", r.Status, r.Reason, why)
			}
			if r.Score != nil || r.Finding != "" {
				t.Errorf("a blocked scan has a score: %+v %q", r.Score, r.Finding)
			}
			if n := visits(host); n != 2 {
				t.Errorf("%d visits, want the first and exactly one retry", n)
			}
			if !hasNote(r, "repeated once") {
				t.Errorf("notes %q do not say the scan was repeated", r.Notes)
			}
		})
	}
}

func TestThinFirstVisitThenFullPageIsScored(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://thinonce.test/")
	if r.Status != statusDone || r.Score == nil {
		t.Fatalf("status %s reason %q, want a scored result from the second visit", r.Status, r.Reason)
	}
	if n := visits("thinonce.test"); n != 2 {
		t.Errorf("%d visits, want 2", n)
	}
	// the planted first page has no text and no bot cookie: it did not finish loading
	if !hasNote(r, "first visit was blocked") {
		t.Errorf("notes %q", r.Notes)
	}
}

func TestRealPageBehindABotManagerStaysScored(t *testing.T) {
	t.Parallel()
	r := scanTest(t, "http://solo.test/")
	if r.Status != statusDone || r.Score == nil {
		t.Fatalf("status %s reason %q, want scored: no outside company, but a real page", r.Status, r.Reason)
	}
	if n := visits("solo.test"); n != 1 {
		t.Errorf("%d visits, want 1: a scored page is not retried", n)
	}
	if r.Capture.Page.TextLen < 2000 {
		t.Errorf("page text %d", r.Capture.Page.TextLen)
	}
}
