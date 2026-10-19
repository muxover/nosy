package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muxover/nosy/internal/chrome"
)

// pinnedServer serves a small stand-in for the Chrome for Testing zip and
// points the harness's installer at it.
func pinnedServer(t *testing.T, h *harness, corrupt bool) *int {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	hdr := &zip.FileHeader{Name: "chrome-linux64/chrome", Method: zip.Deflate}
	hdr.SetMode(0o755)
	w, _ := zw.CreateHeader(hdr)
	w.Write([]byte(strings.Repeat("#!/bin/sh\n", 5000)))
	zw.Close()
	good := buf.Bytes()
	sum := sha256.Sum256(good)
	served := append([]byte(nil), good...)
	if corrupt {
		served[len(served)/2] ^= 0xff
	}
	hits := new(int)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		w.Write(served)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	h.app.installer = func() (*chrome.Installer, error) {
		return &chrome.Installer{
			Version:     "155.0.0.1",
			Platform:    chrome.Platform{Name: "linux64", Size: int64(len(good)), SHA256: hex.EncodeToString(sum[:]), Exec: "chrome-linux64/chrome"},
			Dir:         h.cache,
			BaseURL:     srv.URL,
			AllowedHost: u.Host,
			Client:      srv.Client(),
		}, nil
	}
	return hits
}

