package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/muxover/nosy/internal/check"
	"github.com/muxover/nosy/internal/detect"
	"github.com/muxover/nosy/internal/scan"
	"github.com/muxover/nosy/internal/score"
)

// the test binary stands in for a browser that exists, for the tests that
// stop before it would be started
func fakeChrome(t *testing.T, h *harness) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h.env["NOSY_CHROME"] = exe
}

func TestHelpAndVersion(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{}, {"help"}, {"-h"}, {"--help"}} {
		if code := h.run(args...); code != exitOK || !strings.Contains(h.out.String(), "Usage:") || !strings.Contains(h.out.String(), "130 interrupted") {
			t.Errorf("%v: code %d, output %q", args, code, h.out.String())
		}
	}
	for _, c := range []string{"scan", "check", "browser", "config", "doctor"} {
		if !strings.Contains(h.out.String(), c) {
			t.Errorf("help does not list %s", c)
		}
	}
	for _, line := range strings.Split(h.out.String(), "\n") {
		for _, name := range []string{"report", "rules", "serve"} {
			if f := strings.Fields(line); len(f) > 0 && f[0] == name {
				t.Errorf("help lists %s, which does not exist yet", name)
			}
		}
	}
	for _, args := range [][]string{{"version"}, {"--version"}} {
		if code := h.run(args...); code != exitOK || h.out.String() != "nosy "+nosyVersion()+"\n" {
			t.Errorf("%v: code %d, output %q", args, code, h.out.String())
		}
	}
	for _, c := range []string{"scan", "check", "browser", "config", "doctor"} {
		if code := h.run("help", c); code != exitOK || !strings.Contains(h.out.String(), "Usage: nosy "+c) {
			t.Errorf("help %s: code %d, output %q", c, code, h.out.String())
		}
	}
	if code := h.run("help", "nope"); code != exitUsage {
		t.Errorf("help nope: code %d", code)
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	h := newHarness(t)
	for _, c := range []string{"nope", "report", "rules", "serve"} {
		if code := h.run(c); code != exitUsage || !strings.Contains(h.err.String(), "unknown command") || h.out.Len() != 0 {
			t.Errorf("%s: code %d, stderr %q", c, code, h.err.String())
		}
	}
}

func TestScanUsageErrors(t *testing.T) {
	h := newHarness(t)
	fakeChrome(t, h)
	cases := map[string][]string{
		"no address":         {"scan"},
		"two addresses":      {"scan", "a.example", "b.example"},
		"unknown flag":       {"scan", "--nope", "a.example"},
		"duration text":      {"scan", "--duration", "soon", "a.example"},
		"duration too low":   {"scan", "--duration", "5", "a.example"},
		"duration too big":   {"scan", "--duration", "3m", "a.example"},
		"device":             {"scan", "--device", "tablet", "a.example"},
		"flag without value": {"scan", "a.example", "--duration"},
	}
	for name, args := range cases {
		if code := h.run(args...); code != exitUsage || h.out.Len() != 0 || !strings.Contains(h.err.String(), "nosy scan -h") {
			t.Errorf("%s: code %d, stdout %q, stderr %q", name, code, h.out.String(), h.err.String())
		}
	}
	if code := h.run("scan", "-h"); code != exitOK {
		t.Errorf("scan -h: code %d", code)
	}
	for _, flag := range []string{"--duration", "--device", "--gpc", "--allow-private", "--json", "--quiet"} {
		if !strings.Contains(h.out.String(), flag) {
			t.Errorf("scan -h does not document %s", flag)
		}
	}
}

func TestFlagsMayFollowTheAddress(t *testing.T) {
	fs := newFlagSet("scan")
	asJSON := fs.Bool("json", false, "")
	quiet := fs.Bool("quiet", false, "")
	pos, err := parseFlags(fs, []string{"example.com", "--json", "-quiet"})
	if err != nil || len(pos) != 1 || pos[0] != "example.com" || !*asJSON || !*quiet {
		t.Errorf("pos %v json %v quiet %v err %v", pos, *asJSON, *quiet, err)
	}
}

