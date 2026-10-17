//go:build nosytest

package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/muxover/nosy/internal/browser"
	"github.com/muxover/nosy/internal/capture"
	"github.com/muxover/nosy/internal/detect"
	"github.com/muxover/nosy/internal/guard"
	"github.com/muxover/nosy/internal/score"
)

// The pipeline the planted-page tests drive: guard proxy, browser visit,
// detectors, score. It is the shape of a scan without the report around it.

const (
	statusDone    = "done"
	statusBlocked = "blocked"
	statusFailed  = "failed"
)

type config struct {
	maxBytes int64
	remote   string
	dialer   *guard.Dialer
	vendors  *detect.Vendors
	verbose  func(format string, args ...any)
}

type cookie struct {
	Name       string
	Domain     string
	ThirdParty bool
	Session    bool
	Identifier bool
	Length     int
	SetBy      string
}

type keyHit struct {
	detect.KeyHit
	URL string
}

type outcome struct {
	Site         string
	FinalURL     string
	Status       string
	Reason       string
	Unfinished   bool
	Notes        []string
	Capture      *capture.Capture
	Requests     []capture.Request
	Parties      detect.Parties
	Cookies      []cookie
	Techniques   []detect.Technique
	Recording    []int
	KeyHits      []keyHit
	FieldsTested int
	Consent      detect.Consent
	Score        *score.Result
	Finding      string
	Bytes        int64
}

// run scans one site: refuse it up front if it resolves to a private address,
// visit it, and visit once more if the first visit met a thin page.
func run(ctx context.Context, input string, c config) (*outcome, error) {
	target, err := url.Parse(input)
	if err != nil {
		return nil, err
	}
	d := c.dialer
	if d == nil {
		d = &guard.Dialer{}
	}
	if err := d.Check(ctx, target.Hostname()); err != nil {
		return nil, fmt.Errorf("refused %s: %w", target.Hostname(), err)
	}
	if c.maxBytes == 0 {
		c.maxBytes = guard.MaxBytes
	}
	o, err := visitAndBuild(ctx, target, d, c)
	if err != nil || o.Status != statusBlocked {
		return o, err
	}
	again, err := visitAndBuild(ctx, target, d, c)
	if err != nil {
		return nil, err
	}
	again.Notes = append(again.Notes, "the first visit was blocked ("+o.Reason+"), so the scan was repeated once")
	return again, nil
}

const maxRestarts = 2

func visitAndBuild(ctx context.Context, target *url.URL, d *guard.Dialer, c config) (*outcome, error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	var capt *capture.Capture
	restarts := 0
	for {
		if capt, err = visit(ctx, target, token, d, c); err != nil {
			return nil, err
		}
		if restarts == maxRestarts || !(capt.Page.Crashed || networkRestarted(capt)) {
			break
		}
		restarts++
		time.Sleep(time.Second)
	}
	if capt.Page.Crashed {
		return nil, errors.New("the browser kept crashing")
	}
	return build(target, capt, c), nil
}

func visit(ctx context.Context, target *url.URL, token string, d *guard.Dialer, c config) (*capture.Capture, error) {
	proxy, err := guard.StartProxy(d, c.maxBytes, nil)
	if err != nil {
		return nil, err
	}
	defer proxy.Close()
	return browser.Visit(ctx, target, browser.Options{Proxy: proxy, Token: token, Remote: c.remote, Verbose: c.verbose})
}

// Chromium drops in-flight requests with these errors when its network
// stack reconfigures itself, which can happen just after it starts. A visit
// that saw one did not see the page the way a visitor would have.
var restartErrors = []string{"ERR_NETWORK_CHANGED", "ERR_CERT_VERIFIER_CHANGED", "ERR_SOCKET_NOT_CONNECTED"}

func networkRestarted(c *capture.Capture) bool {
	for _, r := range c.Requests {
		for _, e := range restartErrors {
			if strings.Contains(r.Failed, e) {
				return true
			}
		}
	}
	for _, e := range restartErrors {
		if strings.Contains(c.Page.MainError, e) {
			return true
		}
	}
	return false
}

