package detect

import (
	"strings"
	"testing"

	"github.com/muxover/nosy/internal/capture"
)

func ck(names ...string) []capture.Cookie {
	var out []capture.Cookie
	for _, n := range names {
		out = append(out, capture.Cookie{Name: n})
	}
	return out
}

func TestThin(t *testing.T) {
	cases := []struct {
		name    string
		pg      capture.Page
		cookies []capture.Cookie
		third   bool
		want    string // a fragment of the reason, "" for a page that is fine
	}{
		// the measured bot walls
		{"amazon.ae wall", capture.Page{TextLen: 149, Elements: 39}, ck("aws-waf-token"), false, "nearly empty"},
		{"jumia wall, no third party", capture.Page{TextLen: 264, Elements: 47}, nil, false, "nearly empty"},
		{"jumia wall with a bot cookie", capture.Page{TextLen: 20, Elements: 12}, ck("_abck"), false, "nearly empty"},
		// nothing readable and no bot cookie: not finished loading
		{"u.ae loader screen", capture.Page{TextLen: 0, Elements: 19}, nil, false, "not finished loading"},
		{"blank page, ordinary cookie", capture.Page{TextLen: 0, Elements: 19}, ck("ASP.NET_SessionId"), false, "not finished loading"},
		// the limits themselves
		{"just under both limits", capture.Page{TextLen: thinText - 1, Elements: thinElements - 1}, nil, false, "nearly empty"},
		{"under the blank limit", capture.Page{TextLen: blankText - 1, Elements: 30}, nil, false, "not finished loading"},
		{"at the blank limit", capture.Page{TextLen: blankText, Elements: 30}, nil, false, "nearly empty"},
		{"text at the limit", capture.Page{TextLen: thinText, Elements: 10}, nil, false, ""},
		{"elements at the limit", capture.Page{TextLen: 10, Elements: thinElements}, nil, false, ""},
		// a small page the golden set scored
		{"example.com", capture.Page{TextLen: 917, Elements: 808}, nil, false, ""},
		{"gnu.org", capture.Page{TextLen: 6161, Elements: 392}, nil, false, ""},
		// reaching an outside company means the page loaded
		{"empty but third party", capture.Page{TextLen: 0, Elements: 5}, nil, true, ""},
		{"anti-bot cookies but third party", capture.Page{TextLen: 800, Elements: 300}, ck("bm_sv"), true, ""},
		// only anti-bot cookies, on a page too big to be nearly empty
		{"akamai cookies only", capture.Page{TextLen: 800, Elements: 300}, ck("ak_bmsc", "bm_sv", "AKA_A2"), false, "anti-bot cookies"},
		{"cloudflare cookie only", capture.Page{TextLen: 1200, Elements: 300}, ck("__cf_bm"), false, "anti-bot cookies"},
		{"incapsula cookies", capture.Page{TextLen: 1200, Elements: 300}, ck("visid_incap_123", "incap_ses_9_123"), false, "anti-bot cookies"},
		{"one ordinary cookie among them", capture.Page{TextLen: 800, Elements: 300}, ck("bm_sv", "language"), false, ""},
		{"no cookies at all", capture.Page{TextLen: 800, Elements: 300}, nil, false, ""},
		// a real page behind a bot manager stays scored
		{"big clean page behind cloudflare", capture.Page{TextLen: antibotText, Elements: 900}, ck("__cf_bm"), false, ""},
		{"big clean page behind akamai", capture.Page{TextLen: 9000, Elements: 2000}, ck("ak_bmsc", "bm_sv"), false, ""},
	}
	for _, c := range cases {
		got := Thin(c.pg, c.cookies, c.third)
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAntiBotCookie(t *testing.T) {
	for _, n := range []string{"_abck", "ak_bmsc", "bm_sz", "bm_sv", "bm_mi", "bm_so", "bm_lso", "AKA_A2", "__cf_bm", "cf_clearance",
		"aws-waf-token", "reese84", "datadome", "_pxvid", "pxcts", "_px3", "incap_ses_1_2_3", "visid_incap_2", "nlbi_2", "TS0109abf7", "TS02bcbb96027"} {
		if !AntiBotCookie(n) {
			t.Errorf("%s should be an anti-bot cookie", n)
		}
	}
	for _, n := range []string{"sessionid", "ASP.NET_SessionId", "_ga", "language", "cookies_policy", "uae#lang", "theme", "tsession", "TSDKS"} {
		if AntiBotCookie(n) {
			t.Errorf("%s is not an anti-bot cookie", n)
		}
	}
}

func TestUnfinished(t *testing.T) {
	if !Unfinished(reasonBlank) || !Unfinished(reasonEmpty) {
		t.Error("blank and empty pages did not finish loading")
	}
	if Unfinished("the site showed a bot check") || Unfinished("the site answered with an error page (HTTP 403)") {
		t.Error("a bot check or an error page is not an unfinished page")
	}
}
