package browser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

// ErrNoSandbox means the browser starts only without its sandbox. Nosy never
// runs it that way.
var ErrNoSandbox = errors.New("the browser's sandbox is not available")

// Info is what Probe learned about a browser.
type Info struct {
	Path    string
	Version string // for example Chrome/155.0.8059.39
	Sandbox bool   // it starts with its sandbox on
}

// Probe starts the browser once, with its sandbox, and asks for its version.
// If it will not start because of the sandbox the error is ErrNoSandbox. The
// browser is never started without its sandbox, not even to find out why.
func Probe(ctx context.Context, path string) (Info, error) {
	info := Info{Path: path}
	var out bytes.Buffer
	v, err := probeVersion(ctx, path, &out)
	if err == nil {
		info.Version, info.Sandbox = v, true
		return info, nil
	}
	msg := strings.TrimSpace(out.String())
	if strings.Contains(strings.ToLower(msg+" "+err.Error()), "sandbox") {
		return info, ErrNoSandbox
	}
	if msg != "" {
		if len(msg) > 400 {
			msg = msg[:400]
		}
		return info, fmt.Errorf("%w: %s", err, msg)
	}
	return info, err
}

func probeVersion(ctx context.Context, path string, output io.Writer) (string, error) {
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
		noSandboxOff,
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.CombinedOutput(output),
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
