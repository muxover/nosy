package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/muxover/nosy/internal/chrome"
	"github.com/muxover/nosy/internal/guard"
	"github.com/muxover/nosy/internal/scan"
	"github.com/muxover/nosy/internal/site"
)

const scanUsage = `Usage: nosy scan [flags] <url>

Opens the site in a clean, throwaway Chrome, with a new profile and every byte
sent through a guard that refuses private addresses, and reports what the page
does before any click.

Flags:
  --duration D     longest the visit may last, 10s to 120s (default 20s)
  --device D       desktop or mobile (default desktop)
  --gpc            send the Global Privacy Control signal
  --allow-private  scan private, loopback and link-local addresses too
  --json           print the result as JSON on stdout
  --quiet          no progress, and a one-line summary

Defaults for --duration, --device and --gpc come from "nosy config".
Exit codes: 0 scanned, 1 the scan failed or was blocked, 2 wrong usage, 130 interrupted.
`

const allowPrivateWarning = `warning: --allow-private is on. Nosy will connect to private, loopback and link-local
addresses, including this machine and your network. Scan only sites you own.`

const noChromeHelp = `Run "nosy browser install" to download Nosy's own Chrome for Testing, install Google Chrome or
Chromium, or point NOSY_CHROME (or "nosy config set chrome") at a Chromium browser.`

// scanSettings are the options a scan command line and the config add up to.
type scanSettings struct {
	duration time.Duration
	mobile   bool
	gpc      bool
}

func (a *app) scan(ctx context.Context, args []string) int {
	fs := newFlagSet("scan")
	durationFlag := fs.String("duration", "", "")
	deviceFlag := fs.String("device", "", "")
	gpc := fs.Bool("gpc", false, "")
	allowPrivate := fs.Bool("allow-private", false, "")
	asJSON := fs.Bool("json", false, "")
	quiet := fs.Bool("quiet", false, "")
	pos, err := parseFlags(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(a.out, scanUsage)
		return exitOK
	}
	if err != nil {
		return a.usageError("scan", err)
	}
	if len(pos) != 1 {
		return a.usageError("scan", errors.New("give one address to scan"))
	}
	// a wrong address is a usage error whatever else is missing on this machine
	if _, err := site.Parse(pos[0], *allowPrivate); err != nil {
		return a.scanError(&site.InputError{Err: err}, *allowPrivate)
	}
	cur, err := a.loadSettings()
	if err != nil {
		return a.fail(err)
	}
	set := setFlags(fs)
	ss, err := resolveScan(cur, set, *durationFlag, *deviceFlag, *gpc)
	if err != nil {
		return a.usageError("scan", err)
	}
	choice, err := a.chooseBrowser(cur)
	if err != nil {
		return a.noBrowser(err)
	}

	if *allowPrivate {
		fmt.Fprintln(a.err, allowPrivateWarning)
	}
	opts := scan.Options{
		Version:       nosyVersion(),
		Chrome:        choice.Path,
		BrowserSource: choice.Source,
		Duration:      ss.duration,
		Mobile:        ss.mobile,
		GPC:           ss.gpc,
		AllowPrivate:  *allowPrivate,
	}
	var st *status
	if a.progress && !*quiet {
		st = startStatus(a.err)
		opts.Progress = st.set
	}
	r, err := scan.Run(ctx, pos[0], opts)
	if st != nil {
		st.stop()
	}
	if err != nil {
		return a.scanError(err, *allowPrivate)
	}

	switch {
	case *asJSON:
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(r); err != nil {
			return a.fail(err)
		}
	case *quiet:
		printOneLine(a.out, r)
	default:
		printSummary(a.out, r)
	}
	if r.Status != scan.StatusDone {
		return exitFailed
	}
	return exitOK
}

// resolveScan applies the flags the user gave on top of the config.
func resolveScan(cur settings, set map[string]bool, durationFlag, deviceFlag string, gpcFlag bool) (scanSettings, error) {
	ss := scanSettings{duration: guard.HardStop}
	if cur.Duration > 0 {
		ss.duration = time.Duration(cur.Duration) * time.Second
	}
	device := cur.Device
	if set["duration"] {
		d, err := parseDuration(durationFlag)
		if err != nil {
			return ss, fmt.Errorf("--duration: %w", err)
		}
		ss.duration = d
	}
	if set["device"] {
		device = deviceFlag
	}
	if device != "" {
		mobile, err := parseDevice(device)
		if err != nil {
			return ss, fmt.Errorf("--device: %w", err)
		}
		ss.mobile = mobile
	}
	if cur.GPC != nil {
		ss.gpc = *cur.GPC
	}
	if set["gpc"] {
		ss.gpc = gpcFlag
	}
	return ss, nil
}

// chooseBrowser picks the browser: the chrome setting, NOSY_CHROME, Nosy's
// pinned build, then a Chrome or Chromium found on the machine.
func (a *app) chooseBrowser(cur settings) (chrome.Choice, error) {
	in, err := a.installer()
	if err != nil {
		in = nil // no pinned build for this system; the others still apply
	}
	return chrome.Select(chrome.Preference{Config: cur.Chrome, Env: a.getenv("NOSY_CHROME"), System: a.findSystem}, in)
}

func (a *app) noBrowser(err error) int {
	fmt.Fprintf(a.err, "nosy: %v\n%s\n", err, noChromeHelp)
	return exitFailed
}

// scanError turns an error from a scan or check that could not run into a message and a code.
func (a *app) scanError(err error, allowPrivate bool) int {
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(a.err, "nosy: interrupted")
		return exitInterrupted
	}
	var ie *site.InputError
	if errors.As(err, &ie) {
		fmt.Fprintf(a.err, "nosy: %v\n", err)
		if !allowPrivate && refusedAsPrivate(err) {
			fmt.Fprintln(a.err, "To scan a site of your own, add --allow-private.")
		}
		return exitUsage
	}
	return a.fail(err)
}

func refusedAsPrivate(err error) bool {
	for _, target := range []error{site.ErrIP, site.ErrLocal, site.ErrPort, guard.ErrBlockedAddr, guard.ErrPort} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
