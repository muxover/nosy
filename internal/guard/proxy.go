package guard

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// RefusedHeader marks a plain-HTTP response the proxy produced by refusing
// the request, so the scan can tell it from a site's own 403.
const RefusedHeader = "X-Nosy-Refused"

// UpstreamErrorHeader marks a plain-HTTP response the proxy produced
// because the site could not be reached, so it is not mistaken for the
// site answering with an error.
const UpstreamErrorHeader = "X-Nosy-Upstream-Error"

type Refusal struct {
	Host   string `json:"host"`
	Port   string `json:"port"`
	Kind   Kind   `json:"kind,omitempty"`
	Reason string `json:"reason"`
}

// Limits counts the times a cap below the size cap was reached.
type Limits struct {
	// RequestCap is how many answers were cut at MaxRequestBytes. Inside an
	// encrypted tunnel the guard cannot see requests, so there it is the
	// connection that is capped.
	RequestCap int
	// Connections is how many connections were refused because too many were open.
	Connections int
	// Idle is how many connections were closed because the server stayed silent
	// for IdleTimeout while the browser waited for an answer.
	Idle int
	// SlowAnswers is how many requests got no answer within HeaderTimeout.
	SlowAnswers int
}

type Stats struct {
	BytesDown int64
	BytesUp   int64
	HostIPs   map[string][]string
	Refused   []Refusal
	CapHit    bool
	Limits    Limits
	// Unauthorized is how many requests came without the scan's secret.
	Unauthorized int
}

// The caps on one connection and on the number open at once. A hostile or slow
// server cannot hold a scan past them.
const (
	MaxRequestBytes int64 = 25 << 20
	MaxConns              = 128
)

// The waits for a server; variables so tests can shorten them.
var (
	IdleTimeout   = 15 * time.Second
	HeaderTimeout = 10 * time.Second
)

// Proxy is the only way out for the browser. It resolves every name
// itself, refuses non-public addresses and ports other than 80 and 443,
// counts bytes against the size cap, and records which address each host
// was reached at.
type Proxy struct {
	dialer   *Dialer
	maxBytes int64
	onCap    func()

	ln  net.Listener
	srv *http.Server
	rt  *http.Transport

	down, up atomic.Int64
	capOnce  sync.Once
	capHit   atomic.Bool

	secret string
	open   atomic.Int64

	mu      sync.Mutex
	hostIPs map[string]map[string]struct{}
	refused []Refusal
	conns   map[net.Conn]struct{}
	limits  Limits
	noAuth  int
}

// StartProxy listens on a free port of 127.0.0.1, for one scan. Every request
// must carry the scan's secret as proxy credentials (user "nosy"); the browser
// is given it, nothing else on the machine is.
func StartProxy(d *Dialer, maxBytes int64, onCap func()) (*Proxy, error) {
	key := make([]byte, 24)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &Proxy{
		dialer:   d,
		maxBytes: maxBytes,
		onCap:    onCap,
		ln:       ln,
		secret:   hex.EncodeToString(key),
		hostIPs:  map[string]map[string]struct{}{},
		conns:    map[net.Conn]struct{}{},
	}
	d.Resolved = p.resolved
	d.Refused = p.refuse
	d.Never(netip.MustParseAddrPort(ln.Addr().String()))
	p.rt = &http.Transport{
		// Proxy is left nil: the guard goes out directly, whatever HTTP_PROXY,
		// HTTPS_PROXY or the system says
		DialContext:           d.DialContext,
		DisableCompression:    true,
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: HeaderTimeout,
	}
	p.srv = &http.Server{Handler: p, ReadHeaderTimeout: HeaderTimeout, IdleTimeout: IdleTimeout}
	go p.srv.Serve(ln)
	return p, nil
}

// Never refuses addr for the rest of the scan, whatever else is allowed.
func (p *Proxy) Never(addr netip.AddrPort) { p.dialer.Never(addr) }

// User and Secret are the proxy credentials the browser must send.
func (p *Proxy) User() string   { return "nosy" }
func (p *Proxy) Secret() string { return p.secret }

func (p *Proxy) authorized(r *http.Request) bool {
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(p.User()+":"+p.secret))
	return subtle.ConstantTimeCompare([]byte(r.Header.Get("Proxy-Authorization")), []byte(want)) == 1
}

func (p *Proxy) count(f func(*Limits)) {
	p.mu.Lock()
	f(&p.limits)
	p.mu.Unlock()
}

func (p *Proxy) Addr() string { return p.ln.Addr().String() }

func (p *Proxy) Close() {
	p.srv.Close()
	p.closeConns()
}

func (p *Proxy) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := Stats{
		BytesDown: p.down.Load(),
		BytesUp:   p.up.Load(),
		HostIPs:   map[string][]string{},
		Refused:   append([]Refusal(nil), p.refused...),
		CapHit:    p.capHit.Load(),
		Limits:    p.limits,

		Unauthorized: p.noAuth,
	}
	for h, ips := range p.hostIPs {
		for ip := range ips {
			s.HostIPs[h] = append(s.HostIPs[h], ip)
		}
		sort.Strings(s.HostIPs[h])
	}
	return s
}

func (p *Proxy) resolved(host string, ip netip.Addr) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hostIPs[host] == nil {
		p.hostIPs[host] = map[string]struct{}{}
	}
	p.hostIPs[host][ip.String()] = struct{}{}
}

func (p *Proxy) refuse(host, port string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refused = append(p.refused, Refusal{Host: host, Port: port, Kind: KindOf(err), Reason: err.Error()})
}