func TestScanRefusesPrivateTargetsAsUsage(t *testing.T) {
	h := newHarness(t)
	// no browser is needed to be told no
	for _, in := range []string{"http://127.0.0.1/", "http://localhost/", "http://[::1]/", "http://169.254.169.254/latest/meta-data/", "http://10.0.0.5/", "http://2130706433/", "intranet"} {
		if code := h.run("scan", in); code != exitUsage || !strings.Contains(h.err.String(), "--allow-private") {
			t.Errorf("%s: code %d, stderr %q", in, code, h.err.String())
		}
	}
	for _, in := range []string{"ftp://example.com/", "file:///etc/passwd", "https://example.com:8443/", "javascript:alert(1)"} {
		if code := h.run("scan", in); code != exitUsage {
			t.Errorf("%s: code %d", in, code)
		}
	}
	if code := h.run("check", "http://127.0.0.1/"); code != exitUsage || !strings.Contains(h.err.String(), "--allow-private") {
		t.Errorf("check: code %d, stderr %q", code, h.err.String())
	}
}

func TestAllowPrivatePrintsAWarning(t *testing.T) {
	h := newHarness(t)
	fakeChrome(t, h)
	// the stand-in browser cannot be driven, so the scan fails after the warning
	if code := h.run("scan", "--allow-private", "http://127.0.0.1:9/"); code != exitFailed {
		t.Errorf("code %d, stderr %q", code, h.err.String())
	}
	if !strings.Contains(h.err.String(), "warning: --allow-private") || !strings.Contains(h.err.String(), "Scan only sites you own") {
		t.Errorf("stderr = %q", h.err.String())
	}
	h.run("check", "--allow-private", "http://127.0.0.1:9/")
	if !strings.Contains(h.err.String(), "warning: --allow-private") {
		t.Errorf("check stderr = %q", h.err.String())
	}
	h.run("scan", "http://127.0.0.1:9/")
	if strings.Contains(h.err.String(), "warning: --allow-private") {
		t.Error("the warning was printed without the flag")
	}
}

func TestScanWithoutABrowser(t *testing.T) {
	h := newHarness(t)
	h.env["NOSY_CHROME"] = filepath.Join(t.TempDir(), "no-such-chrome")
	if code := h.run("scan", "example.com"); code != exitFailed {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(h.err.String(), "no Chrome or Chromium found") || !strings.Contains(h.err.String(), "nosy browser install") {
		t.Errorf("stderr = %q", h.err.String())
	}
	if h.out.Len() != 0 {
		t.Errorf("stdout = %q", h.out.String())
	}
}

func TestDoctorReportsAMissingBrowser(t *testing.T) {
	h := newHarness(t)
	h.env["NOSY_CHROME"] = filepath.Join(t.TempDir(), "no-such-chrome")
	if code := h.run("doctor"); code != exitFailed {
		t.Errorf("code %d", code)
	}
	out := h.out.String()
	for _, want := range []string{"Nosy", "System", runtime.GOOS + "/" + runtime.GOARCH, "Config", "Pinned", "Browser", "not found", "fix:", "NOSY_CHROME", "nosy browser install", "Rules", "none yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output lacks %q:\n%s", want, out)
		}
	}
	if code := h.run("doctor", "extra"); code != exitUsage {
		t.Errorf("doctor with an argument: code %d", code)
	}
}

