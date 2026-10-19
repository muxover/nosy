package guard

import (
	"errors"
	"net/netip"
	"strconv"
	"strings"
)

var ErrBlockedAddr = errors.New("address is private, local or reserved")

// Kind says what an address is, in the words a result uses.
type Kind string

const (
	Public        Kind = "public"
	Loopback      Kind = "loopback"
	Private       Kind = "private"
	LinkLocal     Kind = "link-local"
	Metadata      Kind = "cloud metadata"
	SharedNAT     Kind = "carrier-grade NAT"
	ThisNetwork   Kind = "this network"
	Multicast     Kind = "multicast"
	Broadcast     Kind = "broadcast"
	Reserved      Kind = "reserved"
	Benchmark     Kind = "benchmarking"
	Documentation Kind = "documentation"
	Tunnel        Kind = "tunnel"
	LocalName     Kind = "local name"
)

// Local is true for the kinds that are the user's own machine or network,
// which --allow-private opens. Cloud metadata is never among them.
func (k Kind) Local() bool {
	switch k {
	case Loopback, Private, LinkLocal, SharedNAT, ThisNetwork, LocalName:
		return true
	}
	return false
}

type addrRange struct {
	prefix netip.Prefix
	kind   Kind
}

func r(cidr string, k Kind) addrRange { return addrRange{netip.MustParsePrefix(cidr), k} }

// ranges is every address a scan may not reach, most specific first: the first
// match names the kind. IPv6 forms that carry an IPv4 address (mapped,
// compatible, NAT64, 6to4) are judged by the address they carry, before this
// table, in Classify.
var ranges = []addrRange{
	// cloud metadata services, refused even with --allow-private
	r("169.254.169.254/32", Metadata), // AWS, GCP, Azure, OCI and most others
	r("169.254.169.253/32", Metadata), // AWS DNS resolver
	r("169.254.169.123/32", Metadata), // AWS time service
	r("169.254.170.2/32", Metadata),   // AWS ECS task metadata
	r("169.254.170.23/32", Metadata),  // AWS EKS pod identity agent
	r("100.100.100.200/32", Metadata), // Alibaba Cloud
	r("168.63.129.16/32", Metadata),   // Azure WireServer
	r("192.0.0.192/32", Metadata),     // Oracle Cloud classic
	r("fd00:ec2::254/128", Metadata),  // AWS over IPv6
	r("fd00:ec2::253/128", Metadata),  // AWS DNS resolver over IPv6
	r("fd00:ec2::123/128", Metadata),  // AWS time service over IPv6
	r("fd00:ec2::23/128", Metadata),   // AWS EKS pod identity agent over IPv6
	r("fd20:ce::254/128", Metadata),   // GCP over IPv6

	// IPv4
	r("0.0.0.0/8", ThisNetwork),         // "this network"; 0.0.0.0 reaches the host on most systems
	r("10.0.0.0/8", Private),            // RFC 1918
	r("100.64.0.0/10", SharedNAT),       // carrier-grade NAT, RFC 6598
	r("127.0.0.0/8", Loopback),          // loopback
	r("169.254.0.0/16", LinkLocal),      // link-local
	r("172.16.0.0/12", Private),         // RFC 1918
	r("192.0.0.0/24", Reserved),         // IETF protocol assignments
	r("192.0.2.0/24", Documentation),    // TEST-NET-1
	r("192.88.99.0/24", Tunnel),         // 6to4 relay anycast, deprecated
	r("192.168.0.0/16", Private),        // RFC 1918
	r("198.18.0.0/15", Benchmark),       // benchmarking, RFC 2544
	r("198.51.100.0/24", Documentation), // TEST-NET-2
	r("203.0.113.0/24", Documentation),  // TEST-NET-3
	r("224.0.0.0/4", Multicast),         // multicast
	r("255.255.255.255/32", Broadcast),  // limited broadcast
	r("240.0.0.0/4", Reserved),          // reserved for future use

	// IPv6
	r("::/128", ThisNetwork),          // unspecified
	r("::1/128", Loopback),            // loopback
	r("::/96", Reserved),              // IPv4-compatible, deprecated (one carrying a blocked IPv4 is named by it)
	r("64:ff9b:1::/48", Private),      // NAT64 for local use, RFC 8215
	r("100::/64", Reserved),           // discard-only
	r("2001::/32", Tunnel),            // Teredo
	r("2001::/23", Reserved),          // IETF protocol assignments
	r("2001:db8::/32", Documentation), // documentation
	r("2002::/16", Tunnel),            // 6to4 (one carrying a blocked IPv4 is named by it)
	r("3fff::/20", Documentation),     // documentation, RFC 9637
	r("5f00::/16", Reserved),          // segment routing
	r("fc00::/7", Private),            // unique local
	r("fe80::/10", LinkLocal),         // link-local, with or without a zone
	r("fec0::/10", Private),           // site-local, deprecated
	r("ff00::/8", Multicast),          // multicast
}

