package detect

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/muxover/nosy/internal/capture"
)

type Technique struct {
	Name    string   `json:"technique"`
	Scripts []string `json:"scripts"`
	// Frames are the addresses (scheme and host) of the frames the calls ran in.
	Frames  []string `json:"frames,omitempty"`
	Calls   int      `json:"calls"`
	Details []string `json:"details,omitempty"`
}

var techniqueOrder = []string{"canvas", "webgl", "audio", "fonts"}

// Fingerprinting groups the instrumented API calls by technique. The page
// script only reports calls that already match a rule, so every call here
// counts.
func Fingerprinting(calls []capture.APICall) []Technique {
	by := map[string]*Technique{}
	for _, c := range calls {
		t := by[c.Technique]
		if t == nil {
			t = &Technique{Name: c.Technique}
			by[c.Technique] = t
		}
		t.Calls++
		t.Scripts = appendUnique(t.Scripts, c.Script)
		if c.Frame != "" && c.Frame != "null" {
			t.Frames = appendUnique(t.Frames, c.Frame)
		}
		if c.Detail != "" && len(t.Details) < 3 {
			t.Details = appendUnique(t.Details, c.Detail)
		}
	}
	var out []Technique
	for _, name := range techniqueOrder {
		if t := by[name]; t != nil {
			sort.Strings(t.Scripts)
			sort.Strings(t.Frames)
			out = append(out, *t)
		}
	}
	return out
}

func TechniqueNames(ts []Technique) []string {
	names := make([]string, len(ts))
	for i, t := range ts {
		names[i] = t.Name
	}
	return names
}

type KeyHit struct {
	Request    int    `json:"request"` // index into the requests, -1 for a WebSocket frame
	Form       string `json:"form"`    // plain, base64, md5 or sha256
	ThirdParty bool   `json:"third_party"`
}

// KeyCapture looks for the typed test string, or its Base64, MD5 or
// SHA-256 form, in the URL or body of any request and in WebSocket frames.
func KeyCapture(token, siteName string, reqs []capture.Request, ws []capture.WSFrame, radar *Index) []KeyHit {
	if token == "" {
		return nil
	}
	forms := tokenForms(token)
	var hits []KeyHit
	for i, r := range reqs {
		if !r.Sent() {
			continue
		}
		hay := r.URL + "\n" + r.PostData
		if u, err := url.QueryUnescape(r.URL); err == nil {
			hay += "\n" + u
		}
		if form, ok := firstForm(hay, forms); ok {
			reg := Registrable(r.Host)
			third := reg != siteName && !radar.SameOwner(reg, siteName)
			hits = append(hits, KeyHit{Request: i, Form: form, ThirdParty: third})
		}
	}
	for _, f := range ws {
		if form, ok := firstForm(f.Payload, forms); ok {
			reg := Registrable(f.Host)
			third := f.Host != "" && reg != siteName && !radar.SameOwner(reg, siteName)
			hits = append(hits, KeyHit{Request: -1, Form: form, ThirdParty: third})
		}
	}
	return hits
}

type tokenForm struct{ name, text string }

func tokenForms(token string) []tokenForm {
	md := md5.Sum([]byte(token))
	sh := sha256.Sum256([]byte(token))
	b := []byte(token)
	return []tokenForm{
		{"plain", token},
		{"base64", base64.StdEncoding.EncodeToString(b)},
		{"base64", base64.RawStdEncoding.EncodeToString(b)},
		{"base64", base64.URLEncoding.EncodeToString(b)},
		{"base64", base64.RawURLEncoding.EncodeToString(b)},
		{"md5", hex.EncodeToString(md[:])},
		{"sha256", hex.EncodeToString(sh[:])},
	}
}

func firstForm(hay string, forms []tokenForm) (string, bool) {
	lower := strings.ToLower(hay)
	for _, f := range forms {
		if strings.Contains(hay, f.text) {
			return f.name, true
		}
		if f.name == "md5" || f.name == "sha256" {
			if strings.Contains(lower, f.text) {
				return f.name, true
			}
		}
	}
	return "", false
}

type Consent struct {
	Present bool     `json:"present"`
	Via     []string `json:"via,omitempty"`
}

// DetectConsent says whether the page has a consent platform: a consent API
// it exposes, or the name of a cookie-compliance product found on it.
func DetectConsent(apis, platforms []string) Consent {
	c := Consent{}
	for _, a := range apis {
		c.Via = appendUnique(c.Via, a)
	}
	for _, p := range platforms {
		c.Via = appendUnique(c.Via, p)
	}
	sort.Strings(c.Via)
	c.Present = len(c.Via) > 0
	return c
}

var challengeMarkers = []string{
	"just a moment...",
	"attention required! | cloudflare",
	"checking your browser before accessing",
	"verify you are human",
	"are you a human",
	"press & hold to confirm you are",
	"pardon our interruption",
	"access denied",
	"request unsuccessful. incapsula",
	"enable javascript and cookies to continue",
	"please enable cookies",
	"unusual traffic from your computer network",
	"captcha-delivery.com",
	"px-captcha",
	"_incapsula_resource",
	"cf-browser-verification",
	"you have been blocked",
	"sorry, we just need to make sure you're not a robot",
	"enter the characters you see below",
	"type the characters you see in this image",
	"to discuss automated access to amazon data",
	"verifying you are human",
	"performing security verification",
}

// Blocked decides whether the scan saw a bot challenge, an error page or
// an empty page instead of the site. It returns the reason, or "".
func Blocked(pg capture.Page) string {
	text := strings.ToLower(pg.Title + "\n" + pg.Text)
	if len(text) > 6000 {
		text = text[:6000]
	}
	for _, m := range challengeMarkers {
		if strings.Contains(text, m) && pg.TextLen < 2000 {
			return "the site showed a bot check"
		}
	}
	if pg.MainStatus >= 400 {
		return "the site answered with an error page (HTTP " + strconv.Itoa(pg.MainStatus) + ")"
	}
	if pg.TextLen < 10 && pg.Elements < 10 {
		return reasonEmpty
	}
	return ""
}

// challengeHosts serve the scripts of bot-check pages.
var challengeHosts = []string{
	"token.awswaf.com", "challenges.cloudflare.com", "captcha-delivery.com",
	"perimeterx.net", "px-cloud.net", "px-cdn.net", "hcaptcha.com", "arkoselabs.com", "funcaptcha.com",
}

// BotCheckScripts catches a bot-check page that has little text of its own:
// a small page that calls a verification endpoint, or whose scripts come from
// a bot-check service.
func BotCheckScripts(pg capture.Page, reqs []capture.Request) string {
	if pg.TextLen >= 1500 {
		return ""
	}
	for _, r := range reqs {
		if r.Sent() && strings.Contains(r.URL, "/_sec/verify") {
			return "the site showed a bot check"
		}
	}
	for _, r := range reqs {
		if r.Type != "Script" || !r.Sent() {
			continue
		}
		for _, h := range challengeHosts {
			if r.Host == h || strings.HasSuffix(r.Host, "."+h) {
				return "the site showed a bot check"
			}
		}
	}
	return ""
}
