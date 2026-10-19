package guard

import (
	"fmt"
	"net/netip"
	"testing"
)

// spellings writes an IPv4 address every way a browser reads it.
func spellings(ip netip.Addr) []string {
	b := ip.As4()
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	return []string{
		ip.String(),
		fmt.Sprint(v),          // 2130706433
		fmt.Sprintf("0x%x", v), // 0x7f000001
		fmt.Sprintf("0%o.0%o.0%o.0%o", b[0], b[1], b[2], b[3]), // 0177.00.00.01
		fmt.Sprintf("0x%x.0x%x.0x%x.0x%x", b[0], b[1], b[2], b[3]),
		fmt.Sprintf("%d.%d", b[0], v&0xffffff),        // 127.1
		fmt.Sprintf("%d.%d.%d", b[0], b[1], v&0xffff), // 127.0.1
		fmt.Sprintf("0x%x.%d", b[0], v&0xffffff),      // 0x7f.1
		ip.String() + ".",                             // a trailing dot
	}
}

// v6forms wraps an IPv4 address in every IPv6 form that carries one.
func v6forms(ip netip.Addr) []string {
	b := ip.As4()
	return []string{
		"::ffff:" + ip.String(), // IPv4-mapped
		fmt.Sprintf("[::ffff:%02x%02x:%02x%02x]", b[0], b[1], b[2], b[3]), // mapped, hex, in brackets
		"::" + ip.String(), // IPv4-compatible
		fmt.Sprintf("64:ff9b::%02x%02x:%02x%02x", b[0], b[1], b[2], b[3]), // NAT64
		fmt.Sprintf("2002:%02x%02x:%02x%02x::1", b[0], b[1], b[2], b[3]),  // 6to4
	}
}

// every range in the table, with an address inside it and the kind it is
var inRange = []struct {
	addr string
	kind Kind
}{
	{"169.254.169.254", Metadata}, {"169.254.169.253", Metadata}, {"169.254.169.123", Metadata},
	{"169.254.170.2", Metadata}, {"169.254.170.23", Metadata}, {"100.100.100.200", Metadata},
	{"168.63.129.16", Metadata}, {"192.0.0.192", Metadata},
	{"0.0.0.0", ThisNetwork}, {"0.1.2.3", ThisNetwork},
	{"10.0.0.1", Private}, {"10.255.255.255", Private},
	{"100.64.0.1", SharedNAT}, {"100.127.255.254", SharedNAT},
	{"127.0.0.1", Loopback}, {"127.255.255.254", Loopback},
	{"169.254.0.1", LinkLocal}, {"169.254.255.254", LinkLocal},
	{"172.16.0.1", Private}, {"172.31.255.254", Private},
	{"192.0.0.8", Reserved},
	{"192.0.2.1", Documentation}, {"198.51.100.7", Documentation}, {"203.0.113.9", Documentation},
	{"192.88.99.1", Tunnel},
	{"192.168.1.1", Private}, {"192.168.255.254", Private},
	{"198.18.0.1", Benchmark}, {"198.19.255.254", Benchmark},
	{"224.0.0.1", Multicast}, {"239.255.255.250", Multicast},
	{"255.255.255.255", Broadcast},
	{"240.0.0.1", Reserved}, {"254.255.255.254", Reserved},
}

var inRange6 = []struct {
	addr string
	kind Kind
}{
	{"fd00:ec2::254", Metadata}, {"fd00:ec2::253", Metadata}, {"fd00:ec2::123", Metadata},
	{"fd00:ec2::23", Metadata}, {"fd20:ce::254", Metadata},
	{"::", ThisNetwork}, {"::1", Loopback},
	{"::1.2.3.4", Reserved}, // IPv4-compatible, carrying a public address
	{"64:ff9b:1::1", Private},
	{"100::1", Reserved},
	{"2001::1", Tunnel}, {"2001:0:4136:e378:8000:63bf:3fff:fdd2", Tunnel},
	{"2001:2::1", Reserved},
	{"2001:db8::1", Documentation}, {"3fff::1", Documentation},
	{"2002:808:808::1", Tunnel}, // 6to4 carrying a public address
	{"5f00::1", Reserved},
	{"fc00::1", Private}, {"fd12:3456::1", Private},
	{"fe80::1", LinkLocal}, {"fe80::1%eth0", LinkLocal}, {"fe80::a9fe:a9fe", LinkLocal},
	{"fec0::1", Private},
	{"ff02::1", Multicast}, {"ff05::2", Multicast},
}

