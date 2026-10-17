package guard

import (
	"bufio"
	"net"
	"net/http"
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

func TestProxyRefusesConnectToPrivateAddresses(t *testing.T) {
	local, hits := listener(t)
	_, port, _ := net.SplitHostPort(local)
	p := startProxy(t, 0, nil)
	for _, target := range []string{"127.0.0.1:443", "169.254.169.254:80", "10.0.0.5:443", "[::1]:443", "93.184.216.34:8080", "127.0.0.1:" + port} {
		conn, err := net.Dial("tcp", p.Addr())
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := conn.Write([]byte("CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n\r\n")); err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		conn.Close()
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		resp.Body.Close()
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
	proxyURL, _ := url.Parse("http://" + p.Addr())
	c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
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
