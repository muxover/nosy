//go:build !nosytest

package guard

import "net/netip"

type testTarget struct {
	addr string
	ip   netip.Addr
}

func testDial(host, port string) (testTarget, bool) { return testTarget{}, false }
func testLookup(host string) ([]netip.Addr, bool)   { return nil, false }
func guardDisabled() bool                           { return false }
