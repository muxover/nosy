package guard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"syscall"
	"time"
)

// AllowedPorts are the only destination ports a scan may reach.
var AllowedPorts = map[string]bool{"80": true, "443": true}

var (
	ErrPort = errors.New("port is not 80 or 443")
	ErrNoIP = errors.New("host did not resolve")
)

// Dialer resolves host names itself and connects only to public addresses.
// The address is checked twice: on the resolved list, and again on the
// socket right before connect, so a changed DNS answer cannot slip through.
type Dialer struct {
	Lookup  func(ctx context.Context, host string) ([]netip.Addr, error)
	Timeout time.Duration

	// Resolved is told which address a host name was dialed at.
	Resolved func(host string, ip netip.Addr)
	// Refused is told about every connection the guard turned away.
	Refused func(host, port string, err error)
}

func (d *Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	conn, err := d.dial(ctx, host, port)
	if err != nil && (errors.Is(err, ErrBlockedAddr) || errors.Is(err, ErrPort)) && d.Refused != nil {
		d.Refused(host, port, err)
	}
	return conn, err
}

func (d *Dialer) dial(ctx context.Context, host, port string) (net.Conn, error) {
	if to, ok := testDial(host, port); ok {
		return d.connect(ctx, to.addr, to.ip, host, false)
	}
	if !AllowedPorts[port] {
		return nil, fmt.Errorf("%w: %s", ErrPort, port)
	}
	ips, err := d.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	guarded := !guardDisabled()
	if guarded {
		for _, ip := range ips {
			if !Allowed(ip) {
				return nil, fmt.Errorf("%w: %s resolves to %s", ErrBlockedAddr, host, ip)
			}
		}
	}
	// IPv4 first: most scanner networks have no working IPv6 route
	sort.SliceStable(ips, func(i, j int) bool { return ips[i].Is4() && !ips[j].Is4() })
	var last error
	for _, ip := range ips {
		c, err := d.connect(ctx, net.JoinHostPort(ip.String(), port), ip, host, guarded)
		if err == nil {
			return c, nil
		}
		last = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, last
}

func (d *Dialer) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip}, nil
	}
	lookup := d.Lookup
	if lookup == nil {
		lookup = func(ctx context.Context, h string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", h)
		}
	}
	if ips, ok := testLookup(host); ok {
		return ips, nil
	}
	ips, err := lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoIP, host)
	}
	return ips, nil
}

func (d *Dialer) connect(ctx context.Context, addr string, ip netip.Addr, host string, guarded bool) (net.Conn, error) {
	timeout := d.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	nd := net.Dialer{Timeout: timeout}
	if guarded {
		nd.Control = func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return err
			}
			if !Allowed(ap.Addr()) {
				return fmt.Errorf("%w: %s", ErrBlockedAddr, ap.Addr())
			}
			return nil
		}
	}
	c, err := nd.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if d.Resolved != nil {
		d.Resolved(host, ip)
	}
	return c, nil
}

// Check refuses a host name up front, before a browser is started.
func (d *Dialer) Check(ctx context.Context, host string) error {
	if _, ok := testDial(host, "443"); ok {
		return nil
	}
	ips, err := d.resolve(ctx, host)
	if err != nil {
		return err
	}
	if guardDisabled() {
		return nil
	}
	for _, ip := range ips {
		if !Allowed(ip) {
			return fmt.Errorf("%w: %s resolves to %s", ErrBlockedAddr, host, ip)
		}
	}
	return nil
}
