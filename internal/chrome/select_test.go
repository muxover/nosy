package chrome

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeBrowser(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func installedPinned(t *testing.T) *Installer {
	t.Helper()
	in := &Installer{Version: "155.0.0.1", Platform: Platform{Name: "linux64", Exec: "chrome-linux64/chrome"}, Dir: t.TempDir()}
	vdir := filepath.Join(in.Dir, in.Version)
	if err := os.MkdirAll(filepath.Join(vdir, "chrome-linux64"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(vdir, "chrome-linux64", "chrome"), []byte("x"), 0o755)
	os.WriteFile(filepath.Join(vdir, markerName), []byte("{}"), 0o644)
	return in
}

func TestSelectOrder(t *testing.T) {
	dir := t.TempDir()
	cfg := fakeBrowser(t, dir, "from-config")
	env := fakeBrowser(t, dir, "from-env")
	sys := fakeBrowser(t, dir, "google-chrome")
	t.Setenv("PATH", dir)
	pinned := installedPinned(t)

	c, err := Select(Preference{Config: cfg, Env: env}, pinned)
	if err != nil || c.Path != cfg || c.Source != SourceConfig || c.Pinned() {
		t.Errorf("config first: %+v, %v", c, err)
	}
	c, err = Select(Preference{Env: env}, pinned)
	if err != nil || c.Path != env || c.Source != SourceEnv {
		t.Errorf("then NOSY_CHROME: %+v, %v", c, err)
	}
	c, err = Select(Preference{}, pinned)
	if err != nil || c.Source != SourcePinned || !c.Pinned() || c.Version != "155.0.0.1" || !strings.Contains(c.Path, "155.0.0.1") {
		t.Errorf("then the pinned build: %+v, %v", c, err)
	}
	c, err = Select(Preference{}, &Installer{Dir: t.TempDir(), Version: "1", Platform: Platform{Exec: "x"}})
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		if err != nil || c.Path != sys || c.Source != SourceSystem {
			t.Errorf("then a system browser: %+v, %v", c, err)
		}
	}
	for _, c := range []Choice{{Source: SourceConfig}, {Source: SourceEnv}, {Source: SourcePinned, Version: "155"}, {Source: SourceSystem}} {
		if c.Why() == "" {
			t.Errorf("%s has no explanation", c.Source)
		}
	}
}

func TestSelectRefusesAWrongSetting(t *testing.T) {
	dir := t.TempDir()
	good := fakeBrowser(t, dir, "google-chrome")
	t.Setenv("PATH", dir)
	missing := filepath.Join(dir, "gone")

	_, err := Select(Preference{Config: missing, Env: good}, installedPinned(t))
	if !errors.Is(err, ErrNoBrowser) || !strings.Contains(err.Error(), `"chrome" setting`) {
		t.Errorf("a wrong config setting fell through: %v", err)
	}
	_, err = Select(Preference{Env: missing}, installedPinned(t))
	if !errors.Is(err, ErrNoBrowser) || !strings.Contains(err.Error(), "NOSY_CHROME") {
		t.Errorf("a wrong NOSY_CHROME fell through: %v", err)
	}
}

func TestOtherChromiumBrowsersAreNeverFoundByThemselves(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("the PATH search is how Linux finds a browser")
	}
	dir := t.TempDir()
	for _, name := range []string{"microsoft-edge", "microsoft-edge-stable", "msedge", "brave", "brave-browser", "opera", "vivaldi"} {
		fakeBrowser(t, dir, name)
	}
	t.Setenv("PATH", dir)
	if _, err := Select(Preference{}, nil); !errors.Is(err, ErrNoBrowser) {
		t.Errorf("error = %v, want none found", err)
	}
	// pointed at one, it is used
	edge := filepath.Join(dir, "microsoft-edge")
	if c, err := Select(Preference{Env: edge}, nil); err != nil || c.Path != edge {
		t.Errorf("explicit Edge: %+v, %v", c, err)
	}
}

func TestMajorAndMinimumVersion(t *testing.T) {
	for in, want := range map[string]int{"Chrome/155.0.8059.84": 155, "HeadlessChrome/120.0.6099.0": 120, "Chromium/99.0.1": 99} {
		if got, ok := Major(in); !ok || got != want {
			t.Errorf("Major(%q) = %d, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "Chrome", "Chrome/x.1", "/1.2"} {
		if _, ok := Major(in); ok {
			t.Errorf("Major(%q) read a version", in)
		}
	}
	for _, in := range []string{"Chrome/120.0.0.0", "HeadlessChrome/155.0.8059.84"} {
		if err := CheckVersion(in); err != nil {
			t.Errorf("CheckVersion(%q) = %v", in, err)
		}
	}
	err := CheckVersion("Chrome/119.0.6045.0")
	if !errors.Is(err, ErrTooOld) || !strings.Contains(err.Error(), "Chrome 120") || !strings.Contains(err.Error(), "nosy browser install") {
		t.Errorf("an old browser: %v", err)
	}
	if err := CheckVersion("garbage"); err == nil {
		t.Error("an unreadable version was accepted")
	}
}
