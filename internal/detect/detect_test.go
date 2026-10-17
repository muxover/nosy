package detect

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/muxover/nosy/internal/capture"
)

func testIndex() *Index {
	return NewIndex(map[string]Domain{
		"adnet.test":     {Owner: "AdNet Inc", Display: "AdNet", Categories: []string{"Advertising", "Ad Motivated Tracking"}},
		"adnet-cdn.test": {Owner: "AdNet Inc", Display: "AdNet", Categories: []string{"CDN"}},
		"stats.test":     {Owner: "Stats Ltd", Display: "Stats", Categories: []string{"Analytics"}},
		"social.test":    {Owner: "Social Co", Display: "Social", Categories: []string{"Social - Share"}},
		"replay.test":    {Owner: "Replay Co", Display: "Replay", Categories: []string{"Session Replay"}},
		"cdn.test":       {Owner: "CDN Co", Display: "CDN Co", Categories: []string{"CDN"}},
		"tagman.test":    {Owner: "Tag Co", Display: "Tag", Categories: []string{"Tag Manager"}},
		"sister.test":    {Owner: "Site Corp", Display: "Site Corp", Categories: []string{"Analytics"}},
	}, map[string][]string{
		"Site Corp": {"site.test", "sister.test"},
	})
}

func get(url, host, typ string) capture.Request {
	return capture.Request{URL: url, Host: host, Type: typ, Method: "GET", Status: 200}
}

func TestAnalyzeCountsCompanies(t *testing.T) {
	reqs := []capture.Request{
		get("https://www.site.test/", "www.site.test", "Document"),
		get("https://static.site.test/a.js", "static.site.test", "Script"),
		get("https://ads.adnet.test/px", "ads.adnet.test", "Image"),
		get("https://ads.adnet.test/px2", "ads.adnet.test", "Image"),
		get("https://x.adnet-cdn.test/lib.js", "x.adnet-cdn.test", "Script"),
		get("https://stats.test/collect", "stats.test", "XHR"),
		get("https://cdn.test/lib.js", "cdn.test", "Script"),
		get("https://tagman.test/gtm.js", "tagman.test", "Script"),
		get("https://unknown-thing.test/x", "unknown-thing.test", "Image"),
		get("https://sister.test/a.js", "sister.test", "Script"),
		get("data:image/png;base64,AAAA", "", "Image"),
	}
	p := Analyze("site.test", reqs, testIndex(), noVendors(), nil)
	if p.Tracking != 2 {
		t.Errorf("tracking companies = %d, want 2 (AdNet, Stats)", p.Tracking)
	}
	if !p.AdTech {
		t.Error("ad tech not detected")
	}
	wantThird := []bool{false, false, true, true, true, true, true, true, true, false, false}
	for i, w := range wantThird {
		if p.Per[i].ThirdParty != w {
			t.Errorf("request %d third party = %v, want %v", i, p.Per[i].ThirdParty, w)
		}
	}
	byName := map[string]Company{}
	for _, c := range p.Companies {
		byName[c.Name] = c
	}
	ad := byName["AdNet"]
	if ad.Requests != 3 || !ad.Tracking || len(ad.Domains) != 2 {
		t.Errorf("AdNet = %+v", ad)
	}
	if c := byName["CDN Co"]; c.Tracking {
		t.Error("a CDN was counted as tracking")
	}
	if c := byName["Tag"]; c.Tracking {
		t.Error("a tag manager was counted as tracking")
	}
	if c, ok := byName["unknown-thing.test"]; !ok || c.Tracking {
		t.Errorf("unlisted third party = %+v, %v", c, ok)
	}
	if _, ok := byName["Site Corp"]; ok {
		t.Error("a domain owned by the site's own company was listed as third party")
	}
	if p.Companies[0].Tracking != true {
		t.Error("tracking companies should be listed first")
	}
}

