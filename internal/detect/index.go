package detect

import "strings"

// Domain is what the tracker data says about one third-party domain.
type Domain struct {
	Owner          string
	Display        string
	Categories     []string
	Fingerprinting int
}

// Index maps hosts to the company that owns them and to the categories the
// data gives them, and knows which domains belong to one owner.
type Index struct {
	domains  map[string]Domain
	entityOf map[string]string
}

// NewIndex builds an Index from domains and from entities, which lists the
// domains each owner holds.
func NewIndex(domains map[string]Domain, entities map[string][]string) *Index {
	x := &Index{domains: domains, entityOf: map[string]string{}}
	for name, doms := range entities {
		for _, d := range doms {
			x.entityOf[d] = name
		}
	}
	return x
}

// Lookup finds host in the index, trying the host itself and then each parent
// domain. It returns the domain that matched.
func (x *Index) Lookup(host string) (Domain, string, bool) {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	for {
		if d, ok := x.domains[h]; ok {
			return d, h, true
		}
		i := strings.IndexByte(h, '.')
		if i < 0 || !strings.Contains(h[i+1:], ".") {
			return Domain{}, "", false
		}
		h = h[i+1:]
	}
}

func (x *Index) entity(domain string) string {
	for d := strings.ToLower(domain); d != ""; {
		if e, ok := x.entityOf[d]; ok {
			return e
		}
		i := strings.IndexByte(d, '.')
		if i < 0 {
			break
		}
		d = d[i+1:]
	}
	return ""
}

// SameOwner reports whether two domains belong to one known company.
func (x *Index) SameOwner(a, b string) bool {
	ea, eb := x.entity(a), x.entity(b)
	return ea != "" && ea == eb
}
