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
	Fingerprinting   []detect.Technique `json:"fingerprinting"`
	SessionRecording []Evidence         `json:"session_recording"`
	TypedText        TypedText          `json:"typed_text"`
	Consent          detect.Consent     `json:"consent"`
	Notes            []string           `json:"notes,omitempty"`
}

// Conditions are the circumstances of a scan. A score means little without them.
type Conditions struct {
	Date                 string   `json:"date"`
	Country              string   `json:"country"`
	Browser              string   `json:"browser"`
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
