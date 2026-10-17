package detect

import (
	"sort"
	"strings"

	"github.com/muxover/nosy/internal/capture"
)

// Party says how one request relates to the site being scanned.
type Party struct {
	Counted    bool   `json:"-"`
	ThirdParty bool   `json:"third_party"`
	Company    string `json:"company,omitempty"`
	Tracking   bool   `json:"tracking,omitempty"`
}

type Company struct {
	Name                string   `json:"name"`
	Owner               string   `json:"owner,omitempty"`
	Tracking            bool     `json:"tracking"`
	Classes             []string `json:"classes,omitempty"`
	Categories          []string `json:"categories,omitempty"`
	Domains             []string `json:"domains"`
	Requests            int      `json:"requests"`
	Evidence            []int    `json:"evidence"`
	KnownFingerprinting int      `json:"known_fingerprinting,omitempty"`

	// the requests the blocklist blocked, with the rule that matched
	BlocklistHits []BlockHit `json:"blocklist_hits,omitempty"`
	BlocklistMore int        `json:"blocklist_more,omitempty"`
}

// BlockHit is one request the blocklist blocked and the rule it matched.
type BlockHit struct {
	URL  string `json:"url"`
	Type string `json:"type,omitempty"`
	Rule string `json:"rule"`
}

type Parties struct {
	Per       []Party
	Companies []Company
	Tracking  int  // distinct companies in tracking classes
	AdTech    bool // at least one advertising company
}

// ThirdPartyContacted reports whether any third party answered a request.
func (p Parties) ThirdPartyContacted() bool {
	for _, q := range p.Per {
		if q.Counted && q.ThirdParty {
			return true
		}
	}
	return false
}

const maxEvidence = 10

// Analyze splits the requests into first and third party and groups the
// third parties by the company that owns them.
func Analyze(siteName string, reqs []capture.Request, radar *Index, vendors *Vendors, blocklist *Blocklist) Parties {
	out := Parties{Per: make([]Party, len(reqs))}
	byKey := map[string]*Company{}
	domSeen := map[string]map[string]bool{}
	for i, r := range reqs {
		if !r.Sent() || !networkURL(r.URL) {
			continue
		}
		out.Per[i].Counted = true
		reg := Registrable(r.Host)
		if reg == siteName || radar.SameOwner(reg, siteName) {
			continue
		}
		out.Per[i].ThirdParty = true

		dom, matched, known := radar.Lookup(r.Host)
		key, display, owner := reg, reg, ""
		if known && dom.Owner != "" {
			key, display, owner = dom.Owner, dom.Display, dom.Owner
			if display == "" {
				display = dom.Owner
			}
		}
		if !known {
			matched = reg
		}
		c := byKey[key]
		if c == nil {
			c = &Company{Name: display, Owner: owner}
			byKey[key] = c
			domSeen[key] = map[string]bool{}
		}
		c.Requests++
		if len(c.Evidence) < maxEvidence {
			c.Evidence = append(c.Evidence, i)
		}
		if !domSeen[key][matched] {
			domSeen[key][matched] = true
			c.Domains = append(c.Domains, matched)
		}
		if dom.Fingerprinting > c.KnownFingerprinting {
			c.KnownFingerprinting = dom.Fingerprinting
		}
		// a domain the tracker data labels CDN is shared infrastructure: it never
		// makes its owner a tracking company on its own, only a request that
		// the blocklist has a blocking rule for does
		cdn := false
		for _, cat := range dom.Categories {
			if cat == "CDN" {
				cdn = true
			}
		}
		if rule, hit := blocklist.Match(r.Host, r.URL, siteName, r.Type); cdn && hit {
			if len(c.BlocklistHits) < maxEvidence {
				c.BlocklistHits = append(c.BlocklistHits, BlockHit{URL: r.URL, Type: r.Type, Rule: rule})
			} else {
				c.BlocklistMore++
			}
			c.Tracking = true
			c.Classes = appendUnique(c.Classes, string(ClassBlocklisted))
			out.Per[i].Tracking = true
		}
		if _, ok := vendors.Match(r); ok {
			c.Tracking = true
			c.Classes = appendUnique(c.Classes, string(ClassReplay))
			out.Per[i].Tracking = true
		}
		for _, cat := range dom.Categories {
			c.Categories = appendUnique(c.Categories, cat)
			if class, ok := ClassOf(cat); ok && !cdn {
				c.Tracking = true
				c.Classes = appendUnique(c.Classes, string(class))
				out.Per[i].Tracking = true
				if class == ClassAdvertising {
					out.AdTech = true
				}
			}
		}
		out.Per[i].Company = display
	}
	for _, c := range byKey {
		sort.Strings(c.Domains)
		sort.Strings(c.Classes)
		sort.Strings(c.Categories)
		if c.Tracking {
			out.Tracking++
		}
		out.Companies = append(out.Companies, *c)
	}
	sort.Slice(out.Companies, func(i, j int) bool {
		a, b := out.Companies[i], out.Companies[j]
		if a.Tracking != b.Tracking {
			return a.Tracking
		}
		if a.Requests != b.Requests {
			return a.Requests > b.Requests
		}
		return a.Name < b.Name
	})
	return out
}

func networkURL(u string) bool {
	for _, p := range []string{"http://", "https://", "ws://", "wss://"} {
		if strings.HasPrefix(u, p) {
			return true
		}
	}
	return false
}

func appendUnique(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}