func newToken() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "nosy" + hex.EncodeToString(b), nil
}

func build(target *url.URL, c *capture.Capture, cfg config) *outcome {
	o := &outcome{Capture: c, FinalURL: c.Page.FinalURL, Requests: c.Requests}
	o.Site = detect.Registrable(target.Hostname())
	if fu, err := url.Parse(c.Page.FinalURL); err == nil && fu.Hostname() != "" && !strings.HasPrefix(c.Page.FinalURL, "chrome-error:") {
		o.Site = detect.Registrable(fu.Hostname())
	}
	vendors := cfg.vendors
	if vendors == nil {
		vendors = testVendors()
	}
	reqs := c.Requests
	o.Parties = detect.Analyze(o.Site, reqs, testIndex(), vendors, testBlocklist())
	o.Notes = visitNotes(c)
	switch {
	case c.Page.MainError != "" || strings.HasPrefix(c.Page.FinalURL, "chrome-error:"):
		o.Status, o.Reason = statusFailed, failReason(c)
	case c.Page.SizeCapHit && c.Page.TextLen == 0:
		o.Status, o.Reason = statusFailed, "the page was larger than the download limit"
	case c.Page.TimeLimitHit && c.Page.TextLen == 0:
		o.Status, o.Reason = statusFailed, "the page did not finish loading within 20 seconds"
	default:
		why := detect.Blocked(c.Page)
		if why == "" {
			why = detect.BotCheckScripts(c.Page, c.Requests)
		}
		if why == "" {
			why = detect.Thin(c.Page, pageCookies(c, o.Site), o.Parties.ThirdPartyContacted())
		}
		if why != "" {
			o.Status, o.Reason, o.Unfinished = statusBlocked, why, detect.Unfinished(why)
		} else {
			o.Status = statusDone
		}
	}
	for i, r := range reqs {
		if o.Parties.Per[i].Counted {
			o.Bytes += r.Bytes
		}
	}
	o.Consent = detect.DetectConsent(c.Page.ConsentAPIs, nil)

	now := c.Page.ScannedAt
	if now.IsZero() {
		now = time.Now()
	}
	jar := *c
	jar.Cookies = pageCookies(c, o.Site)
	idIdx := detect.IdentifierCookies(jar.Cookies, now)
	ident := map[int]bool{}
	for _, i := range idIdx {
		ident[i] = true
	}
	o.Cookies = cookies(&jar, o.Site, ident)

	o.Techniques = detect.Fingerprinting(c.APICalls)
	o.Recording = detect.Replay(reqs, vendors)
	o.FieldsTested = c.Page.TypedInto
	for _, h := range detect.KeyCapture(c.Token, o.Site, reqs, c.WSFrames, testIndex()) {
		kh := keyHit{KeyHit: h}
		if h.Request >= 0 {
			kh.URL = reqs[h.Request].URL
		}
		o.KeyHits = append(o.KeyHits, kh)
	}
	if o.Status == statusDone {
		in := score.Input{
			TrackingCompanies: o.Parties.Tracking,
			AdTech:            o.Parties.AdTech,
			IdentifierCookies: len(idIdx),
			Fingerprinting:    detect.TechniqueNames(o.Techniques),
			SessionRecording:  len(o.Recording) > 0,
			KeyCapture:        thirdPartyKeyHit(o.KeyHits),
		}
		res := score.Compute(in)
		o.Score = &res
		o.Finding = score.Finding(o.Site, in)
	}
	return o
}

func visitNotes(c *capture.Capture) []string {
	var n []string
	if c.Page.NeverQuiet {
		n = append(n, "the network did not go quiet within 8.5 seconds, so the scan moved on")
	}
	if c.Page.TimeLimitHit {
		n = append(n, "the 20 second limit was reached and the scan was stopped")
	}
	if c.Page.SizeCapHit {
		n = append(n, "the download size limit was reached and the scan was stopped")
	}
	if hosts := pageRefusals(c); len(hosts) > 0 {
		n = append(n, fmt.Sprintf("the safety guard refused %d destination(s) the page asked for: %s", len(hosts), strings.Join(hosts, ", ")))
	}
	return n
}

