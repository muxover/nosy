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
		{"only private answers", "a.example:443", stub("192.168.0.9", "10.0.0.5"), ErrBlockedAddr},
		{"odd spelling of loopback", "2130706433:80", nil, ErrBlockedAddr},
		{"short loopback", "127.1:80", nil, ErrBlockedAddr},
		{"hex loopback", "0x7f.1:443", nil, ErrBlockedAddr},
		{"NAT64 of metadata", "[64:ff9b::a9fe:a9fe]:80", nil, ErrBlockedAddr},
		{"6to4 of loopback", "[2002:7f00:1::1]:443", nil, ErrBlockedAddr},
		{"scoped link-local", "[fe80::1%25eth0]:80", nil, ErrBlockedAddr},
		{"local name", "printer.local:80", stub("93.184.216.34"), ErrLocalName},
		{"single label", "router:80", stub("93.184.216.34"), ErrLocalName},
		{"internal name", "metadata.google.internal:80", stub("93.184.216.34"), ErrLocalName},
		{"port 8080", "93.184.216.34:8080", nil, ErrPort},
		{"port 22", "93.184.216.34:22", nil, ErrPort},
		{"local server port", "127.0.0.1:" + port, nil, ErrBlockedAddr},
		{"public address, odd port", "93.184.216.34:" + port, nil, ErrPort},
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

func TestDialAllowPrivate(t *testing.T) {
	local, hits := listener(t)
	d := &Dialer{Timeout: time.Second, AllowPrivate: true}
	conn, err := d.DialContext(context.Background(), "tcp", local)
	if err != nil {
		t.Fatalf("allowed dialer refused the local listener: %v", err)
	}
	conn.Close()
	time.Sleep(50 * time.Millisecond)
	if hits.Load() != 1 {
		t.Errorf("local listener got %d connections, want 1", hits.Load())
	}
	if err := d.Check(context.Background(), "127.0.0.1"); err != nil {
		t.Errorf("Check refused a loopback address with AllowPrivate: %v", err)
	}
	if err := (&Dialer{}).Check(context.Background(), "127.0.0.1"); !errors.Is(err, ErrBlockedAddr) {
		t.Errorf("Check without AllowPrivate = %v, want ErrBlockedAddr", err)
	}
}

// fakeDial records the address the dialer connects to, after the socket check.
func fakeDial(t *testing.T, d *Dialer) *[]string {
	t.Helper()
	var dialed []string
	d.dialTCP = func(ctx context.Context, nd *net.Dialer, addr string) (net.Conn, error) {
		if nd.Control != nil {
			if err := nd.Control("tcp", addr, nil); err != nil {
				return nil, err
			}
		}
		dialed = append(dialed, addr)
		a, b := net.Pipe()
		b.Close()
		return a, nil
	}
	return &dialed
}

func TestMixedAnswersOnlyDialAPublicOne(t *testing.T) {
	for _, answers := range [][]string{
		{"93.184.216.34", "10.0.0.5"},
		{"192.168.0.9", "93.184.216.34"},
		{"127.0.0.1", "::1", "2606:2800:220:1::1", "169.254.169.254"},
	} {
		d := &Dialer{Lookup: stub(answers...)}
		dialed := fakeDial(t, d)
		c, err := d.DialContext(context.Background(), "tcp", "mixed.example:443")
		if err != nil {
			t.Errorf("%v: %v", answers, err)
			continue
		}
		c.Close()
		for _, a := range *dialed {
			ap := netip.MustParseAddrPort(a)
			if !Allowed(ap.Addr()) {
				t.Errorf("%v: dialed %s", answers, a)
			}
		}
	}
}

// a resolver that answers a public address first and a private one after, as a
// rebinding attack does: the address dialed is always the one checked
func TestRebindingCannotReachAPrivateAddress(t *testing.T) {
	var n int
	rebind := func(context.Context, string) ([]netip.Addr, error) {
		n++
		if n == 1 {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	d := &Dialer{Lookup: rebind}
	dialed := fakeDial(t, d)
	if err := d.Check(context.Background(), "rebind.example"); err != nil {
		t.Fatalf("first answer refused: %v", err)
	}
	if c, err := d.DialContext(context.Background(), "tcp", "rebind.example:80"); err == nil {
		c.Close()
		t.Fatalf("the second answer, 127.0.0.1, was dialed: %v", *dialed)
	}
	// one lookup per dial: the check and the connect use the same answer
	n = 0
	if c, err := d.DialContext(context.Background(), "tcp", "rebind.example:80"); err != nil {
		t.Fatalf("first answer of a fresh lookup refused: %v", err)
	} else {
		c.Close()
	}
	if n != 1 || len(*dialed) != 1 || (*dialed)[0] != "93.184.216.34:80" {
		t.Errorf("%d lookups, dialed %v; want one lookup and the address it gave", n, *dialed)
	}
}

func TestAllowPrivateNeverOpensMetadataOrNosysOwnPorts(t *testing.T) {
	d := &Dialer{AllowPrivate: true}
	dialed := fakeDial(t, d)
	own := netip.MustParseAddrPort("127.0.0.1:9222")
	d.Never(own)
	for _, addr := range []string{"169.254.169.254:80", "[fd00:ec2::254]:80", "100.100.100.200:80", "168.63.129.16:80", "127.0.0.1:9222", "[::ffff:127.0.0.1]:9222"} {
		if c, err := d.DialContext(context.Background(), "tcp", addr); err == nil {
			c.Close()
			t.Errorf("%s dialed with --allow-private", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:8080", "10.0.0.5:80", "printer.local:80"} {
		d.Lookup = stub("192.168.1.20")
		c, err := d.DialContext(context.Background(), "tcp", addr)
		if err != nil {
			t.Errorf("%s refused with --allow-private: %v", addr, err)
			continue
		}
		c.Close()
	}
	if len(*dialed) != 3 {
		t.Errorf("dialed %v", *dialed)
	}
}

func TestRefusalsSayWhatKindOfAddress(t *testing.T) {
	cases := map[string]Kind{
		"127.0.0.1:80":       Loopback,
		"10.0.0.5:443":       Private,
		"169.254.169.254:80": Metadata,
		"100.64.0.1:80":      SharedNAT,
		"printer.local:80":   LocalName,
	}
	for addr, want := range cases {
		var got Kind
		d := &Dialer{Lookup: stub("93.184.216.34"), Refused: func(_, _ string, err error) { got = KindOf(err) }}
		fakeDial(t, d)
		if c, err := d.DialContext(context.Background(), "tcp", addr); err == nil {
			c.Close()
		}
		if got != want {
			t.Errorf("%s refused as %q, want %q", addr, got, want)
		}
	}
}
