// Package site turns whatever a user typed into the one address a scan opens:
// the home page of a public site. It refuses what a scan must not reach
// (other schemes, odd ports, IP addresses, local names) unless the caller
// allows private targets.
package site

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/idna"
)

var (
	ErrEmpty   = errors.New("no address given")
	ErrScheme  = errors.New("only http and https links are scanned")
	ErrPort    = errors.New("only ports 80 and 443 are scanned")
	ErrIP      = errors.New("IP addresses are not scanned, use a domain name")
	ErrLocal   = errors.New("local names are not scanned")
	ErrInvalid = errors.New("not a valid domain name")
)

// InputError means the address was refused before anything was fetched: it
// was not allowed, or it resolved to a place a scan may not reach.
type InputError struct{ Err error }

func (e *InputError) Error() string { return e.Err.Error() }
func (e *InputError) Unwrap() error { return e.Err }

// a leading "word:" that is not host:port
var otherScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:([^0-9/]|$)`)

// Parse reduces whatever the user pasted to the home page of its site:
// scheme://host/. A bare domain gets https. Internationalised names become
// their ASCII form.
//
// With allowPrivate, IP addresses, local names, single-label names and any
// port are accepted, and a bare address of that kind gets http, since a local
// server rarely has a certificate.
func Parse(input string, allowPrivate bool) (*url.URL, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return nil, ErrEmpty
	}
	if !strings.Contains(s, "://") {
		if otherScheme.MatchString(s) {
			return nil, ErrScheme
		}
		scheme := "https://"
		if allowPrivate && looksLocal(s) {
			scheme = "http://"
		}
		s = scheme + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, ErrScheme
	}
	host, err := cleanHost(u.Hostname(), allowPrivate)
	if err != nil {
		return nil, err
	}
	port := u.Port()
	if !allowPrivate && port != "" && port != "80" && port != "443" {
		return nil, ErrPort
	}
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		host = joinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return &url.URL{Scheme: scheme, Host: host, Path: "/"}, nil
}

func joinHostPort(host, port string) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

// looksLocal is true for a bare address that names this machine or a network
// address rather than a public site.
func looksLocal(s string) bool {
	host := s
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	if strings.HasPrefix(host, "[") {
		return true
	}
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	host = strings.ToLower(host)
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || !strings.Contains(host, ".")
}

func cleanHost(h string, allowPrivate bool) (string, error) {
	h = strings.ToLower(strings.TrimSuffix(h, "."))
	if h == "" {
		return "", ErrInvalid
	}
	if ip, err := netip.ParseAddr(h); err == nil {
		if !allowPrivate {
			return "", ErrIP
		}
		return ip.String(), nil
	}
	if strings.ContainsAny(h, ":[]") {
		if !allowPrivate {
			return "", ErrIP
		}
		return "", ErrInvalid
	}
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		if !allowPrivate {
			return "", ErrLocal
		}
		return h, nil
	}
	ascii, err := idna.Lookup.ToASCII(h)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	h = ascii
	labels := strings.Split(h, ".")
	for _, l := range labels {
		if l == "" || len(l) > 63 {
			return "", ErrInvalid
		}
	}
	// browsers read a numeric last label as an IPv4 address (2130706433,
	// 0x7f.1, 127.1), so a real top-level domain must contain a letter
	last := labels[len(labels)-1]
	if !strings.ContainsAny(last, "abcdefghijklmnopqrstuvwxyz") || strings.HasPrefix(last, "0x") {
		if !allowPrivate {
			return "", ErrIP
		}
		return "", ErrInvalid
	}
	if len(labels) < 2 && !allowPrivate {
		return "", ErrLocal
	}
	for _, r := range h {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
			return "", ErrInvalid
		}
	}
	return h, nil
}
