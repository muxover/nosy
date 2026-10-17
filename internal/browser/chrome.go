package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

// ErrNoBrowser means no Chrome or Chromium was found.
var ErrNoBrowser = errors.New("no Chrome or Chromium found")

// ErrNoSandbox means the browser starts only without its sandbox. Nosy never
// runs it that way.
var ErrNoSandbox = errors.New("the browser's sandbox is not available")

// Find returns the browser to use. A preferred path (from NOSY_CHROME or the
// config) must exist; without one, the usual install locations are searched.
func Find(preferred string) (string, error) {
	if preferred != "" {
		if p, err := exec.LookPath(preferred); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("%w: %s does not exist or cannot be run", ErrNoBrowser, preferred)
	}
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

// Info is what Probe learned about a browser.
type Info struct {
	Path    string
	Version string // for example Chrome/155.0.8059.39
	Sandbox bool   // it starts with its sandbox on
}

// Probe starts the browser once with its sandbox and asks for its version.
// If it starts only without the sandbox, the Info is filled in and the error
// is ErrNoSandbox.
func Probe(ctx context.Context, path string) (Info, error) {
	info := Info{Path: path}
	v, err := probeVersion(ctx, path, true)
	if err == nil {
		info.Version, info.Sandbox = v, true
		return info, nil
	}
	if v, err2 := probeVersion(ctx, path, false); err2 == nil {
		info.Version = v
		return info, ErrNoSandbox
	}
	return info, err
}

func probeVersion(ctx context.Context, path string, sandbox bool) (string, error) {
	profile, err := os.MkdirTemp("", "nosy-probe-")
	if err != nil {
		return "", err
	}
	defer removeProfile(profile)
	opts := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Headless,
		chromedp.UserDataDir(profile),
		chromedp.ExecPath(path),
		chromedp.Flag("disable-dev-shm-usage", true),
	}
	if !sandbox {
		opts = append(opts, chromedp.NoSandbox)
	}
	var cmd *exec.Cmd
	opts = append(opts, chromedp.ModifyCmdFunc(func(c *exec.Cmd) {
		confine(c)
		cmd = c
	}))
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()
	bctx, cancelCtx := chromedp.NewContext(allocCtx)
	defer func() {
		cancelCtx()
		killTree(cmd)
	}()
	var product string
	err = chromedp.Run(bctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, p, _, _, _, err := cdpbrowser.GetVersion().Do(ctx)
		product = p
		return err
	}))
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(product, "Headless"), nil
}
