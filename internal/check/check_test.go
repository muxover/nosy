package check

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/muxover/nosy/internal/guard"
	"github.com/muxover/nosy/internal/site"
)

func run(t *testing.T, input string, o Options) (*Result, error) {
	t.Helper()
	o.AllowPrivate = true
	return Run(context.Background(), input, o)
}

func byID(r *Result, id string) *Check {
	for i := range r.Checks {
		if r.Checks[i].ID == id {
			return &r.Checks[i]
		}
	}
	return nil
}

func TestPlainHomePageWithCookies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "uid=9f86d081884c7d659a2feaa0c55ad015; Max-Age=31536000; Path=/")
		w.Header().Add("Set-Cookie", "theme=dark; Path=/")
		w.Header().Add("Set-Cookie", "bm_sz=A1B2C3D4E5F60718293A4B5C6D7E8F90; Max-Age=3600; Path=/")
		w.Write([]byte("hello"))
	}))
	defer srv.Close()
	r, err := run(t, srv.URL, Options{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != 200 || r.FinalURL != srv.URL+"/" || len(r.Addresses) != 1 || r.Addresses[0] != "127.0.0.1" {
		t.Errorf("result = %+v", r)
	}
	if len(r.Cookies) != 3 {
		t.Fatalf("cookies = %+v", r.Cookies)
	}
	byName := map[string]Cookie{}
	for _, c := range r.Cookies {
		byName[c.Name] = c
	}
	if !byName["uid"].Identifier || byName["uid"].Days < 364 || byName["theme"].Identifier || !byName["theme"].Session || !byName["bm_sz"].AntiBot {
		t.Errorf("cookies = %+v", byName)
	}
	if c := byID(r, "https"); c == nil || c.OK {
		t.Errorf("https check = %+v, want a plain-HTTP warning", c)
	}
	if c := byID(r, "identifier-cookies"); c == nil || c.OK || c.Label != Classified || !strings.Contains(c.Text, "uid") {
		t.Errorf("identifier check = %+v", c)
	}
	if c := byID(r, "first-cookies"); c == nil || c.Label != Observed || !strings.Contains(c.Text, "3 cookies") {
		t.Errorf("cookie check = %+v", c)
	}
	if byID(r, "answer") == nil || !byID(r, "answer").OK {
		t.Errorf("answer check = %+v", byID(r, "answer"))
	}
}

func TestHTTPSAndCleanFirstAnswer(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("hello")) }))
	defer srv.Close()
	tlsCfg := srv.Client().Transport.(*http.Transport).TLSClientConfig
	r, err := run(t, srv.URL, Options{TLS: tlsCfg})
	if err != nil {
		t.Fatal(err)
	}
	if c := byID(r, "https"); c == nil || !c.OK {
		t.Errorf("https check = %+v", c)
	}
	if c := byID(r, "first-cookies"); c == nil || !strings.Contains(c.Text, "no cookies") || byID(r, "identifier-cookies") != nil {
		t.Errorf("cookie check = %+v", c)
	}
	// without the test CA the same site fails to verify
	if _, err := run(t, srv.URL, Options{TLS: &tls.Config{}}); err == nil {
		t.Error("a certificate from an unknown authority was accepted")
	}
}

func TestRedirectsAreFollowedAndListed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "first=1a2b3c4d5e6f7a8b9c0d; Max-Age=86400000; Path=/")
		http.Redirect(w, r, "/a", http.StatusFound)
	})
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/b", http.StatusMovedPermanently) })
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("done")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	r, err := run(t, srv.URL, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Redirects) != 2 || !strings.HasSuffix(r.FinalURL, "/b") {
		t.Errorf("redirects %v final %s", r.Redirects, r.FinalURL)
	}
	if len(r.Cookies) != 1 || r.Cookies[0].Name != "first" {
		t.Errorf("the cookie set on the first hop was lost: %+v", r.Cookies)
	}
}

func TestRedirectLoopFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", http.StatusFound) }))
	defer srv.Close()
	if _, err := run(t, srv.URL, Options{}); err == nil || !strings.Contains(err.Error(), "redirects") {
		t.Errorf("error = %v, want too many redirects", err)
	}
}

func TestErrorAnswerIsFlagged(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	r, err := run(t, srv.URL, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if c := byID(r, "answer"); c == nil || c.OK || !strings.Contains(c.Text, "404") {
		t.Errorf("answer check = %+v", c)
	}
}

func TestPrivateTargetsAreRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("hello")) }))
	defer srv.Close()
	var ie *site.InputError
	if _, err := Run(context.Background(), srv.URL, Options{}); !errors.As(err, &ie) || !errors.Is(err, site.ErrIP) {
		t.Errorf("a loopback address = %v, want an input error for an IP address", err)
	}
	stub := &guard.Dialer{Lookup: func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}}
	if _, err := Run(context.Background(), "http://rebind.example/", Options{Dialer: stub}); !errors.As(err, &ie) || !errors.Is(err, guard.ErrBlockedAddr) {
		t.Errorf("a name that resolves to loopback = %v, want an input error for a blocked address", err)
	}
}
