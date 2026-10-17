//go:build nosytest

package guard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProxyCountsBytesAndStopsAtTheCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(strings.Repeat("x", 256<<10)))
	}))
	defer srv.Close()
	Fixture("cap.test", strings.TrimPrefix(srv.URL, "http://"), "")

	var capped atomic.Bool
	p := startProxy(t, 100<<10, func() { capped.Store(true) })
	proxyURL, _ := url.Parse("http://" + p.Addr())
	c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
	resp, err := c.Get("http://cap.test/")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	st := p.Stats()
	if !capped.Load() || !st.CapHit {
		t.Errorf("cap hit = %v, callback = %v, want both after %d bytes", st.CapHit, capped.Load(), st.BytesDown)
	}
	if ips := st.HostIPs["cap.test"]; len(ips) != 1 || ips[0] != "127.0.0.1" {
		t.Errorf("host addresses = %v, want the address cap.test was dialed at", st.HostIPs)
	}
}
