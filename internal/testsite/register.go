//go:build nosytest

package testsite

import (
	"github.com/muxover/nosy/internal/browser"
	"github.com/muxover/nosy/internal/guard"
)

// Register routes every .test host to a planted-page server, makes the
// browser trust its CA and stubs the names the address tests resolve.
func Register(httpAddr, tlsAddr, spki string) {
	guard.Fixture("test", httpAddr, tlsAddr)
	guard.StubDNS("metadata.google.internal", "169.254.169.254")
	guard.StubDNS("rebind.example", "10.0.0.5")
	guard.StubDNS("metadata.example", "169.254.169.254")
	guard.StubDNS("v6.example", "fd00::1")
	browser.SetTestTrust(spki)
}