func TestConfig(t *testing.T) {
	h := newHarness(t)
	if code := h.run("config"); code != exitUsage || !strings.Contains(h.err.String(), "set <key> <value>") {
		t.Errorf("config alone: code %d", code)
	}
	if code := h.run("config", "-h"); code != exitOK || !strings.Contains(h.out.String(), "duration") {
		t.Errorf("config -h: code %d", code)
	}
	if code := h.run("config", "list"); code != exitOK {
		t.Fatalf("list: code %d", code)
	}
	for _, want := range []string{"chrome", "device    desktop (default)", "duration  20 (default)", "gpc       false (default)"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("list lacks %q:\n%s", want, h.out.String())
		}
	}
	if strings.Contains(h.out.String(), "share") {
		t.Error("a sharing setting exists before v0.2.0")
	}
	path := func() string {
		h.run("config", "path")
		return strings.TrimSpace(h.out.String())
	}()
	if _, err := os.Stat(path); err == nil {
		t.Error("the config file exists before anything was set")
	}

	for _, kv := range [][2]string{{"device", "mobile"}, {"duration", "45"}, {"gpc", "true"}, {"chrome", "/opt/chrome/chrome"}} {
		if code := h.run("config", "set", kv[0], kv[1]); code != exitOK || h.out.String() != kv[0]+" = "+kv[1]+"\n" {
			t.Errorf("set %v: code %d, output %q, stderr %q", kv, code, h.out.String(), h.err.String())
		}
		if code := h.run("config", "get", kv[0]); code != exitOK || h.out.String() != kv[1]+"\n" {
			t.Errorf("get %s: code %d, output %q", kv[0], code, h.out.String())
		}
	}
	if fi, err := os.Stat(path); err != nil {
		t.Fatalf("config file: %v", err)
	} else if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("config file mode %v, want 0600", fi.Mode().Perm())
	}
	h.run("config", "list")
	if strings.Contains(h.out.String(), "(default)") {
		t.Errorf("list still shows defaults after everything was set:\n%s", h.out.String())
	}
	// "20s" and "1m" are durations too, and are stored as seconds
	if code := h.run("config", "set", "duration", "1m"); code != exitOK || h.out.String() != "duration = 60\n" {
		t.Errorf("set 1m: code %d, output %q", code, h.out.String())
	}
	if code := h.run("config", "set", "chrome", ""); code != exitOK {
		t.Errorf("clearing chrome: code %d", code)
	}
	h.run("config", "get", "chrome")
	if h.out.String() != "\n" {
		t.Errorf("chrome after clearing = %q", h.out.String())
	}

	for name, args := range map[string][]string{
		"unknown key":     {"config", "get", "share"},
		"set unknown":     {"config", "set", "share", "on"},
		"bad device":      {"config", "set", "device", "tablet"},
		"bad duration":    {"config", "set", "duration", "2"},
		"bad gpc":         {"config", "set", "gpc", "maybe"},
		"missing value":   {"config", "set", "device"},
		"get nothing":     {"config", "get"},
		"extra for list":  {"config", "list", "x"},
		"extra for path":  {"config", "path", "x"},
		"unknown command": {"config", "unset", "device"},
	} {
		if code := h.run(args...); code != exitUsage {
			t.Errorf("%s: code %d, stderr %q", name, code, h.err.String())
		}
	}
	// a rejected value changes nothing
	h.run("config", "get", "device")
	if h.out.String() != "mobile\n" {
		t.Errorf("device after a rejected value = %q", h.out.String())
	}
}

func TestCorruptConfigIsAFailure(t *testing.T) {
	h := newHarness(t)
	h.run("config", "set", "device", "mobile")
	path, _ := h.app.configPath()
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeChrome(t, h)
	for _, args := range [][]string{{"config", "list"}, {"scan", "example.com"}} {
		if code := h.run(args...); code != exitFailed || !strings.Contains(h.err.String(), "not valid") {
			t.Errorf("%v: code %d, stderr %q", args, code, h.err.String())
		}
	}
	if code := h.run("doctor"); code != exitFailed || !strings.Contains(h.out.String(), "unreadable") {
		t.Errorf("doctor: code %d, output %q", code, h.out.String())
	}
}

func TestScanSettingsFlagsBeatConfig(t *testing.T) {
	yes := true
	cur := settings{Duration: 45, Device: "mobile", GPC: &yes}
	ss, err := resolveScan(cur, map[string]bool{}, "", "", false)
	if err != nil || ss.duration != 45*time.Second || !ss.mobile || !ss.gpc {
		t.Errorf("config only: %+v, %v", ss, err)
	}
	ss, err = resolveScan(cur, map[string]bool{"duration": true, "device": true, "gpc": true}, "30s", "desktop", false)
	if err != nil || ss.duration != 30*time.Second || ss.mobile || ss.gpc {
		t.Errorf("flags over config: %+v, %v", ss, err)
	}
	ss, err = resolveScan(settings{}, map[string]bool{}, "", "", false)
	if err != nil || ss.duration != 20*time.Second || ss.mobile || ss.gpc {
		t.Errorf("defaults: %+v, %v", ss, err)
	}
}

