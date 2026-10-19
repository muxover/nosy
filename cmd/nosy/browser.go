package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
)

const browserUsage = `Usage: nosy browser <command>

Commands:
  install [--force]  download Nosy's own Chrome for Testing build into your user cache
  path               show the browser a scan would use, and why
  remove             delete the browsers Nosy downloaded (never a Chrome you installed)

The pinned build is checked against a SHA-256 kept in Nosy's code before it is unpacked.
A scan uses, in this order: the "chrome" setting, NOSY_CHROME, Nosy's pinned build, and
a Chrome or Chromium found on this machine. Other Chromium browsers are used only when
a path is given.
`

func (a *app) browser(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.err, browserUsage)
		return exitUsage
	}
	switch args[0] {
	case "-h", "--help", "-help":
		fmt.Fprint(a.out, browserUsage)
		return exitOK
	case "install":
		return a.browserInstall(ctx, args[1:])
	case "path":
		return a.browserPath(args[1:])
	case "remove":
		return a.browserRemove(args[1:])
	}
	return a.usageError("browser", fmt.Errorf("unknown command %q", args[0]))
}

func (a *app) browserInstall(ctx context.Context, args []string) int {
	fs := newFlagSet("browser install")
	force := fs.Bool("force", false, "")
	pos, err := parseFlags(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(a.out, browserUsage)
		return exitOK
	}
	if err != nil {
		return a.usageError("browser", err)
	}
	if len(pos) != 0 {
		return a.usageError("browser", errors.New("install takes no arguments"))
	}
	in, err := a.installer()
	if err != nil {
		return a.fail(err)
	}
	if path, ok := in.Installed(); ok && !*force {
		fmt.Fprintf(a.out, "Chrome for Testing %s is already installed.\n%s\nUse --force to download it again.\n", in.Version, path)
		return exitOK
	}
	fmt.Fprintf(a.out, "Installing Chrome for Testing %s (%s) into %s\n", in.Version, in.Platform.Name, in.Root())

	var st *status
	if a.progress {
		st = startStatus(a.err)
		in.Progress = func(done, total int64, phase string) { st.set(progressText(done, total, phase)) }
	} else {
		in.Progress = plainProgress(a.out)
	}
	path, err := in.Install(ctx, *force)
	if st != nil {
		st.stop()
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(a.err, "nosy: interrupted")
			return exitInterrupted
		}
		return a.fail(err)
	}
	fmt.Fprintf(a.out, "Installed %s\nSHA-256 checked against the pinned %s\nBrowser: %s\n", in.Version, in.Platform.SHA256[:12], path)
	return exitOK
}

// progressText is the status line of a download: a bar and the megabytes.
func progressText(done, total int64, phase string) string {
	switch phase {
	case "downloading":
		const width = 24
		filled := 0
		if total > 0 {
			filled = int(done * width / total)
		}
		pct := 0
		if total > 0 {
			pct = int(done * 100 / total)
		}
		return fmt.Sprintf("Downloading [%s%s] %3d%%  %d / %d MB", strings.Repeat("#", filled), strings.Repeat("-", width-filled), pct, done>>20, total>>20)
	case "verifying":
		return "Checking the SHA-256"
	}
	return "Unpacking"
}

// plainProgress prints a line per step and one per quarter of the download, for
// output that is not a terminal.
func plainProgress(w interface{ Write([]byte) (int, error) }) func(done, total int64, phase string) {
	lastQuarter := -1
	lastPhase := ""
	return func(done, total int64, phase string) {
		switch phase {
		case "downloading":
			q := 0
			if total > 0 {
				q = int(done * 4 / total)
			}
			if q != lastQuarter {
				lastQuarter = q
				fmt.Fprintf(w, "downloaded %d of %d MB\n", done>>20, total>>20)
			}
		default:
			if phase != lastPhase {
				fmt.Fprintf(w, "%s\n", strings.ToLower(progressText(done, total, phase)))
			}
		}
		lastPhase = phase
	}
}

func (a *app) browserPath(args []string) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprint(a.out, browserUsage)
		return exitOK
	}
	if len(args) != 0 {
		return a.usageError("browser", errors.New("path takes no arguments"))
	}
	cur, err := a.loadSettings()
	if err != nil {
		return a.fail(err)
	}
	choice, err := a.chooseBrowser(cur)
	if err != nil {
		return a.noBrowser(err)
	}
	fmt.Fprintln(a.out, choice.Path)
	fmt.Fprintf(a.out, "Used because %s.\n", choice.Why())
	return exitOK
}

func (a *app) browserRemove(args []string) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprint(a.out, browserUsage)
		return exitOK
	}
	if len(args) != 0 {
		return a.usageError("browser", errors.New("remove takes no arguments"))
	}
	in, err := a.installer()
	if err != nil {
		return a.fail(err)
	}
	removed, err := in.Remove()
	if err != nil {
		return a.fail(err)
	}
	if !removed {
		fmt.Fprintf(a.out, "Nothing to remove: Nosy has downloaded no browser (looked in %s).\n", in.Root())
		return exitOK
	}
	fmt.Fprintf(a.out, "Removed %s. A Chrome or Chromium you installed yourself was not touched.\n", in.Root())
	return exitOK
}