func TestAnalyzeSkipsRequestsThatWereNotSent(t *testing.T) {
	failed := get("https://ads.adnet.test/px", "ads.adnet.test", "Image")
	failed.Status, failed.Failed = 0, "net::ERR_NAME_NOT_RESOLVED"
	refused := get("https://stats.test/x", "stats.test", "XHR")
	refused.Refused = true
	waiting := get("https://replay.test/r.js", "replay.test", "Script")
	waiting.Status = 0
	blocked := get("https://social.test/s.js", "social.test", "Script")
	blocked.Status, blocked.Failed, blocked.Answered = 0, "net::ERR_BLOCKED_BY_ORB", true
	p := Analyze("site.test", []capture.Request{failed, refused, waiting, blocked}, testIndex(), noVendors(), nil)
	if p.Tracking != 1 || p.AdTech {
		t.Errorf("tracking = %d adtech = %v, want only the company whose answer the browser blocked", p.Tracking, p.AdTech)
	}
}

func TestEntity(t *testing.T) {
	r := testIndex()
	if !r.SameOwner("site.test", "sister.test") || r.SameOwner("site.test", "adnet.test") {
		t.Error("SameOwner wrong")
	}
	if r.SameOwner("unknown.test", "unknown.test") {
		t.Error("unknown domains must not match each other by owner")
	}
}