// pageRefusals lists refused destinations that a request on the page asked
// for; the browser's own background connections are left out.
func pageRefusals(c *capture.Capture) []string {
	asked := map[string]bool{}
	for _, r := range c.Requests {
		asked[r.Host] = true
	}
	var hosts []string
	seen := map[string]bool{}
	for _, rf := range c.Proxy.Refused {
		k := rf.Host + ":" + rf.Port
		if asked[rf.Host] && !seen[k] {
			seen[k] = true
			hosts = append(hosts, k)
		}
	}
	sort.Strings(hosts)
	return hosts
}

func failReason(c *capture.Capture) string {
	e := c.Page.MainError
	switch {
	case strings.Contains(e, "refused by the safety guard"):
		return "the site sent the scan to a private or local address, which is never contacted"
	case strings.Contains(e, "TOO_MANY_REDIRECTS"):
		return "the site redirected the scan too many times"
	case strings.Contains(e, "could not connect"):
		return "the site could not be reached"
	case strings.Contains(e, "EMPTY_RESPONSE"), strings.Contains(e, "CONNECTION_CLOSED"), strings.Contains(e, "CONNECTION_RESET"):
		return "the site closed the connection without answering"
	case strings.Contains(e, "NAME_NOT_RESOLVED"):
		return "the domain name did not resolve"
	case strings.Contains(e, "TUNNEL_CONNECTION_FAILED"), strings.Contains(e, "PROXY_CONNECTION_FAILED"):
		return "the connection was refused or failed"
	case strings.Contains(e, "CERT"):
		return "the site's certificate was not accepted"
	case strings.Contains(e, "TIMED_OUT"):
		return "the connection timed out"
	case e == "":
		return "the page could not be loaded"
	}
	return "the page could not be loaded (" + e + ")"
}

// pageCookies drops cookies whose domain the page never contacted and no
// script on the page wrote.
func pageCookies(c *capture.Capture, siteName string) []capture.Cookie {
	contacted := map[string]bool{siteName: true}
	for _, r := range c.Requests {
		if r.Host != "" {
			contacted[detect.Registrable(r.Host)] = true
		}
	}
	written := map[string]bool{}
	for _, w := range c.CookieWrites {
		written[w.Name] = true
	}
	var out []capture.Cookie
	for _, ck := range c.Cookies {
		if contacted[detect.Registrable(strings.TrimPrefix(ck.Domain, "."))] || written[ck.Name] {
			out = append(out, ck)
		}
	}
	return out
}

func cookies(c *capture.Capture, siteName string, ident map[int]bool) []cookie {
	var out []cookie
	for i, ck := range c.Cookies {
		dom := strings.TrimPrefix(ck.Domain, ".")
		reg := detect.Registrable(dom)
		out = append(out, cookie{
			Name:       ck.Name,
			Domain:     dom,
			ThirdParty: reg != siteName && !testIndex().SameOwner(reg, siteName),
			Session:    ck.Session,
			Length:     len(ck.Value),
			Identifier: ident[i],
			SetBy:      cookieSetter(c, ck),
		})
	}
	return out
}

func cookieSetter(c *capture.Capture, ck capture.Cookie) string {
	dom := strings.TrimPrefix(ck.Domain, ".")
	for _, r := range c.Requests {
		for _, name := range r.SetCookies {
			if name == ck.Name && (r.Host == dom || strings.HasSuffix(r.Host, "."+dom)) {
				return r.URL
			}
		}
	}
	for _, w := range c.CookieWrites {
		if w.Name == ck.Name {
			return w.Script
		}
	}
	return ""
}

// only a third party receiving the typed text counts; the site's own
// servers answering search suggestions are listed but not scored
func thirdPartyKeyHit(hits []keyHit) bool {
	for _, h := range hits {
		if h.ThirdParty {
			return true
		}
	}
	return false
}
