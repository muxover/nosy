package scan

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
	"github.com/muxover/nosy/internal/chrome"
	"github.com/muxover/nosy/internal/detect"
	"github.com/muxover/nosy/internal/guard"
	"github.com/muxover/nosy/internal/score"
	"github.com/muxover/nosy/internal/site"
)

// Rules are the lists the detectors match against. Rules v1 fills them in; until
// then the index is empty, so companies are listed but none is classified.
type Rules struct {
	Index     *detect.Index
	Blocklist *detect.Blocklist
	Vendors   *detect.Vendors
	Version   string
}

// Options say how to scan. The zero value is a desktop scan with the default limits.
type Options struct {
	Version string // Nosy's version, recorded in the result
	Rules   Rules

	// Chrome is the path of the browser; empty lets the driver search PATH.
	Chrome string
	// BrowserSource says why that browser was chosen, for the conditions.
	BrowserSource chrome.Source
	// Duration is the longest a visit may last; 20 seconds when zero.
	Duration time.Duration
	Mobile   bool
	GPC      bool
	// AllowPrivate lets the scan reach private, loopback and link-local addresses.
	AllowPrivate bool
	MaxBytes     int64

	// Remote is the DevTools address of a browser that stays up; the visit gets
	// a context of its own in it.
	Remote string

	Progress func(step string)
	Verbose  func(format string, args ...any)

	// Dialer replaces the guard's default; tests use it to stub name resolution.
	Dialer *guard.Dialer
}

func (o Options) progress(step string) {
	if o.Progress != nil {
		o.Progress(step)
	}
}

func (o Options) rules() Rules {
	r := o.Rules
	if r.Index == nil {
		r.Index = detect.NewIndex(nil, nil)
	}
	if r.Vendors == nil {
		r.Vendors = detect.DefaultVendors()
	}
	if r.Version == "" {
		r.Version = "none"
	}
	return r
}

func (o Options) duration() time.Duration {
	if o.Duration <= 0 {
		return guard.HardStop
	}
	return o.Duration
}

// Run scans one site. An error means the scan could not start: the address was
// refused (a *site.InputError), the browser could not be started, or ctx ended. A
// scan that started always returns a Result, even when it was blocked or failed.
func Run(ctx context.Context, input string, o Options) (*Result, error) {
	target, err := site.Parse(input, o.AllowPrivate)
	if err != nil {
		return nil, &site.InputError{Err: err}
	}
	d := o.Dialer
	if d == nil {
		d = &guard.Dialer{}
	}
	d.AllowPrivate = d.AllowPrivate || o.AllowPrivate
	if err := d.Check(ctx, target.Hostname()); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &site.InputError{Err: fmt.Errorf("refused %s: %w", target.Hostname(), err)}
	}
	if o.MaxBytes == 0 {
		o.MaxBytes = guard.MaxBytes
	}
	r, err := visitAndBuild(ctx, target, d, o)
	if err != nil || r.Status != StatusBlocked {
		return r, err
	}
	// a first visit can meet a thin page that a second does not (a bot check that
	// only trips on a cold start), so a blocked scan gets one more
	note := firstVisitNote(r)
	o.progress("Trying once more")
	if o.Verbose != nil {
		o.Verbose("%s, scanning again", note)
	}
	again, err := visitAndBuild(ctx, target, d, o)
	if err != nil {
		return nil, err
	}
	again.Notes = append(again.Notes, note+", so the scan was repeated once")
	return again, nil
}

// firstVisitNote says what the first visit met, for the note on the second.
func firstVisitNote(r *Result) string {
	if r.Kind == KindUnfinished {
		return fmt.Sprintf("the first visit did not finish loading (%s)", r.Reason)
	}
	return fmt.Sprintf("the first visit was blocked (%s)", r.Reason)
}

const maxRestarts = 2

