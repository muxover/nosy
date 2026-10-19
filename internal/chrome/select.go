package chrome

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// ErrNoBrowser means no usable Chrome or Chromium was found.
var ErrNoBrowser = errors.New("no Chrome or Chromium found")

// Source says why a browser was chosen.
type Source string

const (
	SourceConfig Source = "config" // the chrome setting
	SourceEnv    Source = "env"    // NOSY_CHROME
	SourcePinned Source = "pinned" // Nosy's own Chrome for Testing build
	SourceSystem Source = "system" // Chrome or Chromium found in the usual places
)

// Choice is the browser a scan will use.
type Choice struct {
	Path   string
	Source Source
	// Version is the pinned version, for SourcePinned.
	Version string
}

// Pinned is true for Nosy's own build.
func (c Choice) Pinned() bool { return c.Source == SourcePinned }

// Why says in a sentence why this browser was chosen.
func (c Choice) Why() string {
	switch c.Source {
	case SourceConfig:
		return `the "chrome" setting names it (nosy config get chrome)`
	case SourceEnv:
		return "NOSY_CHROME names it"
	case SourcePinned:
		return "it is Nosy's own Chrome for Testing " + c.Version + " (nosy browser install)"
	}
	return "it is the Chrome or Chromium found on this machine"
}

// Preference is what the user has set.
type Preference struct {
	Config string // the chrome setting
	Env    string // NOSY_CHROME
}

// Select picks the browser. A path the user set must exist: a wrong setting is
// an error, never a quiet fall back to another browser.
func Select(p Preference, in *Installer) (Choice, error) {
	if p.Config != "" {
		path, err := resolve(p.Config)
		if err != nil {
			return Choice{}, fmt.Errorf(`%w: the "chrome" setting is %q, which does not exist or cannot be run`, ErrNoBrowser, p.Config)
		}
		return Choice{Path: path, Source: SourceConfig}, nil
	}
	if p.Env != "" {
		path, err := resolve(p.Env)
		if err != nil {
			return Choice{}, fmt.Errorf("%w: NOSY_CHROME is %q, which does not exist or cannot be run", ErrNoBrowser, p.Env)
		}
		return Choice{Path: path, Source: SourceEnv}, nil
	}
	if in != nil {
		if path, ok := in.Installed(); ok {
			return Choice{Path: path, Source: SourcePinned, Version: in.Version}, nil
		}
	}
	if path, err := FindSystem(); err == nil {
		return Choice{Path: path, Source: SourceSystem}, nil
	}
	return Choice{}, ErrNoBrowser
}

func resolve(p string) (string, error) { return exec.LookPath(p) }

// FindSystem looks for Google Chrome or Chromium in the usual places.
func FindSystem() (string, error) {
	for _, c := range candidates() {
		if filepath.IsAbs(c) {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				return c, nil
			}
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	return "", ErrNoBrowser
}

func candidates() []string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			filepath.Join(home, "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
			filepath.Join(home, "Applications/Chromium.app/Contents/MacOS/Chromium"),
		}
	case "windows":
		var out []string
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			if dir := os.Getenv(env); dir != "" {
				out = append(out,
					filepath.Join(dir, `Google\Chrome\Application\chrome.exe`),
					filepath.Join(dir, `Chromium\Application\chrome.exe`))
			}
		}
		return out
	}
	return []string{
		"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome",
		"/opt/google/chrome/chrome", "/snap/bin/chromium", "/usr/bin/chromium",
	}
}
