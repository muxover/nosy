package site

import (
	"errors"
	"testing"
)

func TestParseReducesToHomepage(t *testing.T) {
	cases := map[string]string{
		"example.com":                          "https://example.com/",
		"  Example.COM  ":                      "https://example.com/",
		"https://www.example.com/a/b?x=1#frag": "https://www.example.com/",
		"http://example.com:80/x":              "http://example.com/",
		"https://example.com:443":              "https://example.com/",
		"https://user:pw@example.com/":         "https://example.com/",
		"https://example.com./":                "https://example.com/",
		"bücher.example":                       "https://xn--bcher-kva.example/",
		"https://例え.jp/path":                   "https://xn--r8jz45g.jp/",
		"https://XN--BCHER-KVA.example/":       "https://xn--bcher-kva.example/",
	}
	for in, want := range cases {
		u, err := Parse(in, false)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		if u.String() != want {
			t.Errorf("Parse(%q) = %q, want %q", in, u, want)
		}
	}
}

func TestParseRefuses(t *testing.T) {
	cases := map[string]error{
		"":                          ErrEmpty,
		"ftp://example.com":         ErrScheme,
		"file:///etc/passwd":        ErrScheme,
		"javascript:alert(1)":       ErrScheme,
		"https://example.com:8443/": ErrPort,
		"http://example.com:8080":   ErrPort,
		"http://127.0.0.1/":         ErrIP,
		"http://10.0.0.1":           ErrIP,
		"http://169.254.169.254/":   ErrIP,
		"http://[::1]/":             ErrIP,
		"http://[fd00::1]:80/":      ErrIP,
		"http://[::ffff:10.0.0.1]/": ErrIP,
		"http://2130706433/":        ErrIP,
		"http://0x7f.0.0.1/":        ErrIP,
		"http://127.1/":             ErrIP,
		"http://localhost/":         ErrLocal,
		"http://foo.localhost/":     ErrLocal,
		"http://intranet/":          ErrLocal,
		"https://exa mple.com/":     ErrInvalid,
		"https://exam_ple.com/":     ErrInvalid,
		"https://.example.com/":     ErrInvalid,
	}
	for in, want := range cases {
		_, err := Parse(in, false)
		if err == nil {
			t.Errorf("Parse(%q) accepted, want %v", in, want)
			continue
		}
		if !errors.Is(err, want) {
			t.Errorf("Parse(%q) = %v, want %v", in, err, want)
		}
	}
}

func TestParseAllowPrivate(t *testing.T) {
	cases := map[string]string{
		"localhost":                  "http://localhost/",
		"localhost:3000":             "http://localhost:3000/",
		"http://localhost:8080/app":  "http://localhost:8080/",
		"127.0.0.1":                  "http://127.0.0.1/",
		"127.0.0.1:8000":             "http://127.0.0.1:8000/",
		"https://127.0.0.1:8443/":    "https://127.0.0.1:8443/",
		"[::1]":                      "http://[::1]/",
		"[::1]:9000":                 "http://[::1]:9000/",
		"10.0.0.5":                   "http://10.0.0.5/",
		"intranet":                   "http://intranet/",
		"app.localhost":              "http://app.localhost/",
		"http://169.254.169.254/":    "http://169.254.169.254/",
		"example.com":                "https://example.com/",
		"https://example.com:8443/x": "https://example.com:8443/",
	}
	for in, want := range cases {
		u, err := Parse(in, true)
		if err != nil {
			t.Errorf("Parse(%q, true): %v", in, err)
			continue
		}
		if u.String() != want {
			t.Errorf("Parse(%q, true) = %q, want %q", in, u, want)
		}
	}
	for _, in := range []string{"", "ftp://localhost", "javascript:alert(1)", "https://exa mple.com/", "http://127.1/"} {
		if _, err := Parse(in, true); err == nil {
			t.Errorf("Parse(%q, true) accepted", in)
		}
	}
}
