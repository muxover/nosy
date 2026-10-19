//go:build nosytest

package guard

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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
	resp, err := client(p).Get("http://cap.test/")
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

// HTTP_PROXY and HTTPS_PROXY point at a canary: the guard goes out directly.
func TestProxyIgnoresTheSystemProxy(t *testing.T) {
	canary, hits := listener(t)
	t.Setenv("HTTP_PROXY", "http://"+canary)
	t.Setenv("HTTPS_PROXY", "http://"+canary)
	t.Setenv("ALL_PROXY", "http://"+canary)
	t.Setenv("NO_PROXY", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("direct")) }))
	defer srv.Close()
	Fixture("direct.test", strings.TrimPrefix(srv.URL, "http://"), "")
	p := startProxy(t, 0, nil)
	resp, err := client(p).Get("http://direct.test/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "direct" || hits.Load() != 0 {
		t.Errorf("body %q, %d connections to the system proxy", body, hits.Load())
	}
}

func TestProxyCutsAnAnswerAtTheCapForOneRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chunk := []byte(strings.Repeat("x", 1<<20))
		for i := int64(0); i < MaxRequestBytes>>20+2; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	Fixture("big1.test", strings.TrimPrefix(srv.URL, "http://"), "")
	p := startProxy(t, 0, nil)
	resp, err := client(p).Get("http://big1.test/")
	if err != nil {
		t.Fatal(err)
	}
	n, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if n > MaxRequestBytes+1 || p.Stats().Limits.RequestCap != 1 {
		t.Errorf("read %d bytes, limits %+v; want the answer cut at %d and counted", n, p.Stats().Limits, MaxRequestBytes)
	}
}

func TestProxyCapsOpenConnections(t *testing.T) {
	hold, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	go func() {
		for {
			c, err := hold.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	Fixture("hold.test", "", hold.Addr().String())
	p := startProxy(t, 0, nil)
	auth := p.User() + ":" + p.Secret()
	for i := 0; i < MaxConns; i++ {
		if resp := connect(t, p, "hold.test:443", auth); resp.StatusCode != http.StatusOK {
			t.Fatalf("connection %d answered %d", i+1, resp.StatusCode)
		}
	}
	if resp := connect(t, p, "hold.test:443", auth); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("connection %d answered %d, want 503", MaxConns+1, resp.StatusCode)
	}
	if n := p.Stats().Limits.Connections; n != 1 {
		t.Errorf("%d refusals counted, want 1", n)
	}
}

func TestProxyGivesUpOnASilentServer(t *testing.T) {
	defer func(i, h time.Duration) { IdleTimeout, HeaderTimeout = i, h }(IdleTimeout, HeaderTimeout)
	IdleTimeout, HeaderTimeout = 300*time.Millisecond, 300*time.Millisecond

	// a server that reads the request and never answers
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	go func() {
		for {
			c, err := silent.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(io.Discard, c) }()
		}
	}()
	Fixture("silent.test", silent.Addr().String(), silent.Addr().String())
	p := startProxy(t, 0, nil)

	start := time.Now()
	if resp, err := client(p).Get("http://silent.test/"); err == nil {
		resp.Body.Close()
	}
	conn, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	auth := base64.StdEncoding.EncodeToString([]byte(p.User() + ":" + p.Secret()))
	fmt.Fprintf(conn, "CONNECT silent.test:443 HTTP/1.1\r\nHost: silent.test:443\r\nProxy-Authorization: Basic %s\r\n\r\n", auth)
	r := bufio.NewReader(conn)
	if resp, err := http.ReadResponse(r, nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("tunnel: %v %v", resp, err)
	}
	conn.Write([]byte("a request the server will not answer"))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := r.ReadByte(); err == nil {
		t.Error("the silent server answered")
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Errorf("giving up took %s", d)
	}
	l := p.Stats().Limits
	if l.SlowAnswers != 1 || l.Idle != 1 {
		t.Errorf("limits %+v, want one slow answer and one idle tunnel", l)
	}
}