var nat64 = netip.MustParsePrefix("64:ff9b::/96")

// Classify names what ip is. Public is the only kind a scan may reach.
func Classify(ip netip.Addr) Kind {
	if !ip.IsValid() {
		return Reserved
	}
	ip = ip.WithZone("")
	if v4, ok := embedded(ip); ok {
		if k := Classify(v4); k != Public {
			return k
		}
		if ip.Is4In6() || nat64.Contains(ip) {
			return Public
		}
	}
	for _, rg := range ranges {
		if rg.prefix.Contains(ip) {
			return rg.kind
		}
	}
	if !ip.IsGlobalUnicast() {
		return Reserved
	}
	return Public
}

// embedded is the IPv4 address an IPv6 address carries: IPv4-mapped,
// IPv4-compatible, NAT64 (64:ff9b::/96) and 6to4 (2002::/16).
func embedded(ip netip.Addr) (netip.Addr, bool) {
	if ip.Is4() {
		return netip.Addr{}, false
	}
	b := ip.As16()
	switch {
	case ip.Is4In6():
		return ip.Unmap(), true
	case nat64.Contains(ip):
		return netip.AddrFrom4([4]byte(b[12:16])), true
	case b[0] == 0x20 && b[1] == 0x02:
		return netip.AddrFrom4([4]byte(b[2:6])), true
	case ip != netip.IPv6Unspecified() && ip != netip.IPv6Loopback() && netip.MustParsePrefix("::/96").Contains(ip):
		return netip.AddrFrom4([4]byte(b[12:16])), true
	}
	return netip.Addr{}, false
}

// Allowed reports whether ip is a public unicast address.
func Allowed(ip netip.Addr) bool { return Classify(ip) == Public }

// ParseHost reads host as an IP address the way a browser does: IPv6 with or
// without brackets and zone, and IPv4 in any of the spellings browsers accept
// (2130706433, 0177.0.0.1, 0x7f.1, 127.1). It is false for a name.
func ParseHost(host string) (netip.Addr, bool) {
	h := strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if strings.Contains(h, ":") {
		// a zone in a URL is percent-encoded
		h = strings.Replace(h, "%25", "%", 1)
		ip, err := netip.ParseAddr(h)
		return ip, err == nil
	}
	return parseIPv4(strings.TrimSuffix(h, "."))
}

// parseIPv4 follows the WHATWG URL standard: one to four parts, each decimal,
// octal (leading 0) or hex (0x); the last part fills the bytes left.
func parseIPv4(s string) (netip.Addr, bool) {
	if s == "" {
		return netip.Addr{}, false
	}
	parts := strings.Split(s, ".")
	if len(parts) > 4 {
		return netip.Addr{}, false
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		n, ok := parseIPv4Part(p)
		if !ok {
			return netip.Addr{}, false
		}
		nums[i] = n
	}
	var v uint64
	for i, n := range nums[:len(nums)-1] {
		if n > 255 {
			return netip.Addr{}, false
		}
		v |= n << (8 * (3 - i))
	}
	last := nums[len(nums)-1]
	if last >= 1<<(8*(5-len(nums))) {
		return netip.Addr{}, false
	}
	v |= last
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}), true
}

func parseIPv4Part(p string) (uint64, bool) {
	base := 10
	switch {
	case p == "":
		return 0, false
	case len(p) > 1 && (p[:2] == "0x" || p[:2] == "0X"):
		p, base = p[2:], 16
		if p == "" {
			return 0, true
		}
	case len(p) > 1 && p[0] == '0':
		p, base = p[1:], 8
	}
	n, err := strconv.ParseUint(p, base, 64)
	if err != nil || n > 0xffffffff {
		return 0, false
	}
	return n, true
}

// localSuffixes are names that only mean something on the user's own network.
var localSuffixes = []string{".local", ".localhost", ".internal", ".home.arpa", ".lan"}

// IsLocalName is true for a name that only means something on the user's own
// network: localhost, the local, localhost, internal, home.arpa and lan names,
// and a name of a single label (a printer, a router).
func IsLocalName(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if _, ok := ParseHost(h); ok {
		return false
	}
	if h == "localhost" || !strings.Contains(h, ".") {
		return true
	}
	for _, s := range localSuffixes {
		if strings.HasSuffix(h, s) {
			return true
		}
	}
	return false
}
