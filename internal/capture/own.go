package capture

import "strings"

var browserSchemes = []string{"chrome://", "chrome-untrusted://", "chrome-extension://", "chrome-search://", "chrome-error://", "chrome-native://", "devtools://"}

// browserScripts are scripts only the browser's own pages load: the new tab
// page's Google bar. When the request that loaded one was not captured (the
// page was gone before the recorder attached), the script's address still
// gives it away.
var browserScripts = []string{"https://www.gstatic.com/og/"}

func browserScript(u string) bool {
	for _, s := range browserScripts {
		if strings.HasPrefix(u, s) {
			return true
		}
	}
	return false
}

func browserPage(u string) bool {
	for _, s := range browserSchemes {
		if strings.HasPrefix(u, s) {
			return true
		}
	}
	return false
}

// WithoutBrowserOwn drops the requests the browser made for itself, which are
// not the site's: the new tab page's Google bar, an error page, an extension.
// A request is the browser's when a page of the browser started it, or a
// script that such a request loaded did, or one of the browser's own scripts.
func WithoutBrowserOwn(reqs []Request) []Request {
	own := map[string]bool{}
	drop := make([]bool, len(reqs))
	for changed := true; changed; {
		changed = false
		for i, r := range reqs {
			if drop[i] {
				continue
			}
			init, _, _ := strings.Cut(r.Initiator, "#")
			if browserPage(r.URL) || browserPage(init) || browserScript(init) || own[init] {
				drop[i], changed = true, true
				u, _, _ := strings.Cut(r.URL, "#")
				own[u] = true
			}
		}
	}
	out := reqs[:0:0]
	for i, r := range reqs {
		if !drop[i] {
			out = append(out, r)
		}
	}
	return out
}
