package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/inspector"
	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/muxover/nosy/internal/capture"
	"github.com/muxover/nosy/internal/guard"
)

func (v *visit) touch() {
	v.lastNet.Store(time.Now().UnixNano())
	v.sawNet.Store(true)
}

func (v *visit) listen(ctx context.Context, stop context.CancelFunc) {
	v.stop = stop
	v.o.Proxy.OnCap(func() {
		v.mu.Lock()
		v.c.Page.SizeCapHit = true
		v.mu.Unlock()
		stop()
	})
	chromedp.ListenTarget(ctx, v.handler(ctx, ""))
}

// handler reads the events of one target. Request ids are only unique inside
// a target, so every key carries the target's own prefix; the page has none.
func (v *visit) handler(ctx context.Context, session string) func(ev any) {
	key := func(id network.RequestID) string { return session + string(id) }
	return func(ev any) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			v.onRequest(key(e.RequestID), e)
		case *network.EventResponseReceived:
			v.onResponse(key(e.RequestID), e)
		case *network.EventResponseReceivedExtraInfo:
			v.onExtraInfo(key(e.RequestID), e)
		case *network.EventDataReceived:
			v.mu.Lock()
			k := v.kind[key(e.RequestID)]
			v.mu.Unlock()
			if k != network.ResourceTypeWebSocket && k != network.ResourceTypeEventSource {
				v.touch()
			}
		case *network.EventLoadingFinished:
			v.touch()
			v.update(key(e.RequestID), func(r *capture.Request) { r.Bytes = int64(e.EncodedDataLength) })
		case *network.EventLoadingFailed:
			v.touch()
			v.update(key(e.RequestID), func(r *capture.Request) {
				if answeredThenBlocked(e) {
					r.Answered = true
				}
				if r.Status == 0 {
					r.Failed = e.ErrorText
				}
			})
		case *network.EventWebSocketCreated:
			v.touch()
			v.addRequest(key(e.RequestID), e.URL, "GET", "WebSocket", "", false)
		case *network.EventWebSocketHandshakeResponseReceived:
			v.update(key(e.RequestID), func(r *capture.Request) { r.Status = int(e.Response.Status) })
		case *network.EventWebSocketFrameSent:
			if e.Response != nil {
				v.mu.Lock()
				if len(v.c.WSFrames) < 500 {
					host := ""
					if i, ok := v.cur[key(e.RequestID)]; ok {
						host = v.reqs[i].Host
					}
					v.c.WSFrames = append(v.c.WSFrames, capture.WSFrame{Host: host, Payload: clip(e.Response.PayloadData, maxPostData)})
				}
				v.mu.Unlock()
			}
		case *page.EventFrameRequestedNavigation:
			v.onNavigationRequest(e.URL)
		case *cdplog.EventEntryAdded:
			// the browser says so when a page reaches for a local file
			if _, raw, ok := strings.Cut(e.Entry.Text, "Not allowed to load local resource: "); ok {
				v.onNavigationRequest(strings.TrimSpace(raw))
			}
		case *page.EventLoadEventFired:
			if session == "" {
				v.loadMS.Store(time.Since(v.t0).Milliseconds())
			}
		case *page.EventFrameNavigated:
			if session == "" && e.Frame != nil && e.Frame.ParentID == "" {
				v.mu.Lock()
				v.c.Page.FinalURL = e.Frame.URL
				v.mu.Unlock()
			}
		case *inspector.EventTargetCrashed:
			if session == "" {
				v.mu.Lock()
				v.c.Page.Crashed = true
				v.mu.Unlock()
				v.stop()
			}
		case *page.EventJavascriptDialogOpening:
			// an alert would hold the page; dismiss it without accepting anything
			go func() {
				_ = chromedp.Run(ctx, page.HandleJavaScriptDialog(false))
			}()
		case *runtime.EventBindingCalled:
			if e.Name == bindingName {
				v.onBinding(e.Payload)
			}
		}
	}
}

