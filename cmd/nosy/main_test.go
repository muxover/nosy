package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muxover/nosy/internal/chrome"
)

// setupBrowserTests, set by a test build, starts what the browser tests need
// and returns what ends it.
var setupBrowserTests func() func()

func TestMain(m *testing.M) {
	// the test binary doubles as the nosy command, for the tests that need a
	// process of their own
	if os.Getenv("NOSY_TEST_RUN_MAIN") == "1" {
		os.Exit(realMain())
	}
	cleanup := func() {}
	if setupBrowserTests != nil {
		cleanup = setupBrowserTests()
	}
	code := m.Run()
	cleanup()
	os.Exit(code)
}

// harness runs the commands in process, against a config of its own.
type harness struct {
	app      *app
	out, err *bytes.Buffer
	env      map[string]string
	cache    string // where the pinned browser would be installed
	// gate, when set, limits how many commands run a browser at once
	gate chan struct{}
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{out: &bytes.Buffer{}, err: &bytes.Buffer{}, env: map[string]string{}}
	cfg := filepath.Join(t.TempDir(), "nosy", "config.json")
	h.cache = filepath.Join(t.TempDir(), "nosy", "chrome")
	h.app = &app{
		out:        h.out,
		err:        h.err,
		getenv:     func(k string) string { return h.env[k] },
		configPath: func() (string, error) { return cfg, nil },
		// the machine's own Chrome must not leak into the tests
		findSystem:  func() (string, error) { return "", chrome.ErrNoBrowser },
		missingLibs: func(context.Context, string) ([]string, error) { return nil, nil },
		osRelease:   func() string { return "ID=ubuntu\nID_LIKE=debian\n" },
		installer: func() (*chrome.Installer, error) {
			return &chrome.Installer{
				Version:  "155.0.0.1",
				Platform: chrome.Platform{Name: "linux64", Size: 1, SHA256: strings.Repeat("0", 64), Exec: "chrome-linux64/chrome"},
				Dir:      h.cache,
			}, nil
		},
	}
	return h
}

func (h *harness) run(args ...string) int {
	if h.gate != nil {
		h.gate <- struct{}{}
		defer func() { <-h.gate }()
	}
	h.out.Reset()
	h.err.Reset()
	return h.app.run(context.Background(), args)
}
