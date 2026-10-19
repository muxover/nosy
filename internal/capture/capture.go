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
	Frame     string `json:"frame,omitempty"` // address (scheme and host) of the frame that made the request
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
	Frame     string `json:"frame,omitempty"` // address (scheme and host) of the frame the call ran in
	Detail    string `json:"detail,omitempty"`
}

// CookieWrite is a document.cookie assignment and the script that made it.
type CookieWrite struct {
	Name   string
	Script string
	Frame  string
}

// Popup is a new window the page asked for. Nosy closes it before it loads.
type Popup struct {
	URL    string
	Script string
	Frame  string
}

// Download is a file the page tried to download. Nosy refuses every download.
type Download struct {
	URL      string
	Filename string
	GUID     string
	State    string // the browser's last word on it: inProgress, completed or canceled
}

type Page struct {
	Title           string
	Text            string // visible text, for blocked-page checks
	TextLen         int
	Elements        int
	HTML            string
	Scripts         []string
	Meta            map[string][]string
	Headers         map[string][]string
	ConsentAPIs     []string
	TypedInto       int
	LoadMS          int64
	NeverQuiet      bool
	MainStatus      int
	MainError       string
	FinalURL        string
	ScannedAt       time.Time
	Duration        time.Duration
	Chromium        string
	UserAgent       string
	Device          string // desktop or mobile
	Viewport        string
	GPC             bool // the Global Privacy Control signal was sent
	SizeCapHit      bool
	TimeLimitHit    bool
	Crashed         bool   // the page's renderer crashed during the visit
	PopupsClosed    int    // new windows the page opened, closed before they loaded anything
	StorageCutShort bool   // the storage read ran out of time and shows less than the browser holds
	WebGL           string // hardware, software or none: what the browser gave scripts that ask for WebGL
}

// Capture is everything one visit recorded.
type Capture struct {
	Page         Page
	Requests     []Request
	Cookies      []Cookie
	CookieWrites []CookieWrite
	APICalls     []APICall
	WSFrames     []WSFrame // messages the page sent over WebSockets
	Storage      []Storage // what the page and its frames stored in the browser
	Popups       []Popup
	Downloads    []Download
	LocalNav     []string // addresses on this machine (file:, chrome: and the like) the page tried to open
	ProfileLeft  string   // why the temporary browser profile could not be removed, empty when it was
	Token        string   // text typed into fields for the key-capture check
	Proxy        guard.Stats
}

type WSFrame struct {
	Host    string
	Payload string
}

// Storage is what one frame origin kept in the browser by the end of the visit.
// Third-party frames have storage of their own, so each origin is its own entry.
type Storage struct {
	Origin         string
	Local          []StorageItem
	Session        []StorageItem
	IndexedDB      []IDBDatabase
	Caches         []string
	ServiceWorkers []ServiceWorker
}

// Empty is true when the origin stored nothing.
func (s Storage) Empty() bool {
	return len(s.Local) == 0 && len(s.Session) == 0 && len(s.IndexedDB) == 0 && len(s.Caches) == 0 && len(s.ServiceWorkers) == 0
}

// StorageItem is one key of local or session storage, or one value read from
// IndexedDB under the path Key.
type StorageItem struct {
	Key    string
	Value  string // clipped; used by the checks, never written to a report
	Length int
}

type IDBDatabase struct {
	Name    string
	Version int
	Stores  []IDBStore
}

type IDBStore struct {
	Name    string
	Entries int
	Keys    []string      // the first few keys
	Values  []StorageItem // string values found in the first few entries
}

type ServiceWorker struct {
	Scope  string
	Script string
}
