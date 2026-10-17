package check

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/muxover/nosy/internal/capture"
	"github.com/muxover/nosy/internal/detect"
	"github.com/muxover/nosy/internal/guard"
	"github.com/muxover/nosy/internal/site"
)

// Schema names the layout of Result in JSON. It is provisional until the
// setup checks of step 9 settle it.
const Schema = "nosy-check/0"

// Labels say how a check is known. A check here is either seen directly or
// seen and then matched to a rule.
const (
	Observed   = "observed"
	Classified = "classified"
)

// Check is one thing learned about the site, with whether it needs attention.
type Check struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Text  string `json:"text"`
	// OK is false when the check found something a site owner would want to fix.
	OK bool `json:"ok"`
}

type Cookie struct {
	Name       string `json:"name"`
	Domain     string `json:"domain,omitempty"`
	Session    bool   `json:"session"`
	Days       int    `json:"days,omitempty"`
	Identifier bool   `json:"identifier"`
	AntiBot    bool   `json:"anti_bot,omitempty"`
}

type Result struct {
	Schema    string   `json:"schema"`
	URL       string   `json:"url"`
	FinalURL  string   `json:"final_url"`
	Addresses []string `json:"addresses"`
	Status    int      `json:"status"`
	Redirects []string `json:"redirects,omitempty"`
	Cookies   []Cookie `json:"cookies"`
	Checks    []Check  `json:"checks"`
	Date      string   `json:"date"`
	Nosy      string   `json:"nosy"`
}

// Options say how to check. The zero value checks a public site.
type Options struct {
	Version      string
	AllowPrivate bool
	Timeout      time.Duration

	// Dialer and TLS replace the defaults; tests use them.
	Dialer *guard.Dialer
	TLS    *tls.Config
}

// Run checks one site. An error means the check could not run: the address was
// refused (a *site.InputError), or the site could not be reached.
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
	timeout := o.Timeout
	if timeout == 0 {
		timeout = guard.HardStop
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	r := &Result{Schema: Schema, URL: target.String(), Date: time.Now().UTC().Format(time.RFC3339), Nosy: o.Version, Cookies: []Cookie{}}
	for _, ip := range lookup(ctx, d, target.Hostname()) {
		r.Addresses = append(r.Addresses, ip.String())
	}

	var responses []*http.Response
	client := &http.Client{
		Transport: &http.Transport{
			DialContext:           d.DialContext,
			TLSClientConfig:       o.TLS,
			DisableKeepAlives:     true,
			ResponseHeaderTimeout: 15 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			responses = append(responses, req.Response)
			r.Redirects = append(r.Redirects, req.URL.String())
			if len(via) > guard.MaxRedirects {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Nosy/"+o.Version+" (+https://nosy.muxover.com)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not fetch %s: %w", target, err)
	}
	// headers are all that is read; the body is not downloaded
	_, _ = io.CopyN(io.Discard, resp.Body, 0)
	resp.Body.Close()
	responses = append(responses, resp)
	r.FinalURL = resp.Request.URL.String()
	r.Status = resp.StatusCode

	now := time.Now()
	for _, resp := range responses {
		for _, c := range resp.Cookies() {
			r.Cookies = append(r.Cookies, cookie(c, resp.Request.URL.Hostname(), now))
		}
	}
	r.Checks = checks(r, target.Hostname())
	return r, nil
}

func lookup(ctx context.Context, d *guard.Dialer, host string) []netip.Addr {
	ips, err := d.Resolve(ctx, host)
	if err != nil {
		return nil
	}
	sort.Slice(ips, func(i, j int) bool { return ips[i].Less(ips[j]) })
	return ips
}

func cookie(c *http.Cookie, host string, now time.Time) Cookie {
	out := Cookie{Name: c.Name, Domain: strings.TrimPrefix(c.Domain, "."), AntiBot: detect.AntiBotCookie(c.Name)}
	if out.Domain == "" {
		out.Domain = host
	}
	cc := capture.Cookie{Name: c.Name, Value: c.Value, Session: true}
	switch {
	case c.MaxAge > 0:
		cc.Session, cc.Expires = false, float64(now.Add(time.Duration(c.MaxAge)*time.Second).Unix())
	case c.MaxAge == 0 && !c.Expires.IsZero():
		cc.Session, cc.Expires = false, float64(c.Expires.Unix())
	}
	out.Session = cc.Session
	if !cc.Session {
		out.Days = int(time.Unix(int64(cc.Expires), 0).Sub(now).Hours() / 24)
	}
	out.Identifier = detect.IsIdentifier(cc, now)
	return out
}

func checks(r *Result, host string) []Check {
	var out []Check
	add := func(id, label, text string, ok bool) {
		out = append(out, Check{ID: id, Label: label, Text: text, OK: ok})
	}

	add("address", Observed, fmt.Sprintf("%s leads to %s.", host, listOr(r.Addresses, "no address that could be listed")), true)
	add("answer", Observed, fmt.Sprintf("The home page answered with HTTP %d.", r.Status), r.Status < 400)
	if n := len(r.Redirects); n > 0 {
		add("redirects", Observed, fmt.Sprintf("%d %s to reach %s.", n, plural(n, "redirect", "redirects"), r.FinalURL), true)
	}
	secure := strings.HasPrefix(r.FinalURL, "https://")
	if secure {
		add("https", Observed, "The home page is served over HTTPS.", true)
	} else {
		add("https", Observed, "The home page ends on plain HTTP, so everything sent to it can be read on the way.", false)
	}

	var ids, anti []string
	for _, c := range r.Cookies {
		if c.Identifier {
			ids = append(ids, c.Name)
		}
		if c.AntiBot {
			anti = append(anti, c.Name)
		}
	}
	switch {
	case len(r.Cookies) == 0:
		add("first-cookies", Observed, "The first answer set no cookies.", true)
	default:
		add("first-cookies", Observed, fmt.Sprintf("The first answer set %d %s.", len(r.Cookies), plural(len(r.Cookies), "cookie", "cookies")), true)
	}
	if len(ids) > 0 {
		add("identifier-cookies", Classified, fmt.Sprintf("Set before anything else loads, %s %s like an identifier for this browser.", strings.Join(ids, ", "), plural(len(ids), "looks", "look")), false)
	}
	if len(anti) > 0 {
		add("bot-management-cookies", Classified, "The cookies include bot-management ones: "+strings.Join(anti, ", ")+".", true)
	}
	return out
}

func listOr(l []string, empty string) string {
	if len(l) == 0 {
		return empty
	}
	return strings.Join(l, ", ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
