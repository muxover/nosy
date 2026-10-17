package guard

import (
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
	Reason string `json:"reason"`
}

type Stats struct {
	BytesDown int64
	BytesUp   int64
	HostIPs   map[string][]string
	Refused   []Refusal
	CapHit    bool
}

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

	mu      sync.Mutex
	hostIPs map[string]map[string]struct{}
	refused []Refusal
	conns   map[net.Conn]struct{}
}

// StartProxy listens on a free loopback port.
func StartProxy(d *Dialer, maxBytes int64, onCap func()) (*Proxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &Proxy{
		dialer:   d,
		maxBytes: maxBytes,
		onCap:    onCap,
		ln:       ln,
		hostIPs:  map[string]map[string]struct{}{},
		conns:    map[net.Conn]struct{}{},
	}
	d.Resolved = p.resolved
	d.Refused = p.refuse
	p.rt = &http.Transport{
		DialContext:           d.DialContext,
		DisableCompression:    true,
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: 20 * time.Second,
	}
	p.srv = &http.Server{Handler: p, ReadHeaderTimeout: 10 * time.Second}
	go p.srv.Serve(ln)
	return p, nil
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
	p.refused = append(p.refused, Refusal{Host: host, Port: port, Reason: err.Error()})
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
	done := make(chan struct{}, 2)
	go func() {
		n, _ := io.Copy(up, client)
		p.up.Add(n)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(countingWriter{client, p}, up)
		done <- struct{}{}
	}()
	<-done
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
	io.Copy(countingWriter{w, p}, resp.Body)
}

// OnCap sets what happens when the size cap is reached.
func (p *Proxy) OnCap(fn func()) {
	p.mu.Lock()
	p.onCap = fn
	p.mu.Unlock()
}
