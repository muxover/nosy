package capture

import (
	"time"

	"github.com/muxover/nosy/internal/guard"
)

// Request is one network request the page made, as the browser saw it.
type Request struct {
	URL       string `json:"url"`
	Host      string `json:"host"`
	Port      string `json:"-"`
	Method    string `json:"method"`
	Type      string `json:"type"`
	Status    int    `json:"status,omitempty"`
	MIME      string `json:"mime,omitempty"`
	Bytes     int64  `json:"bytes"`
	Initiator string `json:"initiator,omitempty"`
	Failed    string `json:"failed,omitempty"`
	Refused   bool   `json:"refused,omitempty"`
	Answered  bool   `json:"answered,omitempty"` // the server replied, but the browser blocked the page from reading it
	IP        string `json:"ip,omitempty"`
	StartMS   int64  `json:"start_ms"`

	// used by the checks, never written to a report
	PostData   string   `json:"-"`
	SetCookies []string `json:"-"`

	// cookies the browser refused to store, with its reason
	BlockedCookies []string `json:"blocked_cookies,omitempty"`
	MainFrame      bool     `json:"-"`
}

// Sent reports whether the destination received the request, which is
// known only when it answered (even if the browser then blocked the page
// from reading the answer). A request still waiting when the visit ended,
// refused, or failed before an answer is not counted.
func (r Request) Sent() bool {
	if r.Refused {
		return false
	}
	return r.Status > 0 || r.Answered
}

type Cookie struct {
	Name     string  `json:"name"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires,omitempty"` // unix seconds, 0 for a session cookie
	Session  bool    `json:"session"`
	Secure   bool    `json:"secure,omitempty"`
	HTTPOnly bool    `json:"http_only,omitempty"`
	SameSite string  `json:"same_site,omitempty"`
	Size     int     `json:"size"`
	Value    string  `json:"-"`
}

// APICall is one instrumented browser API call that matched a
// fingerprinting rule.
type APICall struct {
	Technique string `json:"technique"` // canvas, webgl, audio, fonts
	Script    string `json:"script"`
	Detail    string `json:"detail,omitempty"`
}

// CookieWrite is a document.cookie assignment and the script that made it.
type CookieWrite struct {
	Name   string
	Script string
}

type Page struct {
	Title        string
	Text         string // visible text, for blocked-page checks
	TextLen      int
	Elements     int
	HTML         string
	Scripts      []string
	Meta         map[string][]string
	Headers      map[string][]string
	ConsentAPIs  []string
	TypedInto    int
	LoadMS       int64
	NeverQuiet   bool
	MainStatus   int
	MainError    string
	FinalURL     string
	ScannedAt    time.Time
	Duration     time.Duration
	Chromium     string
	UserAgent    string
	SizeCapHit   bool
	TimeLimitHit bool
	Crashed      bool // the page's renderer crashed during the visit
}

// Capture is everything one visit recorded.
type Capture struct {
	Page         Page
	Requests     []Request
	Cookies      []Cookie
	CookieWrites []CookieWrite
	APICalls     []APICall
	WSFrames     []WSFrame // messages the page sent over WebSockets
	Token        string    // text typed into fields for the key-capture check
	Proxy        guard.Stats
}

type WSFrame struct {
	Host    string
	Payload string
}