// visitAndBuild is one visit, repeated while the browser's own network
// restarts under it, and the result made from it.
func visitAndBuild(ctx context.Context, target *url.URL, d *guard.Dialer, o Options) (*Result, error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	var capt *capture.Capture
	restarts, crashes := 0, 0
	for {
		capt, err = visit(ctx, target, token, d, o)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		crashed := capt.Page.Crashed
		if restarts == maxRestarts || !(crashed || networkRestarted(capt)) {
			break
		}
		restarts++
		if crashed {
			crashes++
		}
		if o.Verbose != nil {
			o.Verbose("the browser crashed or its network restarted during the visit, scanning again (%d)", restarts)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	o.progress("Building the result")
	r := Build(target, capt, o)
	if n := restarts - crashes; n > 0 {
		r.Notes = append(r.Notes, fmt.Sprintf("the browser's own network restarted while it started, so the scan was repeated (%d time(s))", n))
	}
	if crashes > 0 {
		r.Notes = append(r.Notes, fmt.Sprintf("the browser crashed during the visit, so the scan was repeated (%d time(s))", crashes))
	}
	if capt.Page.Crashed {
		return nil, errors.New("the browser kept crashing")
	}
	return r, nil
}

func visit(ctx context.Context, target *url.URL, token string, d *guard.Dialer, o Options) (*capture.Capture, error) {
	proxy, err := guard.StartProxy(d, o.MaxBytes, nil)
	if err != nil {
		return nil, err
	}
	defer proxy.Close()
	return browser.Visit(ctx, target, browser.Options{
		Chrome:   o.Chrome,
		Proxy:    proxy,
		Token:    token,
		Remote:   o.Remote,
		Duration: o.Duration,
		Mobile:   o.Mobile,
		GPC:      o.GPC,
		Verbose:  o.Verbose,
		Progress: o.Progress,
	})
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

// Build turns a capture into a result. It runs no browser.
func Build(target *url.URL, c *capture.Capture, o Options) *Result {
	rules := o.rules()
	limit := o.duration()
	r := &Result{
		Schema:       Schema,
		RequestedURL: target.String(),
		FinalURL:     c.Page.FinalURL,
		Conditions: Conditions{
			Date:                 c.Page.ScannedAt.Format(time.RFC3339),
			Country:              "unknown",
			Browser:              c.Page.Chromium,
			BrowserSource:        browserSource(o.BrowserSource),
			BrowserPinned:        o.BrowserSource == chrome.SourcePinned,
			UserAgent:            c.Page.UserAgent,
			Device:               c.Page.Device,
			Viewport:             c.Page.Viewport,
			Locale:               "en-US",
			ConsentMode:          "not touched",
			GPC:                  c.Page.GPC,
			DurationLimitSeconds: int(limit.Seconds()),
			DurationMS:           c.Page.Duration.Milliseconds(),
			Nosy:                 o.Version,
			Rules:                rules.Version,
			Settings: []string{
				"third-party cookies allowed (TrackingProtection3pcd disabled)",
				"site isolation off",
				"software WebGL on",
			},
		},
		Page: Page{
			Status: c.Page.MainStatus, TextLen: c.Page.TextLen, Elements: c.Page.Elements,
			LoadMS: c.Page.LoadMS, NeverQuiet: c.Page.NeverQuiet,
		},
	}
	if c.Page.ScannedAt.IsZero() {
		r.Conditions.Date = time.Now().UTC().Format(time.RFC3339)
	}
	r.Site = detect.Registrable(target.Hostname())
	if fu, err := url.Parse(c.Page.FinalURL); err == nil && fu.Hostname() != "" && !strings.HasPrefix(c.Page.FinalURL, "chrome-error:") {
		r.Site = detect.Registrable(fu.Hostname())
	}

	reqs := c.Requests
	parties := detect.Analyze(r.Site, reqs, rules.Index, rules.Vendors, rules.Blocklist)
	switch {
	case c.Page.MainError != "" || strings.HasPrefix(c.Page.FinalURL, "chrome-error:"):
		r.Status, r.Reason = StatusFailed, failReason(c)
	case c.Page.SizeCapHit && c.Page.TextLen == 0:
		r.Status, r.Reason = StatusFailed, "the page was larger than the download limit"
	case c.Page.TimeLimitHit && c.Page.TextLen == 0:
		r.Status, r.Reason = StatusFailed, fmt.Sprintf("the page did not finish loading within %d seconds", int(limit.Seconds()))
	default:
		why := detect.Blocked(c.Page)
		if why == "" {
			why = detect.BotCheckScripts(c.Page, c.Requests)
		}
		if why == "" {
			why = detect.Thin(c.Page, pageCookies(c, r.Site), parties.ThirdPartyContacted())
		}
		if why != "" {
			r.Status, r.Reason, r.Kind = StatusBlocked, why, KindBotCheck
			if detect.Unfinished(why) {
				r.Kind = KindUnfinished
			}
		} else {
			r.Status = StatusDone
		}
	}
	r.Notes = visitNotes(c, limit)
	if rules.Version == "none" {
		r.Notes = append(r.Notes, "no tracker rules are installed yet, so companies are listed but none is classified as tracking")
	}

	r.Requests = reqs
	r.Companies = parties.Companies
	r.Weight = weight(reqs, parties)
	r.Consent = detect.DetectConsent(c.Page.ConsentAPIs, nil)

	now := c.Page.ScannedAt
	if now.IsZero() {
		now = time.Now()
	}
	// only cookies the page could have caused: the browser's own background
	// requests can leave cookies in the same jar
	jar := *c
	jar.Cookies = pageCookies(c, r.Site)
	idIdx := detect.IdentifierCookies(jar.Cookies, now)
	ident := map[int]bool{}
	for _, i := range idIdx {
		ident[i] = true
	}
	r.Cookies = cookies(&jar, r.Site, rules.Index, ident, now)

	r.Storage = storage(c, r.Site, rules.Index)
	r.Fingerprinting = detect.Fingerprinting(c.APICalls)
	for _, i := range detect.Replay(reqs, rules.Vendors) {
		r.SessionRecording = append(r.SessionRecording, Evidence{Request: i, URL: reqs[i].URL})
	}
	r.TypedText.FieldsTested = c.Page.TypedInto
	for _, h := range detect.KeyCapture(c.Token, r.Site, reqs, c.WSFrames, rules.Index) {
		kh := KeyHit{KeyHit: h}
		if h.Request >= 0 {
			kh.URL = reqs[h.Request].URL
		}
		r.TypedText.Hits = append(r.TypedText.Hits, kh)
	}
	if c.Page.TypedInto == 0 && r.Status == StatusDone {
		r.Notes = append(r.Notes, "no visible text field was found, so typed-text capture was not tested")
	}

	if r.Status == StatusDone {
		in := score.Input{
			TrackingCompanies: parties.Tracking,
			AdTech:            parties.AdTech,
			IdentifierCookies: len(idIdx),
			Fingerprinting:    detect.TechniqueNames(r.Fingerprinting),
			SessionRecording:  len(r.SessionRecording) > 0,
			KeyCapture:        thirdPartyKeyHit(r.TypedText.Hits),
		}
		res := score.Compute(in)
		r.Score = &res
		r.Finding = score.Finding(r.Site, in)
	}
	r.Findings = findings(r, parties)
	return r
}

func visitNotes(c *capture.Capture, limit time.Duration) []string {
	var n []string
	if c.Page.NeverQuiet {
		n = append(n, "the network did not go quiet in time, so the scan moved on")
	}
	if c.Page.TimeLimitHit {
		n = append(n, fmt.Sprintf("the %d second limit was reached and the scan was stopped", int(limit.Seconds())))
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

func weight(reqs []capture.Request, p detect.Parties) Weight {
	var w Weight
	for i, r := range reqs {
		if !p.Per[i].Counted {
			continue
		}
		w.Requests++
		w.Bytes += r.Bytes
		if p.Per[i].ThirdParty {
			w.ThirdRequests++
			w.ThirdBytes += r.Bytes
		}
	}
	return w
}

func cookies(c *capture.Capture, siteName string, index *detect.Index, ident map[int]bool, now time.Time) []Cookie {
	var out []Cookie
	for i, ck := range c.Cookies {
		dom := strings.TrimPrefix(ck.Domain, ".")
		reg := detect.Registrable(dom)
		rc := Cookie{
			Name:       ck.Name,
			Domain:     dom,
			ThirdParty: reg != siteName && !index.SameOwner(reg, siteName),
			Session:    ck.Session,
			Length:     len(ck.Value),
			Identifier: ident[i],
			SetBy:      cookieSetter(c, ck),
		}
		if !ck.Session && ck.Expires > 0 {
			rc.Days = int(time.Unix(int64(ck.Expires), 0).Sub(now).Hours() / 24)
		}
		out = append(out, rc)
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

// only a third party receiving the typed text counts; the site's own
// servers answering search suggestions are listed but not scored
func thirdPartyKeyHit(hits []KeyHit) bool {
	for _, h := range hits {
		if h.ThirdParty {
			return true
		}
	}
	return false
}

func browserSource(s chrome.Source) string {
	if s == "" {
		return "unknown"
	}
	return string(s)
}

// storage turns what each frame origin stored into the result's form, marking
// the third parties and the values that look like identifiers.
func storage(c *capture.Capture, siteName string, index *detect.Index) []StorageOrigin {
	out := []StorageOrigin{}
	for _, s := range c.Storage {
		host := ""
		if u, err := url.Parse(s.Origin); err == nil {
			host = u.Hostname()
		}
		reg := detect.Registrable(host)
		o := StorageOrigin{
			Origin:         s.Origin,
			ThirdParty:     reg != siteName && !index.SameOwner(reg, siteName),
			CacheStorage:   s.Caches,
			LocalStorage:   storageKeys(s.Local, true),
			SessionStorage: storageKeys(s.Session, false),
		}
		for _, db := range s.IndexedDB {
			d := IDB{Name: db.Name, Version: db.Version}
			for _, st := range db.Stores {
				store := IDBStore{Name: st.Name, Entries: st.Entries, Keys: st.Keys}
				seen := map[string]bool{}
				for _, v := range st.Values {
					name := v.Key
					if name == "" {
						name = "value"
					}
					if !seen[name] && detect.LooksLikeIdentifier(v.Value) {
						seen[name] = true
						store.IdentifierValues = append(store.IdentifierValues, name)
					}
				}
				d.Stores = append(d.Stores, store)
			}
			o.IndexedDB = append(o.IndexedDB, d)
		}
		for _, w := range s.ServiceWorkers {
			o.ServiceWorkers = append(o.ServiceWorkers, ServiceWorker{Scope: w.Scope, Script: w.Script})
		}
		out = append(out, o)
	}
	return out
}

// storageKeys lists the keys; persistent ones are checked with the identifier
// rule, the others are gone when the tab closes and so never count.
func storageKeys(items []capture.StorageItem, persistent bool) []StorageKey {
	var out []StorageKey
	for _, it := range items {
		out = append(out, StorageKey{Key: it.Key, Length: it.Length, Identifier: persistent && detect.LooksLikeIdentifier(it.Value)})
	}
	return out
}