func TestBrowserInstallPathRemove(t *testing.T) {
	h := newHarness(t)
	hits := pinnedServer(t, h, false)

	if code := h.run("browser", "path"); code != exitFailed || !strings.Contains(h.err.String(), "nosy browser install") {
		t.Errorf("path before install: code %d, stderr %q", code, h.err.String())
	}
	if code := h.run("browser", "install"); code != exitOK {
		t.Fatalf("install: code %d, stderr %q", code, h.err.String())
	}
	out := h.out.String()
	for _, want := range []string{"Installing Chrome for Testing 155.0.0.1 (linux64) into " + h.cache, "downloaded", "checking the sha-256", "unpacking", "Installed 155.0.0.1", "SHA-256 checked", filepath.Join(h.cache, "155.0.0.1")} {
		if !strings.Contains(out, want) {
			t.Errorf("install output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "...") || strings.Contains(out, "\u2026") {
		t.Errorf("install output has an ellipsis:\n%s", out)
	}
	if *hits != 1 {
		t.Errorf("%d downloads", *hits)
	}

	if code := h.run("browser", "path"); code != exitOK {
		t.Fatalf("path: code %d, stderr %q", code, h.err.String())
	}
	lines := strings.Split(strings.TrimSpace(h.out.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], h.cache) || !strings.Contains(lines[1], "Nosy's own Chrome for Testing 155.0.0.1") {
		t.Errorf("path output:\n%s", h.out.String())
	}

	// already installed: nothing is downloaded again, and --force does
	if code := h.run("browser", "install"); code != exitOK || *hits != 1 || !strings.Contains(h.out.String(), "already installed") {
		t.Errorf("second install: code %d, %d downloads, %q", code, *hits, h.out.String())
	}
	if code := h.run("browser", "install", "--force"); code != exitOK || *hits != 2 {
		t.Errorf("forced install: code %d, %d downloads", code, *hits)
	}

	// the order: the setting beats NOSY_CHROME beats the pinned build
	exe, _ := os.Executable()
	h.env["NOSY_CHROME"] = exe
	h.run("browser", "path")
	if !strings.HasPrefix(h.out.String(), exe) || !strings.Contains(h.out.String(), "NOSY_CHROME names it") {
		t.Errorf("env: %q", h.out.String())
	}
	h.run("config", "set", "chrome", filepath.Join(filepath.Dir(exe), filepath.Base(exe)))
	h.run("browser", "path")
	if !strings.Contains(h.out.String(), `"chrome" setting`) {
		t.Errorf("config: %q", h.out.String())
	}
	h.run("config", "set", "chrome", "")
	delete(h.env, "NOSY_CHROME")

	// a system Chrome next to Nosy's download is left alone by remove
	sibling := filepath.Join(filepath.Dir(h.cache), "keep.txt")
	os.WriteFile(sibling, []byte("x"), 0o644)
	if code := h.run("browser", "remove"); code != exitOK || !strings.Contains(h.out.String(), "Removed "+h.cache) || !strings.Contains(h.out.String(), "you installed yourself was not touched") {
		t.Errorf("remove: code %d, %q", code, h.out.String())
	}
	if _, err := os.Stat(h.cache); err == nil {
		t.Error("the downloaded browser is still there")
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Error("remove deleted something that is not Nosy's browser folder")
	}
	if code := h.run("browser", "remove"); code != exitOK || !strings.Contains(h.out.String(), "Nothing to remove") {
		t.Errorf("second remove: code %d, %q", code, h.out.String())
	}
}

func TestBrowserInstallRefusesAWrongDownload(t *testing.T) {
	h := newHarness(t)
	pinnedServer(t, h, true)
	if code := h.run("browser", "install"); code != exitFailed || !strings.Contains(h.err.String(), "does not match the pinned SHA-256") {
		t.Errorf("code %d, stderr %q", code, h.err.String())
	}
	if _, err := os.Stat(filepath.Join(h.cache, "155.0.0.1")); err == nil {
		t.Error("a download that failed the hash was installed")
	}
	if code := h.run("browser", "path"); code != exitFailed {
		t.Errorf("path after a refused install: code %d", code)
	}
}

func TestBrowserInstallShowsABarOnATerminal(t *testing.T) {
	h := newHarness(t)
	pinnedServer(t, h, false)
	h.app.progress = true
	if code := h.run("browser", "install"); code != exitOK {
		t.Fatalf("code %d, stderr %q", code, h.err.String())
	}
	got := h.err.String()
	for _, want := range []string{"Downloading [", "100%", "Checking the SHA-256", "Unpacking"} {
		if !strings.Contains(got, want) {
			t.Errorf("status line lacks %q in %q", want, got)
		}
	}
	if strings.Contains(got, "...") || strings.Contains(got, "\u2026") || strings.Contains(got, "\n") {
		t.Errorf("status line has an ellipsis or a newline: %q", got)
	}
	if strings.Contains(h.out.String(), "downloaded ") {
		t.Errorf("plain progress lines on a terminal: %q", h.out.String())
	}
}

func TestBrowserUsageErrors(t *testing.T) {
	h := newHarness(t)
	if code := h.run("browser"); code != exitUsage || !strings.Contains(h.err.String(), "install [--force]") {
		t.Errorf("browser alone: code %d", code)
	}
	for _, args := range [][]string{{"browser", "update"}, {"browser", "install", "x"}, {"browser", "install", "--nope"}, {"browser", "path", "x"}, {"browser", "remove", "x"}} {
		if code := h.run(args...); code != exitUsage {
			t.Errorf("%v: code %d, stderr %q", args, code, h.err.String())
		}
	}
	for _, args := range [][]string{{"browser", "-h"}, {"browser", "install", "-h"}, {"browser", "path", "-h"}, {"browser", "remove", "-h"}, {"help", "browser"}} {
		if code := h.run(args...); code != exitOK || !strings.Contains(h.out.String(), "Usage: nosy browser") {
			t.Errorf("%v: code %d", args, code)
		}
	}
}

func TestBrowserOnAnUnsupportedSystem(t *testing.T) {
	h := newHarness(t)
	h.app.installer = func() (*chrome.Installer, error) {
		_, err := chrome.CurrentPlatform()
		if err == nil {
			return nil, os.ErrInvalid
		}
		return nil, err
	}
	// on a supported system CurrentPlatform works; the error path is the installer failing
	if code := h.run("browser", "install"); code != exitFailed {
		t.Errorf("install: code %d", code)
	}
	if code := h.run("browser", "path"); code != exitFailed {
		t.Errorf("path: code %d", code)
	}
	if code := h.run("doctor"); !strings.Contains(h.out.String(), "Pinned") || code == exitUsage {
		t.Errorf("doctor: code %d\n%s", code, h.out.String())
	}
}
