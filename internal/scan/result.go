package scan

import (
	"github.com/muxover/nosy/internal/capture"
	"github.com/muxover/nosy/internal/detect"
	"github.com/muxover/nosy/internal/score"
)

// Schema names the layout of Result in JSON. It is provisional until the
// versioned report in step 10 replaces it.
const Schema = "nosy-scan/0"

type Status string

const (
	StatusDone    Status = "done"
	StatusBlocked Status = "blocked" // a bot check, an error page or an empty page: nothing to score
	StatusFailed  Status = "failed"  // the page could not be loaded
)

type Kind string

const (
	KindBotCheck   Kind = "bot_check"
	KindUnfinished Kind = "unfinished"
)

// Labels say how a finding is known.
const (
	// Observed: the browser saw it happen.
	Observed = "observed"
	// Classified: observed, then matched against a rule or a list.
	Classified = "classified"
	// Inferred: concluded from the pattern of what was seen.
	Inferred = "inferred"
)

// Result is one finished scan.
type Result struct {
	Schema       string `json:"schema"`
	Status       Status `json:"status"`
	Kind         Kind   `json:"kind,omitempty"`
	Reason       string `json:"reason,omitempty"`
	RequestedURL string `json:"requested_url"`
	FinalURL     string `json:"final_url,omitempty"`
	// Site is the registrable domain the result is about: where the visit ended.
	Site       string     `json:"site"`
	Conditions Conditions `json:"conditions"`

	Score    *score.Result `json:"score,omitempty"`
	Finding  string        `json:"finding,omitempty"`
	Findings []Finding     `json:"findings"`

	Page             Page               `json:"page"`
	Weight           Weight             `json:"weight"`
	Companies        []detect.Company   `json:"companies"`
	Requests         []capture.Request  `json:"requests"`
	Cookies          []Cookie           `json:"cookies"`
	Storage          []StorageOrigin    `json:"storage"`
	Fingerprinting   []detect.Technique `json:"fingerprinting"`
	SessionRecording []Evidence         `json:"session_recording"`
	TypedText        TypedText          `json:"typed_text"`
	Consent          detect.Consent     `json:"consent"`
	Notes            []string           `json:"notes,omitempty"`
}

// Conditions are the circumstances of a scan. A score means little without them.
type Conditions struct {
	Date    string `json:"date"`
	Country string `json:"country"`
	Browser string `json:"browser"`
	// BrowserSource is why this browser was used: config, env, pinned or system.
	BrowserSource string `json:"browser_source"`
	// BrowserPinned is true when it was Nosy's own Chrome for Testing build.
	BrowserPinned        bool     `json:"browser_pinned"`
	UserAgent            string   `json:"user_agent"`
	Device               string   `json:"device"`
	Viewport             string   `json:"viewport"`
	Locale               string   `json:"locale"`
	ConsentMode          string   `json:"consent_mode"`
	GPC                  bool     `json:"gpc"`
	DurationLimitSeconds int      `json:"duration_limit_seconds"`
	DurationMS           int64    `json:"duration_ms"`
	Nosy                 string   `json:"nosy"`
	Rules                string   `json:"rules"`
	Settings             []string `json:"settings"`
}

// Finding is one thing the scan found, in a sentence, with how it is known.
type Finding struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Text     string   `json:"text"`
	Evidence []string `json:"evidence,omitempty"`
}

type Page struct {
	Status     int   `json:"status"`
	TextLen    int   `json:"text_length"`
	Elements   int   `json:"elements"`
	LoadMS     int64 `json:"load_ms"`
	NeverQuiet bool  `json:"never_quiet,omitempty"`
}

type Weight struct {
	Requests      int   `json:"requests"`
	ThirdRequests int   `json:"third_party_requests"`
	Bytes         int64 `json:"bytes"`
	ThirdBytes    int64 `json:"third_party_bytes"`
}

type Cookie struct {
	Name       string `json:"name"`
	Domain     string `json:"domain"`
	ThirdParty bool   `json:"third_party"`
	Session    bool   `json:"session"`
	Days       int    `json:"days,omitempty"`
	Length     int    `json:"length"`
	Identifier bool   `json:"identifier"`
	SetBy      string `json:"set_by,omitempty"`
}

// Evidence points at the request that shows something.
type Evidence struct {
	Request int    `json:"request"`
	URL     string `json:"url"`
}

type TypedText struct {
	FieldsTested int      `json:"fields_tested"`
	Hits         []KeyHit `json:"hits,omitempty"`
}

type KeyHit struct {
	detect.KeyHit
	URL string `json:"url,omitempty"`
}

// StorageOrigin is what one frame origin kept in the browser. Third-party
// frames keep storage of their own, so each origin stands apart.
type StorageOrigin struct {
	Origin         string          `json:"origin"`
	ThirdParty     bool            `json:"third_party"`
	LocalStorage   []StorageKey    `json:"local_storage,omitempty"`
	SessionStorage []StorageKey    `json:"session_storage,omitempty"`
	IndexedDB      []IDB           `json:"indexed_db,omitempty"`
	CacheStorage   []string        `json:"cache_storage,omitempty"`
	ServiceWorkers []ServiceWorker `json:"service_workers,omitempty"`
}

// StorageKey is one key of local or session storage. The value is not kept.
// Identifier is true when the value looks like an identifier by the cookie
// rule; a session-storage value is gone with the tab, so it never is.
type StorageKey struct {
	Key        string `json:"key"`
	Length     int    `json:"length"`
	Identifier bool   `json:"identifier"`
}

type IDB struct {
	Name    string     `json:"name"`
	Version int        `json:"version"`
	Stores  []IDBStore `json:"stores"`
}

type IDBStore struct {
	Name    string   `json:"name"`
	Entries int      `json:"entries"`
	Keys    []string `json:"keys,omitempty"`
	// IdentifierValues names the fields (or "value") of the first entries that look like identifiers.
	IdentifierValues []string `json:"identifier_values,omitempty"`
}

type ServiceWorker struct {
	Scope  string `json:"scope"`
	Script string `json:"script,omitempty"`
}

// StorageCounts adds up the stored data: origins that kept any, how many of
// them are third parties, and how many stored values look like identifiers.
func (r *Result) StorageCounts() (origins, thirdParty, identifiers int) {
	for _, o := range r.Storage {
		origins++
		if o.ThirdParty {
			thirdParty++
		}
		identifiers += o.identifiers()
	}
	return
}

func (o StorageOrigin) identifiers() int {
	n := 0
	for _, k := range o.LocalStorage {
		if k.Identifier {
			n++
		}
	}
	for _, db := range o.IndexedDB {
		for _, st := range db.Stores {
			n += len(st.IdentifierValues)
		}
	}
	return n
}
