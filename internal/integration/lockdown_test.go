//go:build nosytest

package integration

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/muxover/nosy/internal/browser"
	"github.com/muxover/nosy/internal/capture"
	"github.com/muxover/nosy/internal/guard"
	"github.com/muxover/nosy/internal/scan"
)

// visitCapture visits u in a browser started for the visit and returns what it recorded.
func visitCapture(t *testing.T, u string, o browser.Options) (*capture.Capture, error) {
	t.Helper()
	need(t)
	gate <- struct{}{}
	defer func() { <-gate }()
	proxy, err := guard.StartProxy(&guard.Dialer{}, guard.MaxBytes, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	target, _ := url.Parse(u)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	o.Proxy = proxy
	o.Verbose = t.Logf
	return browser.Visit(ctx, target, o)
}

func finding(r *scan.Result, id string) *scan.Finding {
	for i := range r.Findings {
		if r.Findings[i].ID == id {
			return &r.Findings[i]
		}
	}
	return nil
}

func TestPopUpsAreClosedAndReported(t *testing.T) {
	t.Parallel()
	before := len(fixture.Requests("popped.test"))
	r := scanTest(t, "http://popup.test/")
	if len(r.Refused.Popups) == 0 && r.Refused.PopupsClosed == 0 {
		t.Fatalf("no pop-up was recorded: %+v", r.Refused)
	}
	urls := ""
	for _, p := range r.Refused.Popups {
		urls += p.URL + " "
	}
	if !strings.Contains(urls, "http://popped.test/load") {
		t.Errorf("pop-ups = %+v, want the one asked for when the page loaded", r.Refused.Popups)
	}
	if f := finding(r, "popups"); f == nil || f.Label != scan.Observed {
		t.Errorf("popups finding = %+v", f)
	}
	if after := len(fixture.Requests("popped.test")); after != before {
		t.Errorf("the pop-up's address was requested %d time(s): a window was loaded before it was closed", after-before)
	}
}

func TestDownloadsAreRefused(t *testing.T) {
	t.Parallel()
	home, _ := os.UserHomeDir()
	c, err := visitCapture(t, "http://download.test/", browser.Options{Token: "nosydl"})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Downloads) == 0 || c.Downloads[0].Filename != "planted.bin" {
		t.Fatalf("downloads = %+v, want the planted file", c.Downloads)
	}
	for _, d := range c.Downloads {
		if d.State == "completed" {
			t.Errorf("a download completed: %+v", d)
		}
	}
	for _, dir := range []string{filepath.Join(home, "Downloads"), os.TempDir(), "."} {
		if _, err := os.Stat(filepath.Join(dir, "planted.bin")); err == nil {
			t.Errorf("planted.bin was written to %s", dir)
		}
	}
	r := scanTest(t, "http://download.test/")
	if f := finding(r, "downloads"); f == nil || len(f.Evidence) == 0 || f.Evidence[0] != "planted.bin" {
		t.Errorf("downloads finding = %+v", f)
	}
}

func TestEveryPermissionRequestIsDenied(t *testing.T) {
	t.Parallel()
	scanTest(t, "https://perms.test/")
	var query url.Values
	for _, q := range fixture.Requests("perms.test") {
		if q.Path == "/report" {
			query, _ = url.ParseQuery(q.Query)
		}
	}
	if query == nil {
		t.Fatal("the page never reported what it was given")
	}
	denied := map[string][]string{
		"notification": {"denied"},
		"geolocation":  {"error1"},
		"camera":       {"NotAllowedError", "NotFoundError"},
		"microphone":   {"NotAllowedError", "NotFoundError"},
		"clipboard":    {"NotAllowedError"},
		"midi":         {"NotAllowedError", "SecurityError"},
	}
	for k, ok := range denied {
		got := query.Get(k)
		if !has(ok, got) {
			t.Errorf("%s = %q, want one of %v", k, got, ok)
		}
	}
	// the page's own question to the browser: not "prompt", which would mean a visitor was asked
	for _, name := range []string{"geolocation", "notifications", "camera", "microphone", "midi", "clipboard-read"} {
		if got := query.Get("query_" + name); got != "denied" {
			t.Errorf("permission %s is %q, want denied", name, got)
		}
	}
}

func TestLocalAddressesAreNeverOpened(t *testing.T) {
	t.Parallel()
	secret := fmt.Sprintf("NOSY-LOCAL-SECRET-%d", time.Now().UnixNano())
	path := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	local := (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(path), "/")}).String()
	fixture.SetLocalFile(local)

	c, err := visitCapture(t, "http://localnav.test/", browser.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c.Page.Text, secret) || strings.Contains(c.Page.HTML, secret) {
		t.Errorf("the page shows what is in a local file")
	}
	if !strings.HasPrefix(c.Page.FinalURL, "http://localnav.test") {
		t.Errorf("the visit ended at %s: the page was taken away from its own address", c.Page.FinalURL)
	}
	if len(c.LocalNav) == 0 {
		t.Errorf("the attempts to open local addresses were not recorded")
	}
	r := scanTest(t, "http://localnav.test/")
	if f := finding(r, "local-addresses"); f == nil {
		t.Errorf("no finding for the local addresses: %+v", r.Findings)
	}

	redirected, err := visitCapture(t, "http://localredir.test/", browser.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(redirected.Page.Text, secret) || strings.HasPrefix(redirected.Page.FinalURL, "file:") {
		t.Errorf("a redirect to a local file was followed: %q", redirected.Page.FinalURL)
	}
}

