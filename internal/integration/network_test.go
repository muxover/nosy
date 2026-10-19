//go:build nosytest

package integration

import (
	"strings"
	"testing"

	"github.com/muxover/nosy/internal/scan"
	"github.com/muxover/nosy/internal/testsite"
)

// canaries reads every listener the scanner must never reach.
func canaries(t *testing.T) map[string]int64 {
	t.Helper()
	if fixture.LocalCanary == nil || fixture.UDPCanary == nil || fixture.LocalUDPCanary == nil {
		t.Fatal("the canaries on 127.0.0.2 and the UDP canaries could not be opened, so the network tests prove nothing here")
	}
	n := map[string]int64{
		"127.0.0.1 tcp": fixture.Canary.Conns(),
		"127.0.0.2 tcp": fixture.LocalCanary.Conns(),
		"127.0.0.1 udp": fixture.UDPCanary.Packets(),
		"127.0.0.2 udp": fixture.LocalUDPCanary.Packets(),
	}
	for name, c := range map[string]*testsite.Canary{"127.0.0.1:80 tcp": fixture.CanaryPort, "127.0.0.2:80 tcp": fixture.HopCanary} {
		if c != nil {
			n[name] = c.Conns()
		}
	}
	return n
}

func TestNoWayOutReachesTheLocalNetwork(t *testing.T) {
	for _, v := range testsite.Vectors {
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			before := canaries(t)
			scheme := "http"
			if v == "sw" {
				// a service worker needs a secure page
				scheme = "https"
			}
			r := scanTest(t, scheme+"://v"+v+".test/")
			for name, n := range canaries(t) {
				if n != before[name] {
					t.Errorf("the canary on %s was reached %d times", name, n-before[name])
				}
			}
			if r.Status != scan.StatusDone {
				t.Fatalf("status %s: %s", r.Status, r.Reason)
			}
			if v == "rtc" {
				// WebRTC sends UDP itself, never through the guard: only the canaries can tell
				return
			}
			kinds := map[string]bool{}
			for _, b := range r.Blocked {
				kinds[b.Kind] = true
				if b.Frame == "" && v != "sw" && v != "worker" {
					t.Errorf("%s has no frame", b.Address)
				}
			}
			if !kinds["loopback"] {
				t.Errorf("blocked = %+v, want the loopback attempts named", r.Blocked)
			}
			if f := finding(r, "local-network"); f == nil || f.Label != scan.Observed {
				t.Errorf("finding = %+v, want an observed local-network finding", f)
			}
		})
	}
}

// with --allow-private the canaries are reached, so the test above can fail;
// cloud metadata stays refused
// not parallel: it reaches the canaries on purpose
func TestAllowPrivateOpensTheLocalNetworkButNotMetadata(t *testing.T) {
	before := canaries(t)
	r := scanTest(t, "http://vfetch.test/", func(o *scan.Options) { o.AllowPrivate = true })
	after := canaries(t)
	for _, name := range []string{"127.0.0.1 tcp", "127.0.0.2 tcp"} {
		if after[name] == before[name] {
			t.Errorf("with --allow-private the canary on %s was never reached: the network tests cannot fail", name)
		}
	}
	if !r.Conditions.AllowPrivate {
		t.Error("the conditions do not say --allow-private was used")
	}
	metadata := false
	for _, b := range r.Blocked {
		metadata = metadata || b.Kind == "cloud metadata" && strings.HasPrefix(b.Address, "169.254.169.254")
	}
	if !metadata {
		t.Errorf("blocked = %+v, want cloud metadata refused even with --allow-private", r.Blocked)
	}
}
