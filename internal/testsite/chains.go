//go:build nosytest

package testsite

import (
	"net/http"
	"strconv"
	"strings"
)

// chainPages are the hosts that test how many redirects a visit follows, and
// where it stops: a loop, chains of any length, a chain across sites, a chain
// that ends at an address the scanner must never reach, and images behind such
// chains.
func (s *Site) chainPages() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"loop.test": func(w http.ResponseWriter, r *http.Request) {
			next := "/b"
			if r.URL.Path == "/b" {
				next = "/a"
			}
			http.Redirect(w, r, next, http.StatusFound)
		},
		// /N redirects to /N-1 until it reaches 0. The home page of nN.hops.test
		// starts a chain of N, because a scan only ever opens a site's home page.
		"hops.test": func(w http.ResponseWriter, r *http.Request) {
			n, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
			if r.URL.Path == "/" {
				label, _, _ := strings.Cut(hostOnly(r.Host), ".")
				n, err = strconv.Atoi(strings.TrimPrefix(label, "n"))
			}
			if err != nil || n <= 0 {
				html(w, 200, page("The end of the chain", ""))
				return
			}
			http.Redirect(w, r, "/"+strconv.Itoa(n-1), http.StatusFound)
		},
		"hopa.test":   redirectTo("http://hopb.test/"),
		"hopb.test":   redirectTo("http://hopc.test/"),
		"hopc.test":   redirectTo("http://preview.test/"),
		"hopbad.test": redirectTo("http://hopbad2.test/"),
		"hopbad2.test": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, s.hopTarget(), http.StatusFound)
		},
		// a page whose image redirects many times, and one whose image is sent on to a private address
		"imgchain.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Image behind a long chain", `<img alt="" src="http://hopimg.test/60">`))
		},
		"hopimg.test": func(w http.ResponseWriter, r *http.Request) {
			n, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
			if err != nil || n <= 0 {
				pixel(w, r)
				return
			}
			http.Redirect(w, r, "/"+strconv.Itoa(n-1), http.StatusFound)
		},
		"imgbad.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, page("Image sent to a private address", `<img alt="" src="http://hopimgbad.test/">`))
		},
		"hopimgbad.test": redirectTo("http://hopimgbad2.test/"),
		"hopimgbad2.test": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, s.hopTarget(), http.StatusFound)
		},
		"preview.test": func(w http.ResponseWriter, r *http.Request) {
			html(w, 200, `<!doctype html><html><head><title>Preview</title>
<link rel="stylesheet" href="/style.css"><link rel="preload" as="script" href="http://cdn.test/lib.js">
<script src="http://stats.test/s.js"></script><script src="/own.js"></script></head><body>
<img alt="" src="http://ads.test/p.gif"><img alt="" src="http://ads.test/q.gif"><iframe src="http://social.test/embed" width="10" height="10"></iframe>
<noscript><img alt="" src="http://unlisted.test/never.gif"></noscript>`+filler(600)+`</body></html>`)
		},
	}
}

// hopTarget is where hopbad.test's chain ends: a loopback address with its own
// listener on port 80 when that could be opened, and a private address otherwise.
// The scanner must never connect to either.
func (s *Site) hopTarget() string {
	if s.HopCanary != nil {
		return "http://" + s.HopCanary.Addr + "/canary"
	}
	return "http://10.0.0.5/canary"
}