func TestIdentifierCookie(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	year := float64(now.Unix() + 365*86400)
	cases := []struct {
		name string
		c    capture.Cookie
		want bool
	}{
		{"random hex id", capture.Cookie{Value: "9f86d081884c7d659a2feaa0c55ad015", Expires: year}, true},
		{"ga client id", capture.Cookie{Value: "GA1.2.1234567890.1712345678", Expires: year}, true},
		{"uuid", capture.Cookie{Value: "3f2b8a4e-9c1d-4e7a-b2f0-6d5a1c8e9b73", Expires: year}, true},
		{"session cookie", capture.Cookie{Value: "9f86d081884c7d659a2feaa0c55ad015", Session: true}, false},
		{"expires in 12 hours", capture.Cookie{Value: "9f86d081884c7d659a2feaa0c55ad015", Expires: float64(now.Unix() + 12*3600)}, false},
		{"expires in exactly a day", capture.Cookie{Value: "9f86d081884c7d659a2feaa0c55ad015", Expires: float64(now.Unix() + 86400)}, false},
		{"too short", capture.Cookie{Value: "a1B2c3D", Expires: year}, false},
		{"low entropy", capture.Cookie{Value: "aaaaaaaaaaaaaaaa", Expires: year}, false},
		{"word", capture.Cookie{Value: "accepted-all", Expires: year}, false},
		{"epoch seconds", capture.Cookie{Value: "1712345678", Expires: year}, false},
		{"epoch millis", capture.Cookie{Value: "1712345678123", Expires: year}, false},
		{"date with month name", capture.Cookie{Value: "01-Oct-2026", Expires: year}, false},
		{"iso date", capture.Cookie{Value: "2026-10-01", Expires: year}, false},
		{"iso timestamp", capture.Cookie{Value: "2026-10-01T10:15:30Z", Expires: year}, false},
		{"compact date", capture.Cookie{Value: "20261001", Expires: year}, false},
		{"http date", capture.Cookie{Value: "Thu, 01 Oct 2026 10:15:30 GMT", Expires: year}, false},
		{"settings string", capture.Cookie{Value: "isGpcEnabled=0&datestamp=Mon+Jan+01&version=6.9.0", Expires: year}, false},
		{"json", capture.Cookie{Value: `{"a":"9f86d081884c7d65","b":1}`, Expires: year}, false},
		{"url encoded json", capture.Cookie{Value: "%7B%22a%22%3A%229f86d081884c7d65%22%7D", Expires: year}, false},
		{"no expiry info", capture.Cookie{Value: "9f86d081884c7d659a2feaa0c55ad015"}, false},
	}
	for _, c := range cases {
		if got := IsIdentifier(c.c, now); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestEntropy(t *testing.T) {
	if Entropy("") != 0 || Entropy("aaaa") != 0 {
		t.Error("constant strings have no entropy")
	}
	if e := Entropy("abcd"); e < 1.99 || e > 2.01 {
		t.Errorf("Entropy(abcd) = %v, want 2", e)
	}
}

func TestKeyCaptureForms(t *testing.T) {
	token := "nosy-probe-3fa91c07"
	md := md5.Sum([]byte(token))
	sh := sha256.Sum256([]byte(token))
	b64 := base64.StdEncoding.EncodeToString([]byte(token))
	cases := []struct {
		name string
		req  capture.Request
		form string
	}{
		{"plain in url", capture.Request{URL: "https://t.test/c?q=" + token}, "plain"},
		{"plain in body", capture.Request{URL: "https://t.test/c", PostData: `{"v":"` + token + `"}`}, "plain"},
		{"base64 in body", capture.Request{URL: "https://t.test/c", PostData: "d=" + b64}, "base64"},
		{"md5 in url", capture.Request{URL: "https://t.test/c?h=" + hex.EncodeToString(md[:])}, "md5"},
		{"sha256 upper", capture.Request{URL: "https://t.test/c?h=" + upper(hex.EncodeToString(sh[:]))}, "sha256"},
	}
	for _, c := range cases {
		c.req.Host, c.req.Status = "t.test", 200
		hits := KeyCapture(token, "site.test", []capture.Request{c.req}, nil, testIndex())
		if len(hits) != 1 || hits[0].Form != c.form || !hits[0].ThirdParty {
			t.Errorf("%s: %+v", c.name, hits)
		}
	}
	clean := capture.Request{URL: "https://t.test/c?q=other", PostData: "nothing here", Host: "t.test", Status: 200}
	if hits := KeyCapture(token, "site.test", []capture.Request{clean}, nil, testIndex()); len(hits) != 0 {
		t.Errorf("clean request matched: %+v", hits)
	}
	if hits := KeyCapture(token, "site.test", nil, []capture.WSFrame{{Host: "t.test", Payload: "frame " + token}}, testIndex()); len(hits) != 1 || hits[0].Request != -1 || !hits[0].ThirdParty {
		t.Errorf("websocket: %+v", hits)
	}
	if hits := KeyCapture("", "site.test", []capture.Request{clean}, nil, testIndex()); hits != nil {
		t.Error("empty token matched")
	}
	own := capture.Request{URL: "https://www.site.test/suggest?q=" + token, Host: "www.site.test", Status: 200}
	if hits := KeyCapture(token, "site.test", []capture.Request{own}, nil, testIndex()); len(hits) != 1 || hits[0].ThirdParty {
		t.Errorf("first party hit should be reported as first party: %+v", hits)
	}
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func TestFingerprintingGroupsByTechnique(t *testing.T) {
	calls := []capture.APICall{
		{Technique: "webgl", Script: "https://a.test/fp.js"},
		{Technique: "canvas", Script: "https://a.test/fp.js", Detail: "240x60"},
		{Technique: "canvas", Script: "https://b.test/fp.js"},
		{Technique: "canvas", Script: "https://b.test/fp.js"},
	}
	got := Fingerprinting(calls)
	names := TechniqueNames(got)
	if len(names) != 2 || names[0] != "canvas" || names[1] != "webgl" {
		t.Fatalf("techniques = %v", names)
	}
	if got[0].Calls != 3 || len(got[0].Scripts) != 2 {
		t.Errorf("canvas = %+v", got[0])
	}
}

func TestConsent(t *testing.T) {
	c := DetectConsent([]string{"__tcfapi"}, []string{"OneTrust"})
	if !c.Present || len(c.Via) != 2 {
		t.Errorf("consent = %+v", c)
	}
	if DetectConsent(nil, nil).Present {
		t.Error("consent reported without a platform")
	}
}

func TestBlocked(t *testing.T) {
	ok := capture.Page{MainStatus: 200, Title: "Example", Text: "Welcome to a real page with plenty of words on it", TextLen: 900, Elements: 120}
	cases := []struct {
		name string
		pg   capture.Page
		want bool
	}{
		{"normal page", ok, false},
		{"403", capture.Page{MainStatus: 403, TextLen: 500, Elements: 50}, true},
		{"503", capture.Page{MainStatus: 503, TextLen: 500, Elements: 50}, true},
		{"404", capture.Page{MainStatus: 404, TextLen: 500, Elements: 50}, true},
		{"cloudflare", capture.Page{MainStatus: 200, Title: "Just a moment...", Text: "Checking your browser", TextLen: 60, Elements: 30}, true},
		{"incapsula", capture.Page{MainStatus: 200, Text: "Request unsuccessful. Incapsula incident ID: 1", TextLen: 50, Elements: 8}, true},
		{"empty", capture.Page{MainStatus: 200, TextLen: 0, Elements: 4}, true},
		{"marker in a long real page", capture.Page{MainStatus: 200, Text: "we mention access denied in an article", TextLen: 9000, Elements: 400}, false},
		{"tiny but real", capture.Page{MainStatus: 200, Text: "hello world, this page is small", TextLen: 31, Elements: 6}, false},
	}
	for _, c := range cases {
		if got := Blocked(c.pg) != ""; got != c.want {
			t.Errorf("%s: blocked = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSent(t *testing.T) {
	cases := []struct {
		name string
		r    capture.Request
		want bool
	}{
		{"answered", capture.Request{Status: 200}, true},
		{"error status still answered", capture.Request{Status: 404}, true},
		{"still waiting when the visit ended", capture.Request{}, false},
		{"blocked after the reply (ORB)", capture.Request{Failed: "net::ERR_BLOCKED_BY_ORB", Answered: true}, true},
		{"never connected", capture.Request{Failed: "net::ERR_CONNECTION_REFUSED"}, false},
		{"name not found", capture.Request{Failed: "net::ERR_NAME_NOT_RESOLVED"}, false},
		{"aborted with no reply", capture.Request{Failed: "net::ERR_ABORTED"}, false},
		{"refused by the guard", capture.Request{Status: 403, Refused: true}, false},
	}
	for _, c := range cases {
		if got := c.r.Sent(); got != c.want {
			t.Errorf("%s: Sent() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBlockedAmazonCaptchaAndWAF(t *testing.T) {
	captcha := capture.Page{MainStatus: 200, Text: "Sorry, we just need to make sure you're not a robot. For best results, please make sure your browser is accepting cookies. Type the characters you see in this image", TextLen: 200, Elements: 40}
	if Blocked(captcha) == "" {
		t.Error("amazon captcha page not caught")
	}
	waf := capture.Page{MainStatus: 200, Text: "Welcome back", TextLen: 12, Elements: 12}
	reqs := []capture.Request{
		{URL: "https://1c5c1ecf7303.742a0c6e.eu-west-3.token.awswaf.com/x/challenge.js", Host: "1c5c1ecf7303.742a0c6e.eu-west-3.token.awswaf.com", Type: "Script", Status: 200},
	}
	if BotCheckScripts(waf, reqs) == "" {
		t.Error("a small page driven by a bot-check script was not caught")
	}
	real := capture.Page{MainStatus: 200, TextLen: 40000, Elements: 3000}
	if BotCheckScripts(real, reqs) != "" {
		t.Error("a full page that also loads a bot-check script was called blocked")
	}
	other := []capture.Request{{URL: "https://cdn.test/a.js", Host: "cdn.test", Type: "Script", Status: 200}}
	if BotCheckScripts(waf, other) != "" {
		t.Error("an unrelated script was called a bot check")
	}
}

func TestBotCheckByVerifyEndpoint(t *testing.T) {
	small := capture.Page{MainStatus: 200, Text: "Continue shopping", TextLen: 17, Elements: 12}
	reqs := []capture.Request{{URL: "https://www.example.test/_sec/verify?provider=interstitial", Host: "www.example.test", Type: "Fetch", Status: 200}}
	if BotCheckScripts(small, reqs) == "" {
		t.Error("an interstitial that calls the verify endpoint was not caught")
	}
	if BotCheckScripts(capture.Page{TextLen: 9000}, reqs) != "" {
		t.Error("a full page was called blocked")
	}
}

func noVendors() *Vendors {
	v, _ := ParseVendors([]byte(`{"vendors": []}`))
	return v
}

func script(u, host string) capture.Request {
	return capture.Request{URL: u, Host: host, Type: "Script", Status: 200}
}

func TestVendorListIsComplete(t *testing.T) {
	vs := DefaultVendors()
	if len(vs.All()) < 10 {
		t.Fatalf("only %d vendors", len(vs.All()))
	}
	seen := map[string]bool{}
	for _, v := range vs.All() {
		if seen[v.Name] {
			t.Errorf("%s listed twice", v.Name)
		}
		seen[v.Name] = true
		if !strings.HasPrefix(v.Source, "https://") {
			t.Errorf("%s has no source link", v.Name)
		}
	}
	for _, never := range []string{"Qualtrics", "Qualaroo", "ForeSee", "VWO", "AB Tasty", "Permutive", "Bombora", "Fastly"} {
		if seen[never] {
			t.Errorf("%s must not be on the list", never)
		}
	}
}

func TestVendorMatches(t *testing.T) {
	vs := DefaultVendors()
	yes := map[string]string{
		"https://static.hotjar.com/c/hotjar-1234.js?sv=6":              "Hotjar",
		"https://script.hotjar.com/modules.abc.js":                     "Hotjar",
		"https://www.clarity.ms/tag/abcd1234":                          "Microsoft Clarity",
		"https://scripts.clarity.ms/0.8.55/clarity.js":                 "Microsoft Clarity",
		"https://edge.fullstory.com/s/fs.js":                           "FullStory",
		"https://cdn.mouseflow.com/projects/abc-def.js":                "Mouseflow",
		"https://web-sdk.smartlook.com/recorder.js":                    "Smartlook",
		"https://cdn.logr-in.com/LogRocket.min.js":                     "LogRocket",
		"https://tools.luckyorange.com/core/lo.js":                     "Lucky Orange",
		"https://cdn.inspectlet.com/inspectlet.js?wid=1":               "Inspectlet",
		"https://t.contentsquare.net/uxa/abcdef.js":                    "Contentsquare",
		"https://us-assets.i.posthog.com/static/recorder.js?v=1.370.0": "PostHog",
		"https://eu-assets.i.posthog.com/static/1.370.0/recorder.js":   "PostHog",
		"https://cdn.mxpnl.com/libs/mixpanel-recorder.min.js":          "Mixpanel",
		"https://browser.sentry-cdn.com/7.41.0/replay.min.js":          "Sentry Session Replay",
	}
	for u, want := range yes {
		got, ok := vs.Match(script(u, hostOf(u)))
		if !ok || got != want {
			t.Errorf("%s = %q, %v, want %q", u, got, ok, want)
		}
	}
	no := []string{
		// add-on main tags and plain analytics from the same vendors
		"https://us-assets.i.posthog.com/static/array.js",
		"https://us-assets.i.posthog.com/array/phc_x/config.js",
		"https://cdn.mxpnl.com/libs/mixpanel-2-latest.min.js",
		"https://browser.sentry-cdn.com/7.41.0/bundle.min.js",
		"https://cdn.heapanalytics.com/js/heap-1234.js",
		// survey, testing and personalization tools
		"https://cdn.qualaroo.com/kiss.js", "https://dev.visualwebsiteoptimizer.com/j.php", "https://cdn.optimizely.com/js/1.js",
		"https://cdn.permutive.com/x.js",
		// a shared CDN hostname and Bombora's tag
		"https://cnn-tls.map.fastly.net/x.js", "https://cdn.ml314.com/taglw.js",
		// the vendors' hosts but not their recording scripts
		"https://www.clarity.ms/collect", "https://edge.fullstory.com/s/other.js", "https://cdn.mouseflow.com/other.js",
		// a lookalike
		"https://static.hotjar.com.evil.test/c/hotjar-1.js",
	}
	for _, u := range no {
		if got, ok := vs.Match(script(u, hostOf(u))); ok {
			t.Errorf("%s counted as %s", u, got)
		}
	}
	// only scripts count
	r := capture.Request{URL: "https://static.hotjar.com/c/hotjar-1234.js", Host: "static.hotjar.com", Type: "Image", Status: 200}
	if _, ok := vs.Match(r); ok {
		t.Error("an image from a vendor host counted")
	}
}

func hostOf(u string) string {
	p, _ := url.Parse(u)
	return p.Hostname()
}

func TestParseVendorsRefusesBadEntries(t *testing.T) {
	bad := []string{
		`{"vendors":[{"name":"X","kind":"recorder","source":"http://x.test","match":[{"host":"x.test"}]}]}`,
		`{"vendors":[{"name":"X","kind":"addon","source":"https://x.test","match":[{"host":"x.test"}]}]}`,
		`{"vendors":[{"name":"X","kind":"other","source":"https://x.test","match":[{"host":"x.test"}]}]}`,
		`{"vendors":[{"name":"X","kind":"recorder","source":"https://x.test","match":[]}]}`,
		`{"vendors":[{"name":"X","kind":"recorder","source":"https://x.test","match":[{"host":"x.test","path":"("}]}]}`,
	}
	for _, b := range bad {
		if _, err := ParseVendors([]byte(b)); err == nil {
			t.Errorf("accepted %s", b)
		}
	}
}

func TestReplayUsesTheListNotTheTrackerLabel(t *testing.T) {
	vs, err := ParseVendors([]byte(`{"vendors":[{"name":"Rec","kind":"recorder","source":"https://rec.test/docs","match":[{"host":"rec.test"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	reqs := []capture.Request{
		script("https://rec.test/r.js", "rec.test"),
		script("https://replay.test/r.js", "replay.test"), // labelled Session Replay in the tracker data, not on our list
	}
	got := Replay(reqs, vs)
	if len(got) != 1 || got[0] != 0 {
		t.Errorf("Replay = %v, want [0]", got)
	}
}

func TestCDNLabelledDomainsAreNotTracking(t *testing.T) {
	radar := NewIndex(map[string]Domain{
		"shared.test":  {Owner: "Shared Co", Display: "Shared", Categories: []string{"Advertising", "Analytics", "CDN", "Session Replay"}},
		"fonts.test":   {Owner: "BigCo", Display: "BigCo", Categories: []string{"CDN", "Embedded Content"}},
		"ads.big.test": {Owner: "BigCo", Display: "BigCo", Categories: []string{"Advertising"}},
		"only.test":    {Owner: "Solo", Display: "Solo", Categories: []string{"Advertising"}},
	}, nil)
	reqs := []capture.Request{
		get("https://x.shared.test/a.js", "x.shared.test", "Script"),
		get("https://fonts.test/f.css", "fonts.test", "Stylesheet"),
	}
	p := Analyze("site.test", reqs, radar, noVendors(), nil)
	if p.Tracking != 0 || p.AdTech {
		t.Errorf("tracking %d adtech %v, want a CDN-labelled domain to count for nothing", p.Tracking, p.AdTech)
	}
	if len(p.Companies) != 2 {
		t.Errorf("companies = %+v, want both listed", p.Companies)
	}
	// the same company still counts through a domain that is not a CDN
	reqs = append(reqs, get("https://ads.big.test/p", "ads.big.test", "Image"), get("https://only.test/p", "only.test", "Image"))
	p = Analyze("site.test", reqs, radar, noVendors(), nil)
	if p.Tracking != 2 || !p.AdTech {
		t.Errorf("tracking %d adtech %v, want 2 (BigCo and Solo)", p.Tracking, p.AdTech)
	}
}

func TestVendorScriptCountsAsATrackingCompany(t *testing.T) {
	vs, _ := ParseVendors([]byte(`{"vendors":[{"name":"Rec","kind":"recorder","source":"https://rec.test/docs","match":[{"host":"rec.test"}]}]}`))
	reqs := []capture.Request{script("https://rec.test/r.js", "rec.test")}
	p := Analyze("site.test", reqs, testIndex(), vs, nil)
	if p.Tracking != 1 {
		t.Errorf("tracking = %d, want the recording vendor counted", p.Tracking)
	}
}

func TestBlocklistedPathOnCDNDomainCounts(t *testing.T) {
	radar := NewIndex(map[string]Domain{
		"cdn.test": {Owner: "Big Co", Display: "Big Co", Categories: []string{"CDN", "Advertising"}},
	}, nil)
	bl, err := ParseBlocklist([]byte(`{"trackers":{"cdn.test":{"default":"ignore","rules":[{"rule":"cdn\\.test/pixel"}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	lib := get("https://cdn.test/lib.js", "cdn.test", "Script")
	pixel := get("https://cdn.test/pixel?x=1", "cdn.test", "Image")

	p := Analyze("site.test", []capture.Request{lib}, radar, noVendors(), bl)
	if p.Tracking != 0 {
		t.Errorf("library file from a CDN domain counted: %d", p.Tracking)
	}
	p = Analyze("site.test", []capture.Request{lib, pixel}, radar, noVendors(), bl)
	if p.Tracking != 1 || !p.Per[1].Tracking || p.Per[0].Tracking {
		t.Errorf("tracking %d, per request %+v, want only the pixel to count", p.Tracking, p.Per)
	}
	p = Analyze("site.test", []capture.Request{pixel}, radar, noVendors(), nil)
	if p.Tracking != 0 {
		t.Error("counted without a blocklist")
	}
}

// A report must show why a request was called tracking: the URL and the rule.
func TestBlocklistedCompanyCarriesTheMatchedRule(t *testing.T) {
	radar := NewIndex(map[string]Domain{
		"cdn.test": {Owner: "Big Co", Display: "Big Co", Categories: []string{"CDN"}},
	}, nil)
	bl, err := ParseBlocklist([]byte(`{"trackers":{"cdn.test":{"default":"ignore","rules":[{"rule":"cdn\\.test/pixel"}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	reqs := []capture.Request{get("https://cdn.test/lib.js", "cdn.test", "Script"), get("https://cdn.test/pixel?x=1", "cdn.test", "Image")}
	p := Analyze("site.test", reqs, radar, noVendors(), bl)
	if len(p.Companies) != 1 {
		t.Fatalf("%d companies", len(p.Companies))
	}
	c := p.Companies[0]
	if len(c.BlocklistHits) != 1 || c.BlocklistHits[0].URL != "https://cdn.test/pixel?x=1" || c.BlocklistHits[0].Rule != `cdn\.test/pixel` {
		t.Errorf("hits %+v, want the pixel and its rule", c.BlocklistHits)
	}
	for i := 0; i < 15; i++ {
		reqs = append(reqs, get("https://cdn.test/pixel?n="+string(rune('a'+i)), "cdn.test", "Image"))
	}
	c = Analyze("site.test", reqs, radar, noVendors(), bl).Companies[0]
	if len(c.BlocklistHits) != maxEvidence || c.BlocklistMore != 16-maxEvidence {
		t.Errorf("%d hits and %d more, want %d and %d", len(c.BlocklistHits), c.BlocklistMore, maxEvidence, 16-maxEvidence)
	}
}
