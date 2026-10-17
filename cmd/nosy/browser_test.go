//go:build nosytest

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/muxover/nosy/internal/check"
	"github.com/muxover/nosy/internal/scan"
	"github.com/muxover/nosy/internal/testsite"
)

// These tests run the commands against the planted pages with a real Chromium.
// They need -tags nosytest and NOSY_BROWSER_TESTS=1, like internal/integration.

var (
	enabled = os.Getenv("NOSY_BROWSER_TESTS") == "1"
	fixture *testsite.Site
)

func init() {
	if !enabled {
		return
	}
	setupBrowserTests = func() func() {
		var err error
		if fixture, err = testsite.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "setup:", err)
			os.Exit(1)
		}
		testsite.Register(fixture.HTTP, fixture.TLS, fixture.CAPin)
		return fixture.Close
	}
}

func need(t *testing.T) {
	t.Helper()
	if !enabled {
		t.Skip("set NOSY_BROWSER_TESTS=1 to run the command tests against a real Chromium")
	}
}

// browserHarness is a harness that finds the same browser the test process does.
func browserHarness(t *testing.T) *harness {
	t.Helper()
	need(t)
	h := newHarness(t)
	h.env["NOSY_CHROME"] = os.Getenv("NOSY_CHROME")
	return h
}

func scanJSON(t *testing.T, h *harness, args ...string) (*scan.Result, int) {
	t.Helper()
	code := h.run(append([]string{"scan", "--json"}, args...)...)
	var r scan.Result
	if err := json.Unmarshal(h.out.Bytes(), &r); err != nil {
		t.Fatalf("stdout is not a result (code %d): %v\nstdout: %s\nstderr: %s", code, err, h.out.String(), h.err.String())
	}
	return &r, code
}

func TestScanJSONOnAPlantedPage(t *testing.T) {
	t.Parallel()
	h := browserHarness(t)
	r, code := scanJSON(t, h, "http://canvas.test/")
	if code != exitOK {
		t.Fatalf("code %d, stderr %s", code, h.err.String())
	}
	if h.err.Len() != 0 {
		t.Errorf("stderr has output when it is not a terminal: %q", h.err.String())
	}
	if r.Schema != scan.Schema || r.Status != scan.StatusDone || r.Site != "canvas.test" || r.RequestedURL != "http://canvas.test/" {
		t.Errorf("result head = %s %s %s %s", r.Schema, r.Status, r.Site, r.RequestedURL)
	}
	if r.Score == nil || r.Score.Total != 5 || r.Score.Parts.Fingerprinting != 5 || r.Score.Formula != "v1" {
		t.Errorf("score = %+v", r.Score)
	}
	if len(r.Fingerprinting) != 1 || r.Fingerprinting[0].Name != "canvas" || len(r.Fingerprinting[0].Scripts) != 1 || r.Fingerprinting[0].Scripts[0] != "http://fpvendor.test/canvas.js" {
		t.Errorf("fingerprinting = %+v", r.Fingerprinting)
	}
	var found *scan.Finding
	for i := range r.Findings {
		if r.Findings[i].ID == "fingerprinting-canvas" {
			found = &r.Findings[i]
		}
	}
	if found == nil || found.Label != scan.Classified || len(found.Evidence) != 1 {
		t.Errorf("findings = %+v", r.Findings)
	}
	c := r.Conditions
	if c.Device != "desktop" || c.Viewport != "1366x768" || c.GPC || c.DurationLimitSeconds != 20 || c.DurationMS <= 0 ||
		c.Nosy != nosyVersion() || c.Rules != "none" || c.Country != "unknown" || !strings.HasPrefix(c.Browser, "Chrome/") ||
		c.ConsentMode != "not touched" || len(c.Settings) == 0 {
		t.Errorf("conditions = %+v", c)
	}
	if _, err := time.Parse(time.RFC3339, c.Date); err != nil {
		t.Errorf("date %q: %v", c.Date, err)
	}
	if len(r.Requests) < 3 || r.Weight.Requests < 3 || r.Weight.ThirdRequests < 1 {
		t.Errorf("requests %d weight %+v", len(r.Requests), r.Weight)
	}
	if !hasNote(r, "no tracker rules are installed yet") {
		t.Errorf("notes = %v, want one saying no rules are installed", r.Notes)
	}
	// with no rules, a company is listed but never called tracking
	for _, co := range r.Companies {
		if co.Tracking {
			t.Errorf("%s classified as tracking with no rules", co.Name)
		}
	}
}

