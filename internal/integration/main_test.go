//go:build nosytest

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/muxover/nosy/internal/browser"
	"github.com/muxover/nosy/internal/detect"
	"github.com/muxover/nosy/internal/guard"
	"github.com/muxover/nosy/internal/scan"
	"github.com/muxover/nosy/internal/testsite"
)

var (
	enabled = os.Getenv("NOSY_BROWSER_TESTS") == "1"
	fixture *testsite.Site
	gate    = make(chan struct{}, parallel())
)

func TestMain(m *testing.M) {
	if !enabled {
		os.Exit(m.Run())
	}
	var err error
	fixture, err = testsite.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "setup:", err)
		os.Exit(1)
	}
	guard.Fixture("test", fixture.HTTP, fixture.TLS)
	guard.StubDNS("metadata.google.internal", "169.254.169.254")
	guard.StubDNS("rebind.example", "10.0.0.5")
	guard.StubDNS("metadata.example", "169.254.169.254")
	guard.StubDNS("v6.example", "fd00::1")
	guard.StubDNS("mixed.example", "93.184.216.34", "10.0.0.5")
	browser.SetTestTrust(fixture.CAPin)
	code := m.Run()
	fixture.Close()
	os.Exit(code)
}

// need skips a test when the browser tests were not asked for.
func need(t *testing.T) {
	t.Helper()
	if !enabled {
		t.Skip("set NOSY_BROWSER_TESTS=1 to run the planted-page tests (they need Chromium)")
	}
}

// parallel is how many browsers may run at once.
func parallel() int {
	n, err := strconv.Atoi(os.Getenv("NOSY_PARALLEL"))
	if err != nil || n < 1 || n > 8 {
		return 3
	}
	return n
}

// The domain data the planted pages are written against. Every domain ends in
// .test and is invented for these tests.
func testIndex() *detect.Index {
	return detect.NewIndex(map[string]detect.Domain{
		"ads.test":          {Owner: "AdNet Inc", Display: "AdNet", Categories: []string{"Advertising", "Ad Motivated Tracking"}, Fingerprinting: 2},
		"stats.test":        {Owner: "Stats Ltd", Display: "Stats", Categories: []string{"Analytics"}},
		"social.test":       {Owner: "Social Co", Display: "Social", Categories: []string{"Social - Share"}},
		"replayvendor.test": {Owner: "Replay Co", Display: "Replay", Categories: []string{"Session Replay"}},
		"fpvendor.test":     {Owner: "FP Vendor", Display: "FP Vendor", Categories: []string{"Analytics"}, Fingerprinting: 3},
		"keylog.test":       {Owner: "Keylog Co", Display: "Keylog", Categories: []string{"Analytics"}},
		"cdn.test":          {Owner: "CDN Co", Display: "CDN Co", Categories: []string{"CDN"}},
		"cmp.test":          {Owner: "CMP Co", Display: "CMP", Categories: []string{"Non-tracking"}},
		"tpcookie.test":     {Owner: "Cookie Co", Display: "Cookie Co", Categories: []string{"Advertising"}},
		"shared-cdn.test":   {Owner: "Shared Hosting", Display: "Shared Hosting", Categories: []string{"Advertising", "Analytics", "CDN", "Session Replay"}},
	}, map[string][]string{
		"Test Co": {"control.test", "trackers.test", "ownco.test"},
	})
}

func testBlocklist() *detect.Blocklist {
	bl, err := detect.ParseBlocklist([]byte(`{"trackers":{
  "shared-cdn.test": {"default": "ignore", "rules": [
    {"rule": "shared-cdn\\.test/collect"},
    {"rule": "shared-cdn\\.test/allowed/pixel", "action": "ignore"},
    {"rule": "shared-cdn\\.test/beacon", "exceptions": {"domains": ["exempt.test"]}},
    {"rule": "shared-cdn\\.test/img-only", "options": {"types": ["image"]}}]},
  "blockall.test": {"default": "block"}}}`))
	if err != nil {
		panic(err)
	}
	return bl
}

func testVendors() *detect.Vendors {
	v, err := detect.ParseVendors([]byte(`{"vendors":[
{"name":"Test recorder","kind":"recorder","source":"https://replayvendor.test/docs","match":[{"host":"replayvendor.test"}]},
{"name":"Test add-on","kind":"addon","source":"https://addon.test/docs","match":[{"host":"addon.test","path":"^/recorder[.]js"}]}]}`))
	if err != nil {
		panic(err)
	}
	return v
}

// options is a scan with the test rules: the invented domains the planted pages use.
func options() scan.Options {
	return scan.Options{
		Version: "test",
		Rules:   scan.Rules{Index: testIndex(), Blocklist: testBlocklist(), Vendors: testVendors(), Version: "test"},
	}
}

// runGated runs one scan with at most as many browsers open as the gate allows.
func runGated(input string, o scan.Options) (*scan.Result, error) {
	gate <- struct{}{}
	defer func() { <-gate }()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return scan.Run(ctx, input, o)
}

func scanTest(t *testing.T, input string, mods ...func(*scan.Options)) *scan.Result {
	t.Helper()
	need(t)
	o := options()
	o.Verbose = t.Logf
	for _, m := range mods {
		m(&o)
	}
	r, err := runGated(input, o)
	if err != nil {
		t.Fatalf("scan %s: %v", input, err)
	}
	if d := os.Getenv("NOSY_DUMP"); d != "" && strings.Contains(input, d) {
		b, _ := json.MarshalIndent(r, "", "  ")
		t.Logf("%s", b)
	}
	return r
}

func techniques(r *scan.Result) map[string][]string {
	m := map[string][]string{}
	for _, f := range r.Fingerprinting {
		m[f.Name] = f.Scripts
	}
	return m
}

func company(r *scan.Result, name string) (found bool, tracking bool) {
	for _, c := range r.Companies {
		if c.Name == name {
			return true, c.Tracking
		}
	}
	return false, false
}

func requestURLs(r *scan.Result) string {
	var s []string
	for _, q := range r.Requests {
		s = append(s, fmt.Sprintf("%s %s [%d %s]", q.Method, q.URL, q.Status, q.Failed))
	}
	return strings.Join(s, "\n")
}

func hasNote(r *scan.Result, sub string) bool {
	for _, n := range r.Notes {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}

func visits(host string) int {
	n := 0
	for _, p := range fixture.Hits(host) {
		if p == "/" {
			n++
		}
	}
	return n
}
