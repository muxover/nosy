package guard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"sync"
	"syscall"
	"time"
)

// AllowedPorts are the only destination ports a scan may reach.
var AllowedPorts = map[string]bool{"80": true, "443": true}

var (
	ErrPort      = errors.New("port is not 80 or 443")
	ErrNoIP      = errors.New("host did not resolve")
	ErrLocalName = errors.New("name only exists on the local network")
)

// Blocked is a refusal that says what kind of address was refused.
type Blocked struct {
	Host string
	Kind Kind
	Err  error
}

func (b *Blocked) Error() string { return b.Err.Error() }
func (b *Blocked) Unwrap() error { return b.Err }

// KindOf is the kind of address a refusal was about, empty for other errors.
func KindOf(err error) Kind {
	var b *Blocked
	if errors.As(err, &b) {
		return b.Kind
	}
	return ""
}

// Dialer resolves host names itself and connects only to public addresses.
// Each answer is checked, and the address checked is the address dialed: there
// is no second lookup, and the socket is checked again right before connect.
type Dialer struct {
	Lookup  func(ctx context.Context, host string) ([]netip.Addr, error)
	Timeout time.Duration

	// AllowPrivate lets the dialer reach the user's own machine and network
	// (loopback, private, link-local, CGNAT, local names) and any port, for a
	// user who scans a site of their own. Cloud metadata and the addresses in
	// never stay refused.
	AllowPrivate bool

	// Resolved is told which address a host name was dialed at.
	Resolved func(host string, ip netip.Addr)
	// Refused is told about every connection the guard turned away.
	Refused func(host, port string, err error)

	mu    sync.Mutex
	never map[netip.AddrPort]bool

	// dialTCP replaces the connect, in tests
	dialTCP func(ctx context.Context, nd *net.Dialer, addr string) (net.Conn, error)
}

// Never refuses addr whatever else is allowed: the guard's own port and the
// browser's DevTools port, which --allow-private would otherwise open.
func (d *Dialer) Never(addr netip.AddrPort) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.never == nil {
		d.never = map[netip.AddrPort]bool{}
	}
	d.never[addr] = true
}

func (d *Dialer) isNever(ap netip.AddrPort) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.never[netip.AddrPortFrom(ap.Addr().Unmap().WithZone(""), ap.Port())]
}

func (d *Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	conn, err := d.dial(ctx, host, port)
	if err != nil && (errors.Is(err, ErrBlockedAddr) || errors.Is(err, ErrPort) || errors.Is(err, ErrLocalName)) && d.Refused != nil {
		d.Refused(host, port, err)
	}
	return conn, err
}

// permit says whether ip may be dialed, and why not.
func (d *Dialer) permit(host string, ip netip.Addr) error {
	k := Classify(ip)
	if k == Public || (d.AllowPrivate && k.Local()) || guardDisabled() && k != Metadata {
		return nil
	}
	return &Blocked{Host: host, Kind: k, Err: fmt.Errorf("%w: %s is %s (%s)", ErrBlockedAddr, host, ip, k)}
}

func (d *Dialer) dial(ctx context.Context, host, port string) (net.Conn, error) {
	if to, ok := testDial(host, port); ok {
		return d.connect(ctx, to.addr, to.ip, host, false)
	}
	if !d.AllowPrivate && !AllowedPorts[port] {
		// an address that is refused whatever the port is named by its kind
		if ip, ok := ParseHost(host); ok {
			if err := d.permit(host, ip); err != nil {
				return nil, err
			}
		}
		if IsLocalName(host) {
			return nil, &Blocked{Host: host, Kind: LocalName, Err: fmt.Errorf("%w: %s", ErrLocalName, host)}
		}
		return nil, fmt.Errorf("%w: %s", ErrPort, port)
	}
	ips, err := d.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	// only the permitted answers are ever dialed; a name that has none is refused
	var ok []netip.Addr
	var first error
	for _, ip := range ips {
		if err := d.permit(host, ip); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		ok = append(ok, ip)
	}
	if len(ok) == 0 {
		return nil, first
	}
	// IPv4 first: most networks have no working IPv6 route
	sort.SliceStable(ok, func(i, j int) bool { return ok[i].Is4() && !ok[j].Is4() })
	var last error
	for _, ip := range ok {
		c, err := d.connect(ctx, net.JoinHostPort(ip.String(), port), ip, host, true)
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

// Resolve returns the addresses of host as the dialer would dial them.
func (d *Dialer) Resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	return d.resolve(ctx, host)
}

func (d *Dialer) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, ok := ParseHost(host); ok {
		return []netip.Addr{ip}, nil
	}
	if !d.AllowPrivate && IsLocalName(host) {
		return nil, &Blocked{Host: host, Kind: LocalName, Err: fmt.Errorf("%w: %s", ErrLocalName, host)}
	}
	if ips, ok := testLookup(host); ok {
		return ips, nil
	}
	lookup := d.Lookup
	if lookup == nil {
		lookup = func(ctx context.Context, h string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", h)
		}
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
	nd := &net.Dialer{Timeout: timeout}
	if guarded {
		// the socket is checked again: the address connected to is the one checked
		nd.Control = func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return err
			}
			if d.isNever(ap) {
				return &Blocked{Host: host, Kind: Loopback, Err: fmt.Errorf("%w: %s is Nosy's own", ErrBlockedAddr, ap)}
			}
			return d.permit(host, ap.Addr())
		}
	}
	dialTCP := d.dialTCP
	if dialTCP == nil {
		dialTCP = func(ctx context.Context, nd *net.Dialer, addr string) (net.Conn, error) {
			return nd.DialContext(ctx, "tcp", addr)
		}
	}
	c, err := dialTCP(ctx, nd, addr)
	if err != nil {
		return nil, err
	}
	if d.Resolved != nil {
		d.Resolved(host, ip)
	}
	return c, nil
}

// Check refuses a host name up front, before a browser is started: when it
// does not resolve, or has no answer a scan may reach.
func (d *Dialer) Check(ctx context.Context, host string) error {
	if _, ok := testDial(host, "443"); ok {
		return nil
	}
	ips, err := d.resolve(ctx, host)
	if err != nil {
		return err
	}
	var first error
	for _, ip := range ips {
		err := d.permit(host, ip)
		if err == nil {
			return nil
		}
		if first == nil {
			first = err
		}
	}
	return first
}