func (p *Proxy) addDown(n int64) {
	if p.down.Add(n) > p.maxBytes && p.maxBytes > 0 {
		p.capOnce.Do(func() {
			p.capHit.Store(true)
			p.mu.Lock()
			fn := p.onCap
			p.mu.Unlock()
			if fn != nil {
				fn()
			}
			p.closeConns()
		})
	}
}

func (p *Proxy) track(c net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.capHit.Load() {
		return false
	}
	p.conns[c] = struct{}{}
	return true
}

func (p *Proxy) untrack(c net.Conn) {
	p.mu.Lock()
	delete(p.conns, c)
	p.mu.Unlock()
}

func (p *Proxy) closeConns() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for c := range p.conns {
		c.Close()
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !p.authorized(r) {
		p.mu.Lock()
		p.noAuth++
		p.mu.Unlock()
		w.Header().Set("Proxy-Authenticate", `Basic realm="nosy"`)
		http.Error(w, "proxy credentials required", http.StatusProxyAuthRequired)
		return
	}
	if p.open.Add(1) > MaxConns {
		p.open.Add(-1)
		p.count(func(l *Limits) { l.Connections++ })
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}
	defer p.open.Add(-1)
	if r.Method == http.MethodConnect {
		p.tunnel(w, r)
		return
	}
	p.forward(w, r)
}

func (p *Proxy) tunnel(w http.ResponseWriter, r *http.Request) {
	up, err := p.dialer.DialContext(r.Context(), "tcp", r.Host)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, ErrBlockedAddr) || errors.Is(err, ErrPort) {
			status = http.StatusForbidden
		}
		http.Error(w, "refused", status)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		up.Close()
		http.Error(w, "unsupported", http.StatusInternalServerError)
		return
	}
	client, _, err := hj.Hijack()
	if err != nil {
		up.Close()
		return
	}
	if !p.track(client) {
		client.Close()
		up.Close()
		return
	}
	p.track(up)
	defer func() {
		p.untrack(client)
		p.untrack(up)
		client.Close()
		up.Close()
	}()
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	t := &tunnelClock{}
	done := make(chan struct{}, 2)
	go func() {
		n, _ := io.Copy(stampWriter{up, &t.asked}, client)
		p.up.Add(n)
		done <- struct{}{}
	}()
	go func() {
		p.copyCapped(stampWriter{countingWriter{client, p}, &t.answered}, idleReader{up, p, t})
		done <- struct{}{}
	}()
	<-done
}

// tunnelClock remembers when the browser last sent something and when the
// server last answered, so a silent server can be told from an idle connection.
type tunnelClock struct{ asked, answered atomic.Int64 }

type stampWriter struct {
	w io.Writer
	t *atomic.Int64
}

func (s stampWriter) Write(b []byte) (int, error) {
	s.t.Store(time.Now().UnixNano())
	return s.w.Write(b)
}

// idleReader closes the server's side after IdleTimeout of silence. It counts
// as a slow server only when the browser was waiting for an answer.
type idleReader struct {
	c net.Conn
	p *Proxy
	t *tunnelClock
}

func (r idleReader) Read(b []byte) (int, error) {
	r.c.SetReadDeadline(time.Now().Add(IdleTimeout))
	n, err := r.c.Read(b)
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() && r.t.asked.Load() > r.t.answered.Load() {
		r.p.count(func(l *Limits) { l.Idle++ })
	}
	return n, err
}

// copyCapped copies one answer and cuts it at MaxRequestBytes.
func (p *Proxy) copyCapped(w io.Writer, r io.Reader) {
	n, _ := io.Copy(w, io.LimitReader(r, MaxRequestBytes+1))
	if n > MaxRequestBytes {
		p.count(func(l *Limits) { l.RequestCap++ })
	}
}

type countingWriter struct {
	w io.Writer
	p *Proxy
}

func (c countingWriter) Write(b []byte) (int, error) {
	c.p.addDown(int64(len(b)))
	return c.w.Write(b)
}

var hopHeaders = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

func (p *Proxy) forward(w http.ResponseWriter, r *http.Request) {
	if r.URL.Scheme != "http" || r.URL.Host == "" {
		http.Error(w, "unsupported", http.StatusBadRequest)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	for _, h := range hopHeaders {
		out.Header.Del(h)
	}
	resp, err := p.rt.RoundTrip(out)
	var ne net.Error
	if err != nil && errors.As(err, &ne) && ne.Timeout() {
		p.count(func(l *Limits) { l.SlowAnswers++ })
	}
	if err != nil {
		if errors.Is(err, ErrBlockedAddr) || errors.Is(err, ErrPort) {
			w.Header().Set(RefusedHeader, "1")
			http.Error(w, "refused", http.StatusForbidden)
			return
		}
		// drop the connection rather than answer: a 502 from the proxy could be
		// mistaken for the site replying once the browser's own read
		// protections hide its body
		if hj, ok := w.(http.Hijacker); ok {
			if c, _, err := hj.Hijack(); err == nil {
				c.Close()
				return
			}
		}
		w.Header().Set(UpstreamErrorHeader, "1")
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for _, h := range hopHeaders {
		resp.Header.Del(h)
	}
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	p.copyCapped(countingWriter{w, p}, resp.Body)
}

// OnCap sets what happens when the size cap is reached.
func (p *Proxy) OnCap(fn func()) {
	p.mu.Lock()
	p.onCap = fn
	p.mu.Unlock()
}
