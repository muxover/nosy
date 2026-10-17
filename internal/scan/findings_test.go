package scan

import (
	"strings"
	"testing"

	"github.com/muxover/nosy/internal/detect"
)

func find(fs []Finding, id string) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.ID == id {
			out = append(out, f)
		}
	}
	return out
}

func TestFindingsCarryHowTheyAreKnown(t *testing.T) {
	r := &Result{
		Status:  StatusDone,
		Cookies: []Cookie{{Name: "uid", Domain: "a.test", Identifier: true}, {Name: "theme", Domain: "a.test"}},
		Fingerprinting: []detect.Technique{
			{Name: "canvas", Scripts: []string{"https://fp.test/c.js"}, Calls: 2},
		},
		SessionRecording: []Evidence{{Request: 3, URL: "https://rec.test/r.js"}},
		Consent:          detect.Consent{Present: true, Via: []string{"__tcfapi"}},
		RequestedURL:     "http://a.test/",
		FinalURL:         "https://www.a.test/",
	}
	p := detect.Parties{
		AdTech: true,
		Companies: []detect.Company{
			{Name: "AdNet", Tracking: true}, {Name: "Stats", Tracking: true}, {Name: "CDN Co"},
		},
	}
	fs := findings(r, p)
	want := map[string]string{
		"third-party-companies": Observed,
		"tracking-companies":    Classified,
		"ad-tech":               Classified,
		"identifier-cookies":    Classified,
		"fingerprinting-canvas": Classified,
		"session-recording":     Classified,
		"consent-api":           Observed,
		"redirect":              Observed,
	}
	for id, label := range want {
		got := find(fs, id)
		if len(got) != 1 || got[0].Label != label {
			t.Errorf("%s = %+v, want one finding labelled %s", id, got, label)
		}
	}
	if f := find(fs, "tracking-companies")[0]; !strings.Contains(f.Text, "2 of them are classified as tracking") || len(f.Evidence) != 2 {
		t.Errorf("tracking finding = %+v", f)
	}
	if f := find(fs, "identifier-cookies")[0]; !strings.Contains(f.Text, "1 cookie set before any click looks like") || f.Evidence[0] != "uid (a.test)" {
		t.Errorf("identifier finding = %+v", f)
	}
	for _, f := range fs {
		if f.Label != Observed && f.Label != Classified && f.Label != Inferred {
			t.Errorf("%s has the label %q", f.ID, f.Label)
		}
		if strings.Contains(f.Text, "...") || strings.Contains(f.Text, "…") {
			t.Errorf("%s has an ellipsis: %q", f.ID, f.Text)
		}
	}
}

func TestOneTypedTextFindingPerKindOfLeak(t *testing.T) {
	r := &Result{Status: StatusDone, TypedText: TypedText{Hits: []KeyHit{
		{KeyHit: detect.KeyHit{ThirdParty: true, Form: "plain"}, URL: "https://c.test/1"},
		{KeyHit: detect.KeyHit{ThirdParty: true, Form: "plain"}, URL: "https://c.test/2"},
		{KeyHit: detect.KeyHit{ThirdParty: true, Form: "base64"}, URL: "https://c.test/3"},
		{KeyHit: detect.KeyHit{ThirdParty: false, Form: "plain"}, URL: "https://a.test/suggest"},
	}}}
	got := find(findings(r, detect.Parties{}), "typed-text")
	if len(got) != 3 {
		t.Fatalf("%d typed-text findings, want 3: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Text, "a third party in 2 requests (plain encoding)") || len(got[0].Evidence) != 2 {
		t.Errorf("first = %+v", got[0])
	}
	if !strings.Contains(got[2].Text, "its own server in 1 request (plain encoding)") {
		t.Errorf("third = %+v", got[2])
	}
	for _, f := range got {
		if f.Label != Observed {
			t.Errorf("%+v is not observed", f)
		}
	}
}

func TestQuietPageHasOnlyTheContactFinding(t *testing.T) {
	fs := findings(&Result{Status: StatusDone}, detect.Parties{})
	if len(fs) != 1 || fs[0].ID != "third-party-companies" || !strings.Contains(fs[0].Text, "no company other than its own") {
		t.Errorf("findings = %+v", fs)
	}
}

func TestBlockedAndFailedScansHaveOneFinding(t *testing.T) {
	b := findings(&Result{Status: StatusBlocked, Reason: "the site showed a bot check"}, detect.Parties{})
	if len(b) != 1 || b[0].Label != Inferred || !strings.Contains(b[0].Text, "bot check") {
		t.Errorf("blocked = %+v", b)
	}
	f := findings(&Result{Status: StatusFailed, Reason: "the site could not be reached"}, detect.Parties{})
	if len(f) != 1 || f[0].Label != Observed || !strings.Contains(f[0].Text, "could not be reached") {
		t.Errorf("failed = %+v", f)
	}
}