func TestHSTSIsNotRemembered(t *testing.T) {
	t.Parallel()
	// the page sets HSTS and then goes to its own plain address: inside a scan the
	// browser has to remember, or this test could not tell
	if _, err := visitCapture(t, "https://hsts.test/", browser.Options{}); err != nil {
		t.Fatal(err)
	}
	var second []bool
	for _, q := range fixture.Requests("hsts.test") {
		if q.Path == "/second" {
			second = append(second, q.TLS)
		}
	}
	if len(second) == 0 || !second[len(second)-1] {
		t.Skipf("the browser did not apply HSTS inside one scan (%v), so this cannot show it is forgotten", second)
	}
	before := len(second)
	if _, err := visitCapture(t, "http://hsts.test/second", browser.Options{}); err != nil {
		t.Fatal(err)
	}
	second = second[:0]
	for _, q := range fixture.Requests("hsts.test") {
		if q.Path == "/second" {
			second = append(second, q.TLS)
		}
	}
	if len(second) <= before {
		t.Fatalf("the second scan never reached the server")
	}
	for _, tls := range second[before:] {
		if tls {
			t.Errorf("a request for /second came over TLS: the second scan was upgraded by HSTS from the first")
		}
	}
}

// shortTempDir is a folder with a short path: the browser makes a socket inside
// its profile, and a socket address can only be so long.
func shortTempDir(t *testing.T) string {
	t.Helper()
	base := ""
	if runtime.GOOS != "windows" {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "nosy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// profilesIn lists the browser profiles in the temp directory.
func profilesIn(t *testing.T) []string {
	t.Helper()
	left, err := filepath.Glob(filepath.Join(os.TempDir(), "nosy-profile-*"))
	if err != nil {
		t.Fatal(err)
	}
	return left
}

// not parallel: it points the temp directory at a folder of its own and looks
// for what a scan leaves in it
func TestProfileIsRemovedAfterEveryEnding(t *testing.T) {
	need(t)
	tmp := shortTempDir(t)
	for _, k := range []string{"TMPDIR", "TEMP", "TMP"} {
		t.Setenv(k, tmp)
	}
	check := func(ending string) {
		t.Helper()
		if left := profilesIn(t); len(left) > 0 {
			t.Errorf("after %s the profile is still there: %v", ending, left)
		}
	}

	if _, err := visitCapture(t, "http://control.test/", browser.Options{}); err != nil {
		t.Fatal(err)
	}
	check("a normal end")

	if _, err := visitCapture(t, "http://down.test/", browser.Options{}); err != nil {
		t.Fatal(err)
	}
	check("a page that could not be loaded")

	// Ctrl+C: the scan's context ends while the page is loading
	gate <- struct{}{}
	proxy, err := guard.StartProxy(&guard.Dialer{}, guard.MaxBytes, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(3*time.Second, cancel)
	target, _ := url.Parse("http://hang.test/")
	_, _ = browser.Visit(ctx, target, browser.Options{Proxy: proxy})
	proxy.Close()
	<-gate
	check("the scan being cancelled")

	// the browser is killed under the scan
	browser.Started = func(pid int) {
		time.AfterFunc(2*time.Second, func() {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		})
	}
	t.Cleanup(func() { browser.Started = nil })
	_, _ = visitCapture(t, "http://hang.test/", browser.Options{})
	browser.Started = nil
	check("the browser being killed")
}

// not parallel, for the same reason
func TestProfilesLeftByACrashAreClearedByTheNextScan(t *testing.T) {
	need(t)
	tmp := shortTempDir(t)
	for _, k := range []string{"TMPDIR", "TEMP", "TMP"} {
		t.Setenv(k, tmp)
	}
	// a process number that is certainly free: a program that has already ended
	done := exec.Command(os.Args[0], "-test.run=^$")
	if err := done.Run(); err != nil {
		t.Fatal(err)
	}
	dead := done.Process.Pid

	plant := func(name string, owner string) string {
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(filepath.Join(dir, "Default"), 0o700); err != nil {
			t.Fatal(err)
		}
		if owner != "" {
			if err := os.WriteFile(filepath.Join(dir, ".nosy-owner"), []byte(owner+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	stale := plant("nosy-profile-stale", fmt.Sprint(dead))
	live := plant("nosy-profile-live", fmt.Sprint(os.Getpid()))
	foreign := plant("nosy-profile-foreign", "")
	other := plant("something-else", fmt.Sprint(dead))
	file := filepath.Join(tmp, "nosy-profile-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	r := scanTest(t, "http://control.test/")
	for _, n := range r.Notes {
		if strings.Contains(n, "browser profile") {
			t.Errorf("note about a profile: %s", n)
		}
	}
	if _, err := os.Stat(stale); err == nil {
		t.Errorf("the profile of a scan that crashed is still there")
	}
	for name, path := range map[string]string{"a profile in use": live, "a folder without Nosy's note": foreign, "a folder with another name": other, "a file": file} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was removed: %v", name, err)
		}
	}
}