func TestEveryRangeInEverySpelling(t *testing.T) {
	for _, c := range inRange {
		ip := netip.MustParseAddr(c.addr)
		for _, s := range spellings(ip) {
			got, ok := ParseHost(s)
			if !ok || got != ip {
				t.Errorf("%s read as %v %v, want %s", s, got, ok, ip)
				continue
			}
			if k := Classify(got); k != c.kind {
				t.Errorf("%s is %s, want %s", s, k, c.kind)
			}
		}
		for _, s := range v6forms(ip) {
			got, ok := ParseHost(s)
			if !ok {
				t.Errorf("%s not read as an address", s)
				continue
			}
			if Allowed(got) {
				t.Errorf("%s (carrying %s) allowed", s, ip)
			}
		}
	}
	for _, c := range inRange6 {
		got, ok := ParseHost(c.addr)
		if !ok {
			t.Errorf("%s not read as an address", c.addr)
			continue
		}
		if k := Classify(got); k != c.kind {
			t.Errorf("%s is %s, want %s", c.addr, k, c.kind)
		}
		if !ok || Allowed(got) {
			t.Errorf("%s allowed", c.addr)
		}
	}
	// every entry in the table is covered above
	covered := map[netip.Prefix]bool{}
	for _, list := range [][]string{addrs(inRange), addrs(inRange6)} {
		for _, a := range list {
			ip, _ := ParseHost(a)
			for _, rg := range ranges {
				if rg.prefix.Contains(ip.WithZone("")) {
					covered[rg.prefix] = true
					break
				}
			}
		}
	}
	for _, rg := range ranges {
		if !covered[rg.prefix] {
			t.Errorf("%s (%s) has no test address", rg.prefix, rg.kind)
		}
	}
}

func addrs(l []struct {
	addr string
	kind Kind
}) []string {
	var out []string
	for _, c := range l {
		out = append(out, c.addr)
	}
	return out
}

func TestPublicAddressesAreAllowed(t *testing.T) {
	for _, s := range []string{
		"1.1.1.1", "8.8.8.8", "93.184.216.34", "172.15.255.255", "172.32.0.1", "100.63.255.255", "100.128.0.1",
		"169.253.0.1", "11.0.0.1", "223.255.255.254", "2606:4700:4700::1111", "2001:4860:4860::8888",
		"::ffff:8.8.8.8", "64:ff9b::808:808", // a public IPv4 behind NAT64 is reachable on an IPv6-only network
		"16843009", "0x1010101", "1.1.257",
	} {
		ip, ok := ParseHost(s)
		if !ok || !Allowed(ip) {
			t.Errorf("%s refused, want allowed", s)
		}
	}
}

func TestParseHostLeavesNamesAlone(t *testing.T) {
	for _, s := range []string{"example.com", "1.example", "0x7f.example", "08.0.0.1", "256.256.256.256", "1.2.3.4.5", "4294967296", "", "a:b"} {
		if ip, ok := ParseHost(s); ok {
			t.Errorf("%q read as the address %s", s, ip)
		}
	}
}

func TestZoneAndInvalid(t *testing.T) {
	if Allowed(netip.Addr{}) {
		t.Error("zero address allowed")
	}
	for _, s := range []string{"[fe80::1%25eth0]", "fe80::1%eth0", "[fe80::1%251]"} {
		ip, ok := ParseHost(s)
		if !ok || Allowed(ip) {
			t.Errorf("%s: %v %v", s, ip, ok)
		}
	}
}

func TestLocalNames(t *testing.T) {
	for _, s := range []string{"localhost", "LOCALHOST.", "printer", "router.local", "app.localhost", "db.internal", "nas.home.arpa", "box.lan", "metadata.google.internal"} {
		if !IsLocalName(s) {
			t.Errorf("%s is not a local name", s)
		}
	}
	for _, s := range []string{"example.com", "local.example", "internal.example.org", "127.0.0.1", "lan.example"} {
		if IsLocalName(s) {
			t.Errorf("%s is a local name", s)
		}
	}
}

// metadata stays refused when private addresses are allowed
func TestMetadataIsNeverLocal(t *testing.T) {
	for _, c := range append(inRange, inRange6...) {
		if c.kind == Metadata && c.kind.Local() {
			t.Errorf("%s would be opened by --allow-private", c.addr)
		}
	}
}

func FuzzAddressCheck(f *testing.F) {
	for _, c := range inRange {
		b := netip.MustParseAddr(c.addr).As4()
		f.Add(b[0], b[1], b[2], b[3])
	}
	f.Add(byte(8), byte(8), byte(8), byte(8))
	f.Fuzz(func(t *testing.T, a, b, c, d byte) {
		ip := netip.AddrFrom4([4]byte{a, b, c, d})
		want := Classify(ip)
		// every spelling of the address is the same address and the same answer
		for _, s := range spellings(ip) {
			got, ok := ParseHost(s)
			if !ok || got != ip || Classify(got) != want {
				t.Fatalf("%s read as %v %v (%s), want %s (%s)", s, got, ok, Classify(got), ip, want)
			}
		}
		// an IPv6 form never opens what the IPv4 address does not
		for _, s := range v6forms(ip) {
			got, ok := ParseHost(s)
			if !ok {
				t.Fatalf("%s not read", s)
			}
			if want != Public && Allowed(got) {
				t.Fatalf("%s allowed, but %s is %s", s, ip, want)
			}
		}
		// the answer agrees with the table
		for _, rg := range ranges {
			if rg.prefix.Contains(ip) && want == Public {
				t.Fatalf("%s is in %s but allowed", ip, rg.prefix)
			}
		}
	})
}