func (v *visit) onRequest(key string, e *network.EventRequestWillBeSent) {
	v.touch()
	if e.Request == nil {
		return
	}
	main := e.Type == network.ResourceTypeDocument && e.FrameID != "" && string(e.FrameID) == v.frameID()
	v.mu.Lock()
	if e.RedirectResponse != nil {
		if i, ok := v.cur[key]; ok {
			r := v.reqs[i]
			r.Status = int(e.RedirectResponse.Status)
		}
		if main {
			v.redirects[key]++
			if v.redirects[key] > guard.MaxRedirects && v.c.Page.MainError == "" {
				// the page is the one thing a visitor came for: a chain longer than the
				// limit is a loop or a trap, and the visit ends here
				v.c.Page.MainError = "net::ERR_TOO_MANY_REDIRECTS"
				defer v.stop()
			}
		}
	}
	v.mu.Unlock()
	initiator := ""
	if e.Initiator != nil {
		initiator = initiatorURL(e.Initiator)
	}
	id := v.addRequest(key, e.Request.URL, e.Request.Method, string(e.Type), initiator, main)
	if id >= 0 {
		v.mu.Lock()
		v.reqs[id].Frame = originOf(e.DocumentURL)
		v.mu.Unlock()
	}
	if id >= 0 && len(e.Request.PostDataEntries) > 0 {
		v.mu.Lock()
		v.reqs[id].PostData = decodeEntries(e.Request.PostDataEntries)
		v.mu.Unlock()
	}
}

// originOf is the origin of a document address, empty for anything but http and https.
func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + strings.ToLower(u.Host)
}

func (v *visit) frameID() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return string(v.mainFrame)
}

func initiatorURL(i *network.Initiator) string {
	if i.Type == network.InitiatorTypeScript && i.Stack != nil {
		for s := i.Stack; s != nil; s = s.Parent {
			for _, f := range s.CallFrames {
				if f.URL != "" {
					return f.URL
				}
			}
		}
	}
	if i.URL != "" {
		return i.URL
	}
	return string(i.Type)
}

func (v *visit) addRequest(key, rawURL, method, typ, initiator string, main bool) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.kind[key] = network.ResourceType(typ)
	if len(v.reqs) >= maxRequests {
		return -1
	}
	rawURL, _, _ = strings.Cut(rawURL, "#")
	r := &capture.Request{
		URL: clip(rawURL, 600), Method: method, Type: typ, Initiator: initiator,
		MainFrame: main, StartMS: time.Since(v.t0).Milliseconds(),
	}
	if u, err := url.Parse(rawURL); err == nil {
		r.Host = strings.ToLower(u.Hostname())
		r.Port = u.Port()
		if r.Port == "" {
			switch u.Scheme {
			case "http", "ws":
				r.Port = "80"
			case "https", "wss":
				r.Port = "443"
			}
		}
	}
	v.reqs = append(v.reqs, r)
	v.cur[key] = len(v.reqs) - 1
	return len(v.reqs) - 1
}

func (v *visit) update(key string, fn func(*capture.Request)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if i, ok := v.cur[key]; ok {
		fn(v.reqs[i])
	}
}

func (v *visit) onResponse(key string, e *network.EventResponseReceived) {
	v.touch()
	if e.Response == nil {
		return
	}
	v.update(key, func(r *capture.Request) {
		r.Status = int(e.Response.Status)
		r.MIME = e.Response.MimeType
		if hv, ok := headerValue(e.Response.Headers, guard.UpstreamErrorHeader); ok && hv != "" {
			r.Status, r.Failed = 0, "could not connect to the site"
		}
		if hv, ok := headerValue(e.Response.Headers, guard.RefusedHeader); ok && hv != "" {
			r.Refused = true
		}
	})
	if e.Type == network.ResourceTypeDocument && string(e.FrameID) == v.frameID() {
		v.mu.Lock()
		v.c.Page.Headers = lowerHeaders(e.Response.Headers)
		v.mu.Unlock()
	}
}

