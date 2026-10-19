package guard

import (
	"bufio"
	"encoding/base64"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"testing"
	"time"
)

func startProxy(t *testing.T, maxBytes int64, onCap func()) *Proxy {
	t.Helper()
	p, err := StartProxy(&Dialer{Timeout: time.Second}, maxBytes, onCap)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

// client goes through p with the scan's secret.
func client(p *Proxy) *http.Client {
	proxyURL := &url.URL{Scheme: "http", Host: p.Addr(), User: url.UserPassword(p.User(), p.Secret())}
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
}

func connect(t *testing.T, p *Proxy, target, auth string) *http.Response {
	t.Helper()
	conn, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	req := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n"
	if auth != "" {
		req += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(auth)) + "\r\n"
	}
	if _, err := conn.Write([]byte(req + "\r\n")); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("%s: %v", target, err)
	}
	resp.Body.Close()
	return resp
}

func TestProxyNeedsTheScansSecret(t *testing.T) {
	local, hits := listener(t)
	_, port, _ := net.SplitHostPort(local)
	p, err := StartProxy(&Dialer{Timeout: time.Second, AllowPrivate: true}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	for _, auth := range []string{"", "nosy:wrong", "other:" + p.Secret(), p.Secret()} {
		if resp := connect(t, p, "127.0.0.1:"+port, auth); resp.StatusCode != http.StatusProxyAuthRequired {
			t.Errorf("credentials %q: answered %d, want 407", auth, resp.StatusCode)
		}
	}
	resp, err := (&http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: p.Addr()})}, Timeout: 5 * time.Second}).Get("http://" + local + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusProxyAuthRequired {
		t.Errorf("a plain request without the secret answered %d", resp.StatusCode)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("%d requests without the secret went through", n)
	}
	if n := p.Stats().Unauthorized; n != 5 {
		t.Errorf("%d unauthorised requests counted, want 5", n)
	}
	// with the secret, the same request goes through
	if resp := connect(t, p, "127.0.0.1:"+port, p.User()+":"+p.Secret()); resp.StatusCode != http.StatusOK {
		t.Errorf("with the secret: answered %d", resp.StatusCode)
	}
}

func TestProxyListensOnLoopbackOnly(t *testing.T) {
	p := startProxy(t, 0, nil)
	ap := netip.MustParseAddrPort(p.Addr())
	if ap.Addr() != netip.MustParseAddr("127.0.0.1") || ap.Port() == 0 {
		t.Errorf("listening on %s", ap)
	}
	other := startProxy(t, 0, nil)
	if other.Addr() == p.Addr() || other.Secret() == p.Secret() {
		t.Error("two scans share a port or a secret")
	}
	// a page cannot be sent through the guard to the guard itself
	if resp := connect(t, p, p.Addr(), p.User()+":"+p.Secret()); resp.StatusCode != http.StatusForbidden {
		t.Errorf("CONNECT to the guard's own port answered %d", resp.StatusCode)
	}
}

func TestProxyRefusesConnectToPrivateAddresses(t *testing.T) {
	local, hits := listener(t)
	_, port, _ := net.SplitHostPort(local)
	p := startProxy(t, 0, nil)
	for _, target := range []string{"127.0.0.1:443", "169.254.169.254:80", "10.0.0.5:443", "[::1]:443", "93.184.216.34:8080", "127.0.0.1:" + port} {
		resp := connect(t, p, target, p.User()+":"+p.Secret())
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("CONNECT %s answered %d, want 403", target, resp.StatusCode)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("local listener got %d connections", n)
	}
	if got := len(p.Stats().Refused); got != 6 {
		t.Errorf("%d refusals recorded, want 6", got)
	}
}

func TestProxyMarksRefusedPlainRequests(t *testing.T) {
	p := startProxy(t, 0, nil)
	c := client(p)
	for _, target := range []string{"http://127.0.0.1/", "http://169.254.169.254/latest/meta-data/", "http://[fd00::1]/"} {
		resp, err := c.Get(target)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden || resp.Header.Get(RefusedHeader) == "" {
			t.Errorf("%s: status %d, refused header %q", target, resp.StatusCode, resp.Header.Get(RefusedHeader))
		}
	}
}