func TestParseDuration(t *testing.T) {
	good := map[string]time.Duration{"10": 10 * time.Second, "20s": 20 * time.Second, "1m": time.Minute, "120": 2 * time.Minute, " 30 ": 30 * time.Second}
	for in, want := range good {
		if got, err := parseDuration(in); err != nil || got != want {
			t.Errorf("%q = %v, %v, want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "9", "121", "2m1s", "abc", "-30", "1.5"} {
		if _, err := parseDuration(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestStatusLineLeavesNothingBehind(t *testing.T) {
	var out strings.Builder
	st := startStatus(&out)
	st.set("Opening the page")
	time.Sleep(200 * time.Millisecond)
	st.set("Scrolling like a reader")
	time.Sleep(150 * time.Millisecond)
	st.stop()
	got := out.String()
	if !strings.Contains(got, "Opening the page") || !strings.Contains(got, "Scrolling like a reader") {
		t.Errorf("steps missing from %q", got)
	}
	if strings.Contains(got, "...") || strings.Contains(got, "\u2026") || strings.Contains(got, "\n") {
		t.Errorf("status line has an ellipsis or a newline: %q", got)
	}
	if !strings.HasSuffix(got, "\r") {
		t.Errorf("the line was not cleared: %q", got)
	}
}

func sampleResult() *scan.Result {
	res := score.Compute(score.Input{TrackingCompanies: 3, AdTech: true})
	return &scan.Result{
		Schema: scan.Schema, Status: scan.StatusDone, Site: "example.com", FinalURL: "https://www.example.com/",
		Score: &res, Finding: "example.com contacted 3 tracking companies before you clicked anything",
		Weight: scan.Weight{Requests: 20, ThirdRequests: 9},
		Companies: []detect.Company{
			{Name: "AdNet", Tracking: true, Classes: []string{"advertising"}, Requests: 4},
			{Name: "Stats", Tracking: true, Classes: []string{"analytics"}, Requests: 1},
			{Name: "CDN Co", Requests: 3},
		},
		Cookies: []scan.Cookie{{Name: "uid", Identifier: true}, {Name: "theme"}},
		Findings: []scan.Finding{
			{ID: "third-party-companies", Label: scan.Observed, Text: "The page contacted 3 companies other than its own before any click."},
			{ID: "tracking-companies", Label: scan.Classified, Text: "2 of them are classified as tracking."},
			{ID: "blocked", Label: scan.Inferred, Text: "Nothing was scored."},
		},
		Notes: []string{"no tracker rules are installed yet"},
		Conditions: scan.Conditions{Date: "2026-10-17T18:00:00Z", Device: "desktop", Viewport: "1366x768", Browser: "Chrome/155",
			DurationMS: 10400, DurationLimitSeconds: 20, Nosy: "dev", Rules: "none"},
	}
}

func TestSummaryShowsTheScoreCountsFindingsAndConditions(t *testing.T) {
	var out strings.Builder
	printSummary(&out, sampleResult())
	got := out.String()
	for _, want := range []string{
		"example.com", "https://www.example.com/", "Score", "/ 100", "formula v1",
		"Requests    20, 9 to other companies", "Companies   3, 2 classified as tracking", "Cookies     2, 1 look like identifiers",
		"Top companies", "AdNet", "advertising", "4 requests", "Stats", "1 request\n",
		"[observed]", "[classified]", "[inferred]", "Notes", "Conditions", "2026-10-17T18:00:00Z", "desktop 1366x768", "10.4 s of 20 s", "rules none",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "...") || strings.Contains(got, "\u2026") {
		t.Errorf("summary has an ellipsis:\n%s", got)
	}

	blocked := sampleResult()
	blocked.Score, blocked.Status, blocked.Reason = nil, scan.StatusBlocked, "the site showed a bot check"
	out.Reset()
	printSummary(&out, blocked)
	if !strings.Contains(out.String(), "Status      blocked: the site showed a bot check") || strings.Contains(out.String(), "Score") {
		t.Errorf("blocked summary:\n%s", out.String())
	}

	var one strings.Builder
	printOneLine(&one, sampleResult())
	if one.String() != fmt.Sprintf("example.com  %d/100  %s\n", sampleResult().Score.Total, sampleResult().Score.Label) {
		t.Errorf("one line = %q", one.String())
	}
	one.Reset()
	printOneLine(&one, blocked)
	if one.String() != "example.com  blocked: the site showed a bot check\n" {
		t.Errorf("one line = %q", one.String())
	}
}

func TestCheckOutput(t *testing.T) {
	r := &check.Result{
		URL: "https://example.com/", Date: "2026-10-17T18:00:00Z", Nosy: "dev",
		Checks: []check.Check{
			{ID: "address", Label: check.Observed, Text: "example.com leads to 93.184.216.34.", OK: true},
			{ID: "https", Label: check.Observed, Text: "The home page ends on plain HTTP.", OK: false},
		},
		Cookies: []check.Cookie{{Name: "uid", Domain: "example.com", Days: 365, Identifier: true}, {Name: "theme", Domain: "example.com", Session: true}},
	}
	var out strings.Builder
	printChecks(&out, r)
	for _, want := range []string{"Checks for https://example.com/", "ok  [observed]", "fix [observed]", "365 days", "looks like an identifier", "session", "later version"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("check output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestResultJSONIsStable(t *testing.T) {
	b, err := json.Marshal(sampleResult())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema", "status", "site", "conditions", "score", "findings", "companies", "requests", "cookies", "fingerprinting", "session_recording", "typed_text", "consent", "weight", "page"} {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON lacks %q", key)
		}
	}
	for _, key := range []string{"date", "country", "browser", "device", "viewport", "consent_mode", "gpc", "duration_limit_seconds", "duration_ms", "nosy", "rules", "settings"} {
		if _, ok := m["conditions"].(map[string]any)[key]; !ok {
			t.Errorf("conditions lack %q", key)
		}
	}
}

func TestDoctorNamesMissingLibrariesAndTheCommandToInstallThem(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("shared libraries are checked on Linux")
	}
	h := newHarness(t)
	fakeChrome(t, h)
	h.app.missingLibs = func(context.Context, string) ([]string, error) {
		return []string{"libgbm.so.1", "libnss3.so", "libmystery.so.9"}, nil
	}
	for family, want := range map[string]string{
		"ID=ubuntu\nID_LIKE=debian\n": "sudo apt-get install -y libgbm1 libnss3",
		"ID=fedora\n":                 "sudo dnf install -y mesa-libgbm nss",
		"ID=arch\n":                   "sudo pacman -S --needed mesa nss",
		"ID=alpine\n":                 "package manager",
	} {
		h.app.osRelease = func() string { return family }
		if code := h.run("doctor"); code != exitFailed {
			t.Errorf("%q: code %d", family, code)
		}
		out := h.out.String()
		if !strings.Contains(out, "Libraries missing: libgbm.so.1, libnss3.so, libmystery.so.9") || !strings.Contains(out, "fix: ") || !strings.Contains(out, want) {
			t.Errorf("%q: doctor output lacks %q:\n%s", family, want, out)
		}
		if strings.Contains(out, "Sandbox") {
			t.Errorf("doctor tried to start a browser that lacks libraries:\n%s", out)
		}
	}
}

func TestDoctorSaysWhenTheBrowserDoesNotStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for the browser")
	}
	h := newHarness(t)
	script := filepath.Join(t.TempDir(), "chrome")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.env["NOSY_CHROME"] = script
	if code := h.run("doctor"); code != exitFailed || !strings.Contains(h.out.String(), "does not start") || !strings.Contains(h.out.String(), "fix: run it by hand") {
		t.Errorf("code %d\n%s", code, h.out.String())
	}
}
