package detect

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type blockCond struct {
	Domains []string `json:"domains,omitempty"`
	Types   []string `json:"types,omitempty"`
}

type blockRule struct {
	Rule       string     `json:"rule"`
	Action     string     `json:"action,omitempty"`
	Options    *blockCond `json:"options,omitempty"`
	Exceptions *blockCond `json:"exceptions,omitempty"`
	re         *regexp.Regexp
}

type blockTracker struct {
	Default string      `json:"default,omitempty"`
	Rules   []blockRule `json:"rules,omitempty"`
}

// Blocklist answers the one question the domain index cannot: which requests
// to a CDN-labelled domain are tracking.
type Blocklist struct {
	trackers map[string]blockTracker
}

// ParseBlocklist reads a blocklist in the tracker-blocklist JSON format.
func ParseBlocklist(b []byte) (*Blocklist, error) {
	var f struct {
		Trackers map[string]blockTracker `json:"trackers"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("read blocklist: %w", err)
	}
	bl := &Blocklist{trackers: f.Trackers}
	for d, t := range bl.trackers {
		for i := range t.Rules {
			re, err := regexp.Compile("(?i)" + t.Rules[i].Rule)
			if err != nil {
				// a JavaScript-only construct; the rule cannot match here
				continue
			}
			t.Rules[i].re = re
		}
		bl.trackers[d] = t
	}
	return bl, nil
}

// Match follows the list's documented algorithm for one third-party request:
// the entry for the host or its nearest parent domain, the first matching
// rule, then the entry's default. It returns the rule that blocked the
// request as written in the list, or the entry's default when no rule matched.
func (b *Blocklist) Match(host, url, siteDomain, resourceType string) (rule string, blocked bool) {
	if b == nil {
		return "", false
	}
	domain, t, ok := b.entry(host)
	if !ok {
		return "", false
	}
	typ := webRequestType(resourceType)
	for _, r := range t.Rules {
		if r.re == nil || !r.re.MatchString(url) {
			continue
		}
		if r.Action == "ignore" {
			return "", false
		}
		if r.Options != nil && !r.Options.matches(siteDomain, typ) {
			continue
		}
		if r.Exceptions != nil && r.Exceptions.matches(siteDomain, typ) {
			return "", false
		}
		return r.Rule, true
	}
	if t.Default == "block" {
		return "default: block for " + domain, true
	}
	return "", false
}

func (b *Blocklist) entry(host string) (string, blockTracker, bool) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for host != "" {
		if t, ok := b.trackers[host]; ok {
			return host, t, true
		}
		_, rest, found := strings.Cut(host, ".")
		if !found {
			break
		}
		host = rest
	}
	return "", blockTracker{}, false
}

// matches is true when every list the condition has matches: the site
// domain by suffix, the request type exactly.
func (c *blockCond) matches(siteDomain, typ string) bool {
	if len(c.Domains) > 0 {
		hit := false
		for _, d := range c.Domains {
			if siteDomain == d || strings.HasSuffix(siteDomain, "."+d) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if len(c.Types) > 0 {
		hit := false
		for _, t := range c.Types {
			if t == typ {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

// webRequestType maps a DevTools resource type to the WebExtensions name the
// blocklist uses.
func webRequestType(t string) string {
	switch t {
	case "Script":
		return "script"
	case "XHR", "Fetch":
		return "xmlhttprequest"
	case "Image":
		return "image"
	case "Stylesheet":
		return "stylesheet"
	case "Document":
		return "sub_frame"
	case "Font":
		return "font"
	case "Media":
		return "media"
	case "Ping":
		return "ping"
	case "WebSocket":
		return "websocket"
	}
	return "other"
}
