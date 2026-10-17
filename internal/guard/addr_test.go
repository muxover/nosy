package guard

import (
	"net/netip"
	"testing"
)

func TestAllowed(t *testing.T) {
	refused := []string{
		// loopback
		"127.0.0.1", "127.255.255.254", "::1", "0.0.0.0", "::",
		// RFC 1918 and CGNAT
		"10.0.0.1", "10.255.255.255", "172.16.0.1", "172.31.255.255", "192.168.1.1", "100.64.0.1", "100.127.255.255",
		// link local and cloud metadata
		"169.254.169.254", "169.254.0.1", "fe80::1", "fe80::a9fe:a9fe", "fd00:ec2::254", "fd00::1", "fc00::1",
		// documentation, benchmarking, multicast, reserved
		"192.0.2.1", "198.51.100.1", "203.0.113.1", "198.18.0.1", "224.0.0.1", "239.255.255.255", "240.0.0.1", "255.255.255.255",
		"2001:db8::1", "ff02::1", "fec0::1",
		// IPv4 wrapped in IPv6
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:169.254.169.254", "::ffff:192.168.0.1",
		"64:ff9b::7f00:1", "64:ff9b::a9fe:a9fe", "2002:7f00:1::1", "2002:a9fe:a9fe::1",
		// even when the wrapped IPv4 address is public
		"64:ff9b::808:808", "2002:808:808::1",
	}
	for _, s := range refused {
		if Allowed(netip.MustParseAddr(s)) {
			t.Errorf("%s allowed, want refused", s)
		}
	}
	allowed := []string{
		"1.1.1.1", "8.8.8.8", "93.184.216.34", "172.15.255.255", "172.32.0.1", "100.63.255.255", "100.128.0.1",
		"169.253.0.1", "11.0.0.1", "2606:4700:4700::1111", "2001:4860:4860::8888", "::ffff:8.8.8.8",
	}
	for _, s := range allowed {
		if !Allowed(netip.MustParseAddr(s)) {
			t.Errorf("%s refused, want allowed", s)
		}
	}
}

func TestAllowedZoneAndInvalid(t *testing.T) {
	if Allowed(netip.Addr{}) {
		t.Error("zero address allowed")
	}
	if Allowed(netip.MustParseAddr("fe80::1%eth0")) {
		t.Error("scoped link-local allowed")
	}
}
