package detect

import (
	"net/netip"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// Registrable returns the registrable domain of host (example.co.uk for
// www.example.co.uk). A host that is itself a public suffix, or an IP, is
// returned unchanged.
func Registrable(host string) string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if _, err := netip.ParseAddr(host); err == nil {
		return host
	}
	d, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return host
	}
	return d
}
