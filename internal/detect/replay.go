package detect

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/muxover/nosy/internal/capture"
)

//go:embed replay_vendors.json
var replayVendorsJSON []byte

type vendorMatch struct {
	Host string `json:"host"`
	Path string `json:"path"`
	path *regexp.Regexp
}

type Vendor struct {
	Name   string        `json:"name"`
	Kind   string        `json:"kind"`
	Source string        `json:"source"`
	Match  []vendorMatch `json:"match"`
}

type Vendors struct {
	list []Vendor
}

// DefaultVendors is the list in the repo, embedded in the binary.
func DefaultVendors() *Vendors {
	v, err := ParseVendors(replayVendorsJSON)
	if err != nil {
		panic(err)
	}
	return v
}

func ParseVendors(data []byte) (*Vendors, error) {
	var f struct {
		Vendors []Vendor `json:"vendors"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	for i := range f.Vendors {
		v := &f.Vendors[i]
		if v.Name == "" || !strings.HasPrefix(v.Source, "https://") || len(v.Match) == 0 {
			return nil, fmt.Errorf("vendor %q needs a name, an https source and a match", v.Name)
		}
		if v.Kind != "recorder" && v.Kind != "addon" {
			return nil, fmt.Errorf("vendor %q: kind must be recorder or addon", v.Name)
		}
		for j := range v.Match {
			m := &v.Match[j]
			if m.Host == "" {
				return nil, fmt.Errorf("vendor %q: a match has no host", v.Name)
			}
			if v.Kind == "addon" && m.Path == "" {
				return nil, fmt.Errorf("vendor %q is an add-on and must be matched by path", v.Name)
			}
			if m.Path != "" {
				re, err := regexp.Compile(m.Path)
				if err != nil {
					return nil, fmt.Errorf("vendor %q: %w", v.Name, err)
				}
				m.path = re
			}
		}
	}
	return &Vendors{list: f.Vendors}, nil
}

func (vs *Vendors) All() []Vendor { return vs.list }

// Match names the vendor whose recording script the request loaded.
func (vs *Vendors) Match(r capture.Request) (string, bool) {
	if r.Type != "Script" && r.Type != "Other" {
		return "", false
	}
	u, err := url.Parse(r.URL)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	for _, v := range vs.list {
		for _, m := range v.Match {
			if host != m.Host && !strings.HasSuffix(host, "."+m.Host) {
				continue
			}
			if m.path == nil || m.path.MatchString(u.Path) {
				return v.Name, true
			}
		}
	}
	return "", false
}

// Replay returns the requests that loaded a recording script from the list.
func Replay(reqs []capture.Request, vs *Vendors) []int {
	var out []int
	for i, r := range reqs {
		if !r.Sent() {
			continue
		}
		if _, ok := vs.Match(r); ok {
			out = append(out, i)
		}
	}
	return out
}
