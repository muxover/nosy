package guard

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

func listener(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			hits.Add(1)
			c.Close()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String(), &hits
}

func stub(ips ...string) func(context.Context, string) ([]netip.Addr, error) {
	return func(context.Context, string) ([]netip.Addr, error) {
		var out []netip.Addr
		for _, s := range ips {
			out = append(out, netip.MustParseAddr(s))
		}
		return out, nil
	}
}

func TestDialRefusesNonPublic(t *testing.T) {
	local, hits := listener(t)
	_, port, _ := net.SplitHostPort(local)
	cases := []struct {
		name, addr string
		lookup     func(context.Context, string) ([]netip.Addr, error)
		want       error
	}{
		{"loopback literal", "127.0.0.1:80", nil, ErrBlockedAddr},
		{"metadata literal", "169.254.169.254:80", nil, ErrBlockedAddr},
		{"private literal", "10.1.2.3:443", nil, ErrBlockedAddr},
		{"ipv6 loopback", "[::1]:80", nil, ErrBlockedAddr},
		{"ipv6 unique local", "[fd00::1]:443", nil, ErrBlockedAddr},
		{"ipv6 mapped private", "[::ffff:10.0.0.1]:80", nil, ErrBlockedAddr},
		{"name to loopback", "a.example:80", stub("127.0.0.1"), ErrBlockedAddr},
		{"name to metadata", "a.example:80", stub("169.254.169.254"), ErrBlockedAddr},
		{"name to ipv6 loopback", "a.example:80", stub("::1"), ErrBlockedAddr},
		{"mixed public and private", "a.example:443", stub("93.184.216.34", "10.0.0.5"), ErrBlockedAddr},
		{"private first", "a.example:443", stub("192.168.0.9", "93.184.216.34"), ErrBlockedAddr},
		{"port 8080", "93.184.216.34:8080", nil, ErrPort},
		{"port 22", "93.184.216.34:22", nil, ErrPort},
		{"local server port", "127.0.0.1:" + port, nil, ErrPort},
	}
	for _, c := range cases {
		var refused int
		d := &Dialer{Lookup: c.lookup, Timeout: time.Second, Refused: func(string, string, error) { refused++ }}
		conn, err := d.DialContext(context.Background(), "tcp", c.addr)
		if err == nil {
			conn.Close()
			t.Errorf("%s: connected", c.name)
			continue
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
		if refused != 1 {
			t.Errorf("%s: Refused called %d times", c.name, refused)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("local listener got %d connections", n)
	}
}

func TestDialUnresolvable(t *testing.T) {
	d := &Dialer{Lookup: stub()}
	if _, err := d.DialContext(context.Background(), "tcp", "nothing.example:80"); !errors.Is(err, ErrNoIP) {
		t.Errorf("got %v, want ErrNoIP", err)
	}
}
