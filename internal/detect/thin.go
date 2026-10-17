package detect

import (
	"regexp"
	"strings"

	"github.com/muxover/nosy/internal/capture"
)

// A page that gave the scan nothing to measure is reported as blocked, not
// scored: a score built on a bot-check page says nothing about the site.
//
// The limits come from the golden set (20 sites, 3 runs each, plus 16 more
// runs of noon.com and cnn.com and 32 runs after the rule), measured as visible
// text characters and DOM elements of the page the visit ended on:
//
//	bot walls                    149 / 39 (amazon.ae), 264 / 47 (jumia.com.eg)
//	loader screen, no content    0 / 19 (u.ae), thin, reported as not finished loading
//	smallest scored, no 3rd party  917 chars (example.com), 392 elements (gnu.org)
//
// 500 characters and 100 elements sit between the groups with a factor of 3
// or more on each side.
const (
	thinText     = 500
	thinElements = 100

	// only-anti-bot cookies is the weaker signal, so it needs the page to be
	// small as well; a real page with 2000 characters is never a bot wall
	antibotText = 2000

	// A thin page with no readable text at all, and no bot-management
	// cookie, is a page that had not finished loading (a loader screen,
	// u.ae: 0 characters), not a wall: every wall measured showed a message
	// of at least 149 characters. Walls with less text still carry the
	// cookies of the product that raised them and stay blocked.
	blankText = 50
)

const (
	reasonBlank = "the page showed nothing readable and contacted no outside company, so it had probably not finished loading"
	reasonEmpty = "the page was empty"
)

// Unfinished says whether a reason from Blocked or Thin describes a page
// that never finished loading, as opposed to a bot check or an error page.
func Unfinished(why string) bool { return why == reasonBlank || why == reasonEmpty }

// Thin says why a page that was not an error or a known bot check still
// gave the scan nothing to measure, or "". It needs no third party to have
// been contacted: a page that reached outside companies was loaded.
func Thin(pg capture.Page, cookies []capture.Cookie, thirdParty bool) string {
	if thirdParty {
		return ""
	}
	if pg.TextLen < thinText && pg.Elements < thinElements {
		if pg.TextLen < blankText && !anyAntiBot(cookies) {
			return reasonBlank
		}
		return "the page was nearly empty and contacted no outside company, so it was probably a bot check"
	}
	if pg.TextLen < antibotText && onlyAntiBot(cookies) {
		return "the page set only anti-bot cookies and contacted no outside company, so it was probably a bot check"
	}
	return ""
}

func anyAntiBot(cookies []capture.Cookie) bool {
	for _, c := range cookies {
		if AntiBotCookie(c.Name) {
			return true
		}
	}
	return false
}

func onlyAntiBot(cookies []capture.Cookie) bool {
	if len(cookies) == 0 {
		return false
	}
	for _, c := range cookies {
		if !AntiBotCookie(c.Name) {
			return false
		}
	}
	return true
}

// cookies set by bot-management products, from their own documentation:
// Akamai Bot Manager, Cloudflare Bot Management, AWS WAF, Imperva, DataDome,
// HUMAN (PerimeterX), Kasada-style sensors and F5 Distributed Cloud
var antiBotNames = map[string]bool{
	"_abck": true, "ak_bmsc": true, "bm_sz": true, "bm_sv": true, "bm_mi": true,
	"bm_so": true, "bm_ss": true, "bm_lso": true, "aka_a2": true,
	"__cf_bm": true, "cf_clearance": true, "__cfruid": true, "_cfuvid": true,
	"aws-waf-token": true,
	"reese84":       true, "___utmvc": true,
	"datadome": true,
	"_pxvid":   true, "_pxhd": true, "pxcts": true, "_px": true, "_px2": true, "_px3": true,
}

var antiBotPatterns = regexp.MustCompile(`^(incap_ses_|visid_incap_|nlbi_|_pxff_|ts[0-9a-f]{8})`)

// AntiBotCookie reports whether a cookie name belongs to a bot-management product.
func AntiBotCookie(name string) bool {
	n := strings.ToLower(name)
	return antiBotNames[n] || antiBotPatterns.MatchString(n)
}
