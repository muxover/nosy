package detect

import (
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/muxover/nosy/internal/capture"
)

const (
	minIDLength  = 8
	minIDEntropy = 2.5 // bits per character
	minLifetime  = 24 * time.Hour
)

func Entropy(s string) float64 {
	if s == "" {
		return 0
	}
	counts := map[rune]int{}
	n := 0
	for _, r := range s {
		counts[r]++
		n++
	}
	var h float64
	for _, c := range counts {
		p := float64(c) / float64(n)
		h -= p * math.Log2(p)
	}
	return h
}

// IsIdentifier applies the identifier cookie rule: it outlives a day and
// its value looks random and long enough to single out a browser. Values
// that are plainly structured (settings strings, JSON, timestamps) are
// not random.
func IsIdentifier(c capture.Cookie, now time.Time) bool {
	if c.Session || c.Expires <= 0 {
		return false
	}
	if time.Unix(int64(c.Expires), 0).Sub(now) <= minLifetime {
		return false
	}
	return LooksLikeIdentifier(c.Value)
}

// LooksLikeIdentifier is the value half of the identifier rule, for anything
// that outlives a visit: long enough, random enough, and not plainly a date,
// a timestamp, a word or a structured string.
func LooksLikeIdentifier(v string) bool {
	if u, err := url.QueryUnescape(v); err == nil {
		v = u
	}
	if len(v) < minIDLength || strings.ContainsAny(v, " \t\n") {
		return false
	}
	if strings.HasPrefix(v, "{") || strings.HasPrefix(v, "[") || (strings.Contains(v, "&") && strings.Contains(v, "=")) {
		return false
	}
	if isTimestamp(v) || looksLikeDate(v) {
		return false
	}
	// an identifier mixes in digits; plain words (accepted-all) are settings
	if !strings.ContainsAny(v, "0123456789") {
		return false
	}
	return Entropy(v) >= minIDEntropy
}

func isTimestamp(v string) bool {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return false
	}
	return (n >= 1e9 && n <= 4e9) || (n >= 1e12 && n <= 4e12)
}

func IdentifierCookies(cookies []capture.Cookie, now time.Time) []int {
	var out []int
	for i, c := range cookies {
		if IsIdentifier(c, now) {
			out = append(out, i)
		}
	}
	return out
}

var dateLayouts = []string{
	"02-Jan-2006", "2-Jan-2006", "02-Jan-06", "2006-01-02", "2006/01/02", "01/02/2006", "02/01/2006", "20060102",
	"Jan 2, 2006", "2 Jan 2006", "2006-01-02 15:04:05", "2006-01-02T15:04:05", "20060102T150405",
	time.RFC3339, time.RFC3339Nano, time.RFC1123, time.RFC1123Z, time.RFC850, time.ANSIC,
}

// looksLikeDate catches values such as 01-Oct-2026: digits and letters
// mixed, but a date, not a random identifier.
func looksLikeDate(v string) bool {
	v = strings.ReplaceAll(v, "+", " ")
	for _, l := range dateLayouts {
		if _, err := time.Parse(l, v); err == nil {
			return true
		}
	}
	return false
}
