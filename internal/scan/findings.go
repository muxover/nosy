package scan

import (
	"fmt"
	"strings"

	"github.com/muxover/nosy/internal/detect"
)

// maxEvidence is how many examples a finding carries.
const maxEvidence = 5

// findings lists what the scan found in sentences, each with how it is known:
// observed (seen happening), classified (seen, then matched to a rule or list)
// or inferred (concluded from a pattern). The sentences say what was measured
// and no more.
func findings(r *Result, p detect.Parties) []Finding {
	out := []Finding{}
	add := func(id, label, text string, evidence ...string) {
		if len(evidence) > maxEvidence {
			evidence = evidence[:maxEvidence]
		}
		out = append(out, Finding{ID: id, Label: label, Text: text, Evidence: evidence})
	}
	switch r.Status {
	case StatusFailed:
		add("failed", Observed, "The page could not be loaded: "+r.Reason+".")
		return out
	case StatusBlocked:
		add("blocked", Inferred, "Nothing was scored: "+r.Reason+".")
		return out
	}

	var third []string
	for _, c := range p.Companies {
		third = append(third, c.Name)
	}
	if len(third) > 0 {
		add("third-party-companies", Observed,
			fmt.Sprintf("The page contacted %d %s other than its own before any click.", len(third), plural(len(third), "company", "companies")),
			third...)
	} else {
		add("third-party-companies", Observed, "The page contacted no company other than its own before any click.")
	}
	var tracking []string
	for _, c := range p.Companies {
		if c.Tracking {
			tracking = append(tracking, c.Name)
		}
	}
	if len(tracking) > 0 {
		add("tracking-companies", Classified,
			fmt.Sprintf("%d of them %s classified as tracking.", len(tracking), plural(len(tracking), "is", "are")), tracking...)
	}
	if p.AdTech {
		add("ad-tech", Classified, "At least one of them is an advertising company.")
	}
	var ids []string
	for _, c := range r.Cookies {
		if c.Identifier {
			ids = append(ids, c.Name+" ("+c.Domain+")")
		}
	}
	if len(ids) > 0 {
		add("identifier-cookies", Classified,
			fmt.Sprintf("%d %s set before any click look%s like an identifier for this browser.", len(ids), plural(len(ids), "cookie", "cookies"), plural(len(ids), "s", "")), ids...)
	}
	for _, t := range r.Fingerprinting {
		add("fingerprinting-"+t.Name, Classified,
			fmt.Sprintf("A script read %s data in the way browser fingerprinting does (%d %s).", t.Name, t.Calls, plural(t.Calls, "call", "calls")), t.Scripts...)
	}
	if len(r.SessionRecording) > 0 {
		var urls []string
		for _, e := range r.SessionRecording {
			urls = append(urls, e.URL)
		}
		add("session-recording", Classified, "The page loaded a script from a session-recording vendor.", urls...)
	}
	for _, h := range r.TypedText.Hits {
		where := "its own server"
		if h.ThirdParty {
			where = "a third party"
		}
		add("typed-text", Observed,
			fmt.Sprintf("Text typed into a field, without submitting, was sent to %s (%s encoding).", where, h.Form), h.URL)
	}
	if r.Consent.Present {
		add("consent-api", Observed, "The page exposes a consent API: "+strings.Join(r.Consent.Via, ", ")+".")
	}
	if r.FinalURL != "" && !strings.HasPrefix(r.FinalURL, r.RequestedURL) {
		add("redirect", Observed, "The address redirected to "+r.FinalURL+".")
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