func hasNote(r *scan.Result, sub string) bool {
	for _, n := range r.Notes {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}

func TestScanFindsTypedTextLeakJSON(t *testing.T) {
	t.Parallel()
	h := browserHarness(t)
	r, code := scanJSON(t, h, "http://keyinput.test/")
	if code != exitOK {
		t.Fatalf("code %d, stderr %s", code, h.err.String())
	}
	if r.Score == nil || r.Score.Parts.KeyCapture != 10 || r.TypedText.FieldsTested < 2 || len(r.TypedText.Hits) == 0 {
		t.Fatalf("score %+v typed text %+v", r.Score, r.TypedText)
	}
	for _, hit := range r.TypedText.Hits {
		if !hit.ThirdParty || !strings.Contains(hit.URL, "collector.test") {
			t.Errorf("hit = %+v", hit)
		}
	}
	var f *scan.Finding
	for i := range r.Findings {
		if r.Findings[i].ID == "typed-text" {
			f = &r.Findings[i]
		}
	}
	if f == nil || f.Label != scan.Observed || !strings.Contains(f.Text, "a third party") {
		t.Errorf("findings = %+v", r.Findings)
	}
	// the random string Nosy types is not in the output
	if strings.Contains(h.out.String(), `"nosy`+"probe") {
		t.Error("the test string is in the output")
	}
}

func TestScanSummaryAndQuietOnAPlantedPage(t *testing.T) {
	t.Parallel()
	h := browserHarness(t)
	if code := h.run("scan", "http://canvas.test/"); code != exitOK {
		t.Fatalf("code %d, stderr %s", code, h.err.String())
	}
	out := h.out.String()
	for _, want := range []string{"canvas.test", "Score       5 / 100", "Not nosy", "Findings", "[classified]", "canvas", "Conditions", "desktop 1366x768", "rules none"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
	if h.err.Len() != 0 {
		t.Errorf("stderr = %q", h.err.String())
	}
	if code := h.run("scan", "--quiet", "http://canvas.test/"); code != exitOK || h.out.String() != "canvas.test  5/100  Not nosy\n" || h.err.Len() != 0 {
		t.Errorf("quiet: code %d, stdout %q, stderr %q", code, h.out.String(), h.err.String())
	}
}

func TestScanProgressOnlyOnATerminal(t *testing.T) {
	t.Parallel()
	h := browserHarness(t)
	h.app.progress = true
	if code := h.run("scan", "--json", "http://control.test/"); code != exitOK {
		t.Fatalf("code %d", code)
	}
	for _, want := range []string{"Opening the page", "Waiting for the page to settle", "Building the result"} {
		if !strings.Contains(h.err.String(), want) {
			t.Errorf("progress lacks %q in %q", want, h.err.String())
		}
	}
	if strings.Contains(h.err.String(), "...") || strings.Contains(h.err.String(), "\u2026") {
		t.Errorf("progress has an ellipsis: %q", h.err.String())
	}
	if !json.Valid(h.out.Bytes()) {
		t.Error("progress leaked into stdout")
	}
	// quiet turns it off even on a terminal
	if code := h.run("scan", "--quiet", "http://control.test/"); code != exitOK || h.err.Len() != 0 {
		t.Errorf("quiet: code %d, stderr %q", code, h.err.String())
	}
}

func TestScanExitCodesForBlockedAndFailedPages(t *testing.T) {
	t.Parallel()
	h := browserHarness(t)
	r, code := scanJSON(t, h, "http://challenge.test/")
	if code != exitFailed || r.Status != scan.StatusBlocked || r.Score != nil || !strings.Contains(r.Reason, "bot check") {
		t.Errorf("blocked: code %d, %s %q score %v", code, r.Status, r.Reason, r.Score)
	}
	if len(r.Findings) != 1 || r.Findings[0].Label != scan.Inferred {
		t.Errorf("blocked findings = %+v", r.Findings)
	}
	r, code = scanJSON(t, h, "http://down.test/")
	if code != exitFailed || r.Status != scan.StatusFailed || r.Score != nil {
		t.Errorf("failed: code %d, %s %q", code, r.Status, r.Reason)
	}
	if code := h.run("scan", "http://challenge.test/"); code != exitFailed || !strings.Contains(h.out.String(), "Status      blocked") {
		t.Errorf("blocked summary: code %d\n%s", code, h.out.String())
	}
}

func TestScanDurationLimit(t *testing.T) {
	t.Parallel()
	h := browserHarness(t)
	start := time.Now()
	r, code := scanJSON(t, h, "--duration", "10s", "http://hang.test/")
	if code != exitFailed || r.Status != scan.StatusFailed || !strings.Contains(r.Reason, "within 10 seconds") {
		t.Errorf("code %d, %s %q", code, r.Status, r.Reason)
	}
	if d := time.Since(start); d > 15*time.Second {
		t.Errorf("a 10 second limit took %s", d)
	}
	if r.Conditions.DurationLimitSeconds != 10 {
		t.Errorf("conditions = %+v", r.Conditions)
	}
}

func TestScanDeviceAndGPC(t *testing.T) {
	t.Parallel()
	h := browserHarness(t)
	last := func(path string) testsite.EnvHit {
		hits := fixture.EnvHits()
		for i := len(hits) - 1; i >= 0; i-- {
			if hits[i].Path == path {
				return hits[i]
			}
		}
		t.Fatalf("env.test never saw %s", path)
		return testsite.EnvHit{}
	}

	r, code := scanJSON(t, h, "--device", "mobile", "--gpc", "https://env.test/")
	if code != exitOK {
		t.Fatalf("code %d, stderr %s", code, h.err.String())
	}
	if r.Conditions.Device != "mobile" || r.Conditions.Viewport != "390x844" || !r.Conditions.GPC || !strings.Contains(r.Conditions.UserAgent, "Mobile") {
		t.Errorf("conditions = %+v", r.Conditions)
	}
	page, report := last("/"), last("/report")
	if page.GPCHeader != "1" || !strings.Contains(page.UserAgent, "Mobile") {
		t.Errorf("the server saw %+v", page)
	}
	for _, want := range []string{"width=390", "touch=true", "gpc=true", "mobile=true", "ratio=3"} {
		if !strings.Contains(report.Query, want) {
			t.Errorf("the page's own script saw %q, lacks %q", report.Query, want)
		}
	}

	r, code = scanJSON(t, h, "https://env.test/")
	if code != exitOK || r.Conditions.Device != "desktop" || r.Conditions.GPC {
		t.Fatalf("default scan: code %d, conditions %+v", code, r.Conditions)
	}
	page, report = last("/"), last("/report")
	if page.GPCHeader != "" || strings.Contains(page.UserAgent, "Mobile") {
		t.Errorf("the server saw %+v", page)
	}
	for _, want := range []string{"width=1366", "touch=false", "gpc=undefined", "mobile=false", "ratio=1"} {
		if !strings.Contains(report.Query, want) {
			t.Errorf("the page's own script saw %q, lacks %q", report.Query, want)
		}
	}

	// the config sets the defaults and a flag beats it
	h.run("config", "set", "device", "mobile")
	h.run("config", "set", "gpc", "true")
	r, _ = scanJSON(t, h, "https://env.test/")
	if r.Conditions.Device != "mobile" || !r.Conditions.GPC {
		t.Errorf("config defaults not used: %+v", r.Conditions)
	}
	r, _ = scanJSON(t, h, "--device", "desktop", "--gpc=false", "https://env.test/")
	if r.Conditions.Device != "desktop" || r.Conditions.GPC {
		t.Errorf("flags did not beat the config: %+v", r.Conditions)
	}
}

func TestCheckOnAPlantedPage(t *testing.T) {
	t.Parallel()
	need(t)
	h := newHarness(t)
	code := h.run("check", "--json", "http://cookies.test/")
	if code != exitOK {
		t.Fatalf("code %d, stderr %s", code, h.err.String())
	}
	var r check.Result
	if err := json.Unmarshal(h.out.Bytes(), &r); err != nil {
		t.Fatalf("%v\n%s", err, h.out.String())
	}
	ids := map[string]bool{}
	for _, c := range r.Cookies {
		if c.Identifier {
			ids[c.Name] = true
		}
	}
	for _, name := range []string{"uid", "_ga", "vid"} {
		if !ids[name] {
			t.Errorf("%s not an identifier in %+v", name, r.Cookies)
		}
	}
	if r.Status != 200 || r.Schema != check.Schema || len(r.Checks) < 4 {
		t.Errorf("result = %+v", r)
	}
	if code := h.run("check", "http://cookies.test/"); code != exitOK || !strings.Contains(h.out.String(), "looks like an identifier") {
		t.Errorf("text: code %d\n%s", code, h.out.String())
	}
	if code := h.run("check", "http://down.test/"); code != exitFailed || !strings.Contains(h.err.String(), "could not fetch") {
		t.Errorf("unreachable: code %d, stderr %q", code, h.err.String())
	}
}

func TestDoctorFindsTheBrowser(t *testing.T) {
	h := browserHarness(t)
	code := h.run("doctor")
	out := h.out.String()
	for _, want := range []string{"Chrome", os.Getenv("NOSY_CHROME"), "Chrome/", "Rules     none yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor lacks %q:\n%s", want, out)
		}
	}
	// the sandbox depends on the machine: on a runner that forbids it, doctor says so and fails
	switch {
	case strings.Contains(out, "Sandbox   available"):
		if code != exitOK {
			t.Errorf("code %d with everything in place:\n%s", code, out)
		}
	case strings.Contains(out, "Sandbox   not available"):
		if code != exitFailed || !strings.Contains(out, "fix:") {
			t.Errorf("code %d without a sandbox:\n%s", code, out)
		}
	default:
		t.Errorf("doctor says nothing about the sandbox:\n%s", out)
	}
}

