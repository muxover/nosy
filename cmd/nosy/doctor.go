package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/muxover/nosy/internal/browser"
	"github.com/muxover/nosy/internal/chrome"
)

const doctorUsage = `Usage: nosy doctor

Checks that Nosy has what it needs to scan: a Chrome or Chromium that is recent
enough, has the system libraries it links to, and starts with its sandbox on.
Prints what it found and, for anything missing, how to fix it. Nosy never runs
Chrome without its sandbox.

Exit codes: 0 everything is in place, 1 something needed is missing.
`

func (a *app) doctor(ctx context.Context, args []string) int {
	fs := newFlagSet("doctor")
	pos, err := parseFlags(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(a.out, doctorUsage)
		return exitOK
	}
	if err != nil {
		return a.usageError("doctor", err)
	}
	if len(pos) != 0 {
		return a.usageError("doctor", errors.New("doctor takes no arguments"))
	}

	ok := true
	line := func(name, value string) { fmt.Fprintf(a.out, "%-10s%s\n", name, value) }
	more := func(value string) { fmt.Fprintf(a.out, "%-10s%s\n", "", value) }
	problem := func(name, value, fix string) {
		ok = false
		line(name, value)
		more("fix: " + fix)
	}

	line("Nosy", nosyVersion())
	line("System", runtime.GOOS+"/"+runtime.GOARCH)

	cur, cfgErr := a.loadSettings()
	if cfgErr != nil {
		problem("Config", "unreadable: "+cfgErr.Error(), "fix or delete the file, then run nosy doctor again.")
	}
	if path, err := a.configPath(); err == nil {
		state := "not created yet"
		if _, err := os.Stat(path); err == nil {
			state = "found"
		}
		line("Config", path+" ("+state+")")
	} else {
		problem("Config", "no config directory: "+err.Error(), "set XDG_CONFIG_HOME (Linux) or HOME so a config directory exists.")
	}

	in, instErr := a.installer()
	switch {
	case instErr != nil:
		line("Pinned", "no pinned build for this system ("+runtime.GOOS+"/"+runtime.GOARCH+"); Nosy uses a Chrome or Chromium you installed")
	default:
		if path, installed := in.Installed(); installed {
			line("Pinned", "Chrome for Testing "+in.Version+" at "+path)
		} else {
			line("Pinned", "Chrome for Testing "+in.Version+" is not installed; \"nosy browser install\" downloads it")
		}
	}

	choice, err := a.chooseBrowser(cur)
	if err != nil {
		problem("Browser", "not found: "+err.Error(), strings.ReplaceAll(noChromeHelp, "\n", " "))
		line("Rules", "none yet")
		return a.doctorEnd(ok)
	}
	line("Browser", choice.Path)
	more("used because " + choice.Why())

	if runtime.GOOS == "linux" {
		if missing, lerr := a.missingLibs(ctx, choice.Path); lerr != nil {
			line("Libraries", "not checked: "+lerr.Error())
		} else if len(missing) > 0 {
			problem("Libraries", "missing: "+strings.Join(missing, ", "), a.libsFix(missing))
			line("Rules", "none yet")
			return a.doctorEnd(ok)
		} else {
			line("Libraries", "all found")
		}
	}

	info, perr := browser.Probe(ctx, choice.Path)
	switch {
	case perr == nil:
		line("Version", info.Version)
		if verr := chrome.CheckVersion(info.Version); verr != nil {
			problem("Version", verr.Error(), `run "nosy browser install", or update the browser.`)
		} else if !strings.HasPrefix(info.Version, "Chrome/") && !strings.HasPrefix(info.Version, "Chromium/") {
			more("this is not Chrome or Chromium, so results reflect this browser's own behaviour")
		}
		line("Sandbox", "available")
	case errors.Is(perr, browser.ErrNoSandbox):
		problem("Sandbox", "not available: Chrome will not start with it, and Nosy never runs Chrome without it", sandboxFix())
	default:
		problem("Browser", choice.Path+" does not start", "run it by hand to see why: "+choice.Path+" --headless --dump-dom about:blank. Error: "+perr.Error())
	}
	line("Rules", "none yet")
	return a.doctorEnd(ok)
}

func (a *app) doctorEnd(ok bool) int {
	if !ok {
		fmt.Fprintln(a.out, "\nSomething Nosy needs is missing. See the fixes above.")
		return exitFailed
	}
	fmt.Fprintln(a.out, "\nEverything Nosy needs is in place.")
	return exitOK
}

// libsFix is the command that installs the missing libraries, for the
// distribution Nosy is running on.
func (a *app) libsFix(missing []string) string {
	family, known := chrome.FamilyOf(a.osRelease())
	if !known {
		return "install the packages that provide these libraries with your distribution's package manager (Debian and Ubuntu: apt-get, Fedora: dnf, Arch: pacman)."
	}
	cmd, complete := chrome.InstallHint(missing, family)
	if cmd == "" {
		return "install the packages that provide these libraries with your package manager."
	}
	if !complete {
		return cmd + " (and the package for the library not listed in that command)"
	}
	return cmd
}

func sandboxFix() string {
	switch runtime.GOOS {
	case "linux":
		return "run Nosy as a normal user, not root. On Ubuntu 24.04 and later, allow unprivileged user namespaces: sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0. There is no way to turn the sandbox off."
	default:
		return "run Nosy as a normal user, not as an administrator. There is no way to turn the sandbox off."
	}
}
