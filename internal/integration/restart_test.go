//go:build nosytest

package integration

import (
	"testing"

	"github.com/muxover/nosy/internal/capture"
)

func TestNetworkRestarted(t *testing.T) {
	cases := []struct {
		name string
		c    capture.Capture
		want bool
	}{
		{"clean visit", capture.Capture{Requests: []capture.Request{{Status: 200}, {Failed: "net::ERR_NAME_NOT_RESOLVED"}}}, false},
		{"network changed on a subresource", capture.Capture{Requests: []capture.Request{{Status: 200}, {Failed: "net::ERR_NETWORK_CHANGED"}}}, true},
		{"cert verifier changed", capture.Capture{Requests: []capture.Request{{Failed: "net::ERR_CERT_VERIFIER_CHANGED"}}}, true},
		{"socket not connected", capture.Capture{Requests: []capture.Request{{Failed: "net::ERR_SOCKET_NOT_CONNECTED"}}}, true},
		{"main page failed that way", capture.Capture{Page: capture.Page{MainError: "net::ERR_NETWORK_CHANGED"}}, true},
		{"a site that refuses connections", capture.Capture{Requests: []capture.Request{{Failed: "net::ERR_CONNECTION_REFUSED"}}}, false},
	}
	for _, c := range cases {
		if got := networkRestarted(&c.c); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