// subprocess runs the test binary as the nosy command, pointed at the planted pages.
func subprocess(t *testing.T, tmp string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(),
		"NOSY_TEST_RUN_MAIN=1",
		"NOSY_TEST_HTTP="+fixture.HTTP, "NOSY_TEST_TLS="+fixture.TLS, "NOSY_TEST_SPKI="+fixture.CAPin,
		"TMPDIR="+tmp, "TEMP="+tmp, "TMP="+tmp,
		"XDG_CONFIG_HOME="+filepath.Join(tmp, "config"), "HOME="+tmp, "APPDATA="+filepath.Join(tmp, "config"),
	)
	return cmd
}

func TestCommandLineEndToEnd(t *testing.T) {
	t.Parallel()
	need(t)
	var stdout, stderr bytes.Buffer
	cmd := subprocess(t, t.TempDir(), "scan", "--json", "--quiet", "http://canvas.test/")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v\nstderr: %s", err, stderr.String())
	}
	var r scan.Result
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil || r.Status != scan.StatusDone || r.Score == nil || r.Score.Total != 5 {
		t.Errorf("stdout is not the expected result: %v\n%s", err, stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	cmd = subprocess(t, t.TempDir(), "scan", "--bogus", "x.example")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != exitUsage {
		t.Errorf("a usage error exited with %v", err)
	}
}

// Ctrl+C stops the browser, removes the temp profile and exits 130.
func TestInterruptStopsTheBrowserAndExits130(t *testing.T) {
	t.Parallel()
	need(t)
	if runtime.GOOS == "windows" {
		t.Skip("a console interrupt cannot be sent to a child process from a test on Windows")
	}
	tmp := t.TempDir()
	var stdout, stderr bytes.Buffer
	cmd := subprocess(t, tmp, "scan", "--json", "http://interrupt.hang.test/")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	// wait until the browser is on the page, which never finishes loading
	deadline := time.Now().Add(40 * time.Second)
	for len(fixture.Hits("interrupt.hang.test")) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the browser never reached the page; stderr: %s", stderr.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	profiles, _ := filepath.Glob(filepath.Join(tmp, "nosy-profile-*"))
	if len(profiles) == 0 {
		t.Fatalf("no temp profile while the scan runs (looked in %s)", tmp)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		ee, ok := err.(*exec.ExitError)
		if !ok || ee.ExitCode() != exitInterrupted {
			t.Fatalf("exited with %v, want 130; stderr: %s", err, stderr.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("still running 20 s after the interrupt; stderr: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "interrupted") {
		t.Errorf("stderr = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q after an interrupt", stdout.String())
	}
	if left, _ := filepath.Glob(filepath.Join(tmp, "nosy-profile-*")); len(left) != 0 {
		t.Errorf("temp profiles left behind: %v", left)
	}
	if runtime.GOOS == "linux" {
		time.Sleep(300 * time.Millisecond)
		if left := processesUsing(tmp); len(left) != 0 {
			t.Errorf("browser processes left behind: %v", left)
		}
	}
}

// processesUsing lists processes whose command line mentions dir.
func processesUsing(dir string) []string {
	var out []string
	files, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), dir) {
			out = append(out, strings.ReplaceAll(strings.TrimRight(string(b), "\x00"), "\x00", " "))
		}
	}
	return out
}
