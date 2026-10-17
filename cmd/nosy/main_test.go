package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
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
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{out: &bytes.Buffer{}, err: &bytes.Buffer{}, env: map[string]string{}}
	cfg := filepath.Join(t.TempDir(), "nosy", "config.json")
	h.app = &app{
		out:        h.out,
		err:        h.err,
		getenv:     func(k string) string { return h.env[k] },
		configPath: func() (string, error) { return cfg, nil },
	}
	return h
}

func (h *harness) run(args ...string) int {
	h.out.Reset()
	h.err.Reset()
	return h.app.run(context.Background(), args)
}
