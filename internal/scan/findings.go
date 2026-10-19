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
	if origins, third, ids := r.StorageCounts(); origins > 0 {
		add("storage", Observed,
			fmt.Sprintf("%d %s kept data in the browser before any click: %s.", origins, plural(origins, "origin", "origins"), storageWhat(r)), storageOrigins(r)...)
		if third > 0 {
			var tp []string
			for _, o := range r.Storage {
				if o.ThirdParty {
					tp = append(tp, o.Origin)
				}
			}
			add("third-party-storage", Observed,
				fmt.Sprintf("%d of them %s, with storage of its own.", third, plural(third, "is a third-party frame", "are third-party frames")), tp...)
		}
		if ids > 0 {
			var which []string
			for _, o := range r.Storage {
				for _, k := range o.LocalStorage {
					if k.Identifier {
						which = append(which, o.Origin+" localStorage "+k.Key)
					}
				}
				for _, db := range o.IndexedDB {
					for _, st := range db.Stores {
						for _, v := range st.IdentifierValues {
							which = append(which, o.Origin+" IndexedDB "+db.Name+"/"+st.Name+" "+v)
						}
					}
				}
			}
			add("storage-identifiers", Classified,
				fmt.Sprintf("%d stored %s look%s like an identifier for this browser.", ids, plural(ids, "value", "values"), plural(ids, "s", "")), which...)
		}
	}
	if sws := r.serviceWorkerCount(); sws > 0 {
		add("service-workers", Observed, fmt.Sprintf("The page registered %d service %s.", sws, plural(sws, "worker", "workers")))
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
	// one finding per destination kind and encoding, however many fields leaked
	type leak struct {
		third bool
		form  string
	}
	var order []leak
	byLeak := map[leak][]string{}
	for _, h := range r.TypedText.Hits {
		k := leak{h.ThirdParty, h.Form}
		if _, seen := byLeak[k]; !seen {
			order = append(order, k)
		}
		byLeak[k] = append(byLeak[k], h.URL)
	}
	for _, k := range order {
		where := "its own server"
		if k.third {
			where = "a third party"
		}
		urls := byLeak[k]
		add("typed-text", Observed,
			fmt.Sprintf("Text typed into a field, without submitting, was sent to %s in %d %s (%s encoding).", where, len(urls), plural(len(urls), "request", "requests"), k.form), urls...)
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

func storageOrigins(r *Result) []string {
	var out []string
	for _, o := range r.Storage {
		out = append(out, o.Origin)
	}
	return out
}

// storageWhat names the kinds of storage in use and how much of each.
func storageWhat(r *Result) string {
	var local, session, idb, caches, sws int
	for _, o := range r.Storage {
		local += len(o.LocalStorage)
		session += len(o.SessionStorage)
		idb += len(o.IndexedDB)
		caches += len(o.CacheStorage)
		sws += len(o.ServiceWorkers)
	}
	var parts []string
	add := func(n int, one, many string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, plural(n, one, many)))
		}
	}
	add(local, "local storage key", "local storage keys")
	add(session, "session storage key", "session storage keys")
	add(idb, "IndexedDB database", "IndexedDB databases")
	add(caches, "cache", "caches")
	add(sws, "service worker", "service workers")
	return strings.Join(parts, ", ")
}

func (r *Result) serviceWorkerCount() int {
	n := 0
	for _, o := range r.Storage {
		n += len(o.ServiceWorkers)
	}
	return n
}