func (v *visit) onExtraInfo(key string, e *network.EventResponseReceivedExtraInfo) {
	for _, b := range e.BlockedCookies {
		v.logf("blocked cookie %s: %v", b.CookieLine, b.BlockedReasons)
	}
	v.update(key, func(r *capture.Request) {
		for _, b := range e.BlockedCookies {
			name := ""
			if b.Cookie != nil {
				name = b.Cookie.Name
			} else if n, _, ok := strings.Cut(b.CookieLine, "="); ok {
				name = n
			}
			r.BlockedCookies = append(r.BlockedCookies, name+": "+fmt.Sprint(b.BlockedReasons))
		}
		if sc, ok := headerValue(e.Headers, "set-cookie"); ok {
			for _, line := range strings.Split(sc, "\n") {
				name, _, _ := strings.Cut(line, "=")
				if name = strings.TrimSpace(name); name != "" {
					r.SetCookies = append(r.SetCookies, name)
				}
			}
		}
	})
}

func headerValue(h network.Headers, name string) (string, bool) {
	for k, v := range h {
		if strings.EqualFold(k, name) {
			if s, ok := v.(string); ok {
				return s, true
			}
		}
	}
	return "", false
}

func lowerHeaders(h network.Headers) map[string][]string {
	out := map[string][]string{}
	for k, v := range h {
		if s, ok := v.(string); ok {
			out[strings.ToLower(k)] = strings.Split(s, "\n")
		}
	}
	return out
}

type bindingMsg struct {
	T      string `json:"t"`
	Tech   string `json:"tech"`
	Script string `json:"script"`
	Detail string `json:"detail"`
	Name   string `json:"name"`
	Frame  string `json:"frame"`
	URL    string `json:"url"`
}

// onNavigationRequest notes a page trying to open an address on this machine.
// The browser refuses these itself; Nosy records the attempt.
func (v *visit) onNavigationRequest(raw string) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return
	}
	switch strings.ToLower(u.Scheme) {
	case "file", "filesystem", "chrome", "chrome-untrusted", "chrome-extension", "devtools", "view-source", "ftp":
	default:
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.c.LocalNav) < maxPopups {
		v.c.LocalNav = append(v.c.LocalNav, clip(raw, 300))
	}
}

func (v *visit) onBinding(payload string) {
	var m bindingMsg
	if json.Unmarshal([]byte(payload), &m) != nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	script := m.Script
	if script == "inline" && m.Frame != "" && m.Frame != "null" {
		script = "inline in " + m.Frame
	}
	switch m.T {
	case "fp":
		v.c.APICalls = append(v.c.APICalls, capture.APICall{Technique: m.Tech, Script: script, Frame: m.Frame, Detail: m.Detail})
	case "cookie":
		v.c.CookieWrites = append(v.c.CookieWrites, capture.CookieWrite{Name: m.Name, Script: script, Frame: m.Frame})
	case "popup":
		if len(v.c.Popups) < maxPopups {
			v.c.Popups = append(v.c.Popups, capture.Popup{URL: m.URL, Script: script, Frame: m.Frame})
		}
	}
}

// answeredThenBlocked is true when the failure came after the server had
// replied: the browser's own read protections (ORB, CORP, CORS, content
// type) stop the page from using the answer, not the request from going out.
func answeredThenBlocked(e *network.EventLoadingFailed) bool {
	if e.CorsErrorStatus != nil {
		return true
	}
	switch e.ErrorText {
	case "net::ERR_BLOCKED_BY_ORB", "net::ERR_BLOCKED_BY_RESPONSE",
		// the peer answered, then the stream or the body broke
		"net::ERR_HTTP2_PROTOCOL_ERROR", "net::ERR_CONTENT_LENGTH_MISMATCH", "net::ERR_INCOMPLETE_CHUNKED_ENCODING":
		return true
	}
	br := string(e.BlockedReason)
	return br == "origin" || br == "content-type" || strings.HasPrefix(br, "corp")
}
