//go:build nosytest

package guard

import (
	"net/netip"
	"strings"
	"sync"
)

// Test-only hooks. They exist only in binaries built with -tags nosytest.

type testTarget struct {
	addr string
	ip   netip.Addr
}

var hooks struct {
	mu       sync.Mutex
	fixtures map[string][2]string // domain -> http and tls addresses
	dns      map[string][]netip.Addr
	noGuard  bool
}

// Fixture routes domain and its subdomains to a local test server: port 80
// to httpAddr and port 443 to tlsAddr. Only those hosts skip the address
// guard.
func Fixture(domain, httpAddr, tlsAddr string) {
	hooks.mu.Lock()
	defer hooks.mu.Unlock()
	if hooks.fixtures == nil {
		hooks.fixtures = map[string][2]string{}
	}
	hooks.fixtures[domain] = [2]string{httpAddr, tlsAddr}
}

// StubDNS makes host resolve to ips without touching the network.
func StubDNS(host string, ips ...string) {
	hooks.mu.Lock()
	defer hooks.mu.Unlock()
	if hooks.dns == nil {
		hooks.dns = map[string][]netip.Addr{}
	}
	for _, s := range ips {
		hooks.dns[host] = append(hooks.dns[host], netip.MustParseAddr(s))
	}
}

// DisableGuard turns the address check off so a test can show that the
// network firewall stops the traffic on its own.
func DisableGuard(off bool) {
	hooks.mu.Lock()
	hooks.noGuard = off
	hooks.mu.Unlock()
}

func testDial(host, port string) (testTarget, bool) {
	hooks.mu.Lock()
	defer hooks.mu.Unlock()
	for dom, addrs := range hooks.fixtures {
		if host != dom && !strings.HasSuffix(host, "."+dom) {
			continue
		}
		switch port {
		case "80":
			return testTarget{addr: addrs[0], ip: netip.MustParseAddr("127.0.0.1")}, true
		case "443":
			return testTarget{addr: addrs[1], ip: netip.MustParseAddr("127.0.0.1")}, true
		}
	}
	return testTarget{}, false
}

func testLookup(host string) ([]netip.Addr, bool) {
	hooks.mu.Lock()
	defer hooks.mu.Unlock()
	ips, ok := hooks.dns[host]
	return ips, ok
}

func guardDisabled() bool {
	hooks.mu.Lock()
	defer hooks.mu.Unlock()
	return hooks.noGuard
}
