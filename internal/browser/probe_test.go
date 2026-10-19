//go:build unix

package browser

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muxover/nosy/internal/guard"
)

// script writes a stand-in browser that records its arguments in args.txt.
func script(t *testing.T, body string) (path, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args.txt")
	path = filepath.Join(dir, "chrome")
	src := "#!/bin/sh\necho \"$@\" >> " + argsFile + "\n" + body
	if err := os.WriteFile(path, []byte(src), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, argsFile
}

func TestProbeReportsASandboxFailure(t *testing.T) {
	path, argsFile := script(t, "echo 'No usable sandbox! Update your kernel' >&2\nsleep 0.3\nexit 1\n")
	info, err := Probe(context.Background(), path)
	if !errors.Is(err, ErrNoSandbox) {
		t.Fatalf("error = %v, want ErrNoSandbox", err)
	}
	if info.Sandbox || info.Path != path {
		t.Errorf("info = %+v", info)
	}
	// the browser is started once, and never without its sandbox
	args, _ := os.ReadFile(argsFile)
	if n := strings.Count(string(args), "\n"); n != 1 {
		t.Errorf("the browser was started %d times:\n%s", n, args)
	}
	if strings.Contains(string(args), "no-sandbox") {
		t.Errorf("the probe turned the sandbox off: %s", args)
	}
}

func TestProbeReportsABrowserThatDoesNotStart(t *testing.T) {
	path, _ := script(t, "echo 'error while loading shared libraries: libnss3.so' >&2\nsleep 0.3\nexit 127\n")
	_, err := Probe(context.Background(), path)
	if err == nil || errors.Is(err, ErrNoSandbox) {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "libnss3.so") {
		t.Errorf("the browser's own message is lost: %v", err)
	}
}

// chromedp turns the sandbox off by itself for a process running as root. A
// visit and a probe must not, whoever runs them.
func TestAVisitNeverTurnsTheSandboxOff(t *testing.T) {
	path, argsFile := script(t, "exit 1\n")
	proxy, err := guard.StartProxy(&guard.Dialer{}, guard.MaxBytes, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	target, _ := url.Parse("https://example.com/")
	if _, err := Visit(context.Background(), target, Options{Chrome: path, Proxy: proxy}); err == nil {
		t.Fatal("a browser that exits was visited with")
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(args), "--remote-debugging-port") {
		t.Fatalf("the stand-in browser was not started as a visit starts one: %q", args)
	}
	if strings.Contains(string(args), "no-sandbox") {
		t.Errorf("the visit turned the sandbox off: %s", args)
	}
}
