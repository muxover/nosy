//go:build nosytest

// Package testsite serves the planted test pages. It is compiled only into
// test builds (-tags nosytest) and never reaches the production binary.
package testsite

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Site struct {
	HTTP  string // address of the plain HTTP listener
	TLS   string // address of the TLS listener
	CAPin string // base64 SHA-256 of the test CA's public key

	// Canary listens where the scanner must never connect. CanaryPort is
	// the loopback port 80 listener, when it could be opened.
	Canary     *Canary
	CanaryPort *Canary

	// HopCanary is the end of the redirect chain that must be refused: nothing else
	// uses it, so a connection to it can only come from that chain.
	HopCanary *Canary

	servers []*http.Server
	lns     []net.Listener

	mu       sync.Mutex
	envHits  []EnvHit
	hits     map[string][]string
	counters map[string]int
	targets  []string

	ca     *x509.Certificate
	caKey  *ecdsa.PrivateKey
	leafMu sync.Mutex
	leaves map[string]*tls.Certificate
}

// Canary counts connections to a listener that should never be reached.
type Canary struct {
	Addr string
	ln   net.Listener
	n    atomic.Int64
}

func (c *Canary) Conns() int64 { return c.n.Load() }
func (c *Canary) Close()       { c.ln.Close() }

func newCanary(addr string) (*Canary, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	c := &Canary{Addr: ln.Addr().String(), ln: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			c.n.Add(1)
			conn.Close()
		}
	}()
	return c, nil
}

func Start() (*Site, error) {
	s := &Site{hits: map[string][]string{}, counters: map[string]int{}, leaves: map[string]*tls.Certificate{}}
	if err := s.newCA(); err != nil {
		return nil, err
	}
	canary, err := newCanary("127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s.Canary = canary
	// port 80 on loopback needs no privileges inside a container; if it is
	// not available the address tests that need it are skipped
	s.CanaryPort, _ = newCanary("127.0.0.1:80")
	s.HopCanary, _ = newCanary("127.0.0.2:80")

	hl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	tl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s.HTTP, s.TLS = hl.Addr().String(), tl.Addr().String()
	h := http.HandlerFunc(s.serve)
	plain := &http.Server{Handler: h}
	secure := &http.Server{Handler: h}
	cfg := &tls.Config{GetCertificate: s.leaf, NextProtos: []string{"http/1.1"}}
	go plain.Serve(hl)
	go secure.Serve(tls.NewListener(tl, cfg))
	s.servers = []*http.Server{plain, secure}
	s.lns = []net.Listener{hl, tl}
	return s, nil
}

// CAPool is a certificate pool that trusts the test CA, for clients that are
// not the browser.
func (s *Site) CAPool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(s.ca)
	return p
}

func (s *Site) Close() {
	for _, srv := range s.servers {
		srv.Close()
	}
	s.Canary.Close()
	if s.CanaryPort != nil {
		s.CanaryPort.Close()
	}
	if s.HopCanary != nil {
		s.HopCanary.Close()
	}
}

// Hits lists the paths requested from host so far.
func (s *Site) Hits(host string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.hits[host]...)
}

func (s *Site) newCA() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "nosy test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	ca, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	s.ca, s.caKey = ca, key
	sum := sha256.Sum256(ca.RawSubjectPublicKeyInfo)
	s.CAPin = base64.StdEncoding.EncodeToString(sum[:])
	return nil
}

func (s *Site) leaf(hi *tls.ClientHelloInfo) (*tls.Certificate, error) {
	name := strings.ToLower(hi.ServerName)
	if name == "" {
		name = "localhost"
	}
	s.leafMu.Lock()
	defer s.leafMu.Unlock()
	if c, ok := s.leaves[name]; ok {
		return c, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, s.ca, &key.PublicKey, s.caKey)
	if err != nil {
		return nil, err
	}
	c := &tls.Certificate{Certificate: [][]byte{der, s.ca.Raw}, PrivateKey: key}
	s.leaves[name] = c
	return c, nil
}

func hostOnly(h string) string {
	if i := strings.LastIndexByte(h, ':'); i >= 0 && !strings.HasSuffix(h, "]") {
		h = h[:i]
	}
	return strings.ToLower(h)
}

func (s *Site) serve(w http.ResponseWriter, r *http.Request) {
	host := hostOnly(r.Host)
	s.mu.Lock()
	s.hits[host] = append(s.hits[host], r.URL.Path)
	s.mu.Unlock()
	if h, ok := s.route(host); ok {
		h(w, r)
		return
	}
	http.NotFound(w, r)
}

func html(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}

// page wraps body in a document with enough text to count as a real page.
func page(title, body string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>` + title + `</title></head><body><main>` +
		`<h1>` + title + `</h1><p>This is a planted test page used to check one detector of the scanner. ` +
		`It has a few sentences of text so that it is never mistaken for an empty page.</p>` + filler(600) + body +
		`<div style="height:2400px"></div><p>End of the page.</p></main></body></html>`
}

// filler is n characters of visible text in ordinary paragraphs.
func filler(n int) string {
	const sentence = "A plain paragraph of text that a visitor could read on a real page. "
	var b strings.Builder
	for b.Len() < n {
		b.WriteString("<p>" + sentence + "</p>")
	}
	return b.String()
}

func script(w http.ResponseWriter, js string) {
	w.Header().Set("Content-Type", "application/javascript")
	fmt.Fprint(w, js)
}

var gif = []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04\x01\x00\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;")

func pixel(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/gif")
	w.Write(gif)
}

func noContent(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusNoContent)
}

// EnvHit is a request to env.test, with the headers that say how the browser
// presented itself.
type EnvHit struct {
	Path      string
	Query     string
	GPCHeader string
	UserAgent string
}

// EnvHits lists the requests env.test has had, in order.
func (s *Site) EnvHits() []EnvHit {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]EnvHit(nil), s.envHits...)
}
