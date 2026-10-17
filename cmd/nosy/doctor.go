package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/muxover/nosy/internal/browser"
)

const doctorUsage = `Usage: nosy doctor

Checks that Nosy has what it needs to scan: a Chrome or Chromium it can start
with its sandbox on. Prints what it found and, for anything missing, how to fix it.

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
	line := func(name, value string) { fmt.Fprintf(a.out, "%-9s %s\n", name, value) }
	fix := func(text string) { fmt.Fprintf(a.out, "%-9s fix: %s\n", "", text) }

	line("Nosy", nosyVersion())
	line("System", runtime.GOOS+"/"+runtime.GOARCH)

	cur, cfgErr := a.loadSettings()
	if cfgErr != nil {
		ok = false
		line("Config", "unreadable: "+cfgErr.Error())
		fix("fix or delete the file, then run nosy doctor again.")
	}
	if path, err := a.configPath(); err == nil {
		state := "not created yet"
		if _, err := os.Stat(path); err == nil {
			state = "found"
		}
		line("Config", path+" ("+state+")")
	} else {
		ok = false
		line("Config", "no config directory: "+err.Error())
		fix("set XDG_CONFIG_HOME (Linux) or HOME so a config directory exists.")
	}

	path, err := a.findChrome(cur)
	switch {
	case err != nil:
		ok = false
		line("Chrome", "not found")
		fix("install Google Chrome or Chromium, or set NOSY_CHROME (or \"nosy config set chrome\") to its path. \"nosy browser install\" arrives in a later version.")
		if a.getenv("NOSY_CHROME") != "" || cur.Chrome != "" {
			fmt.Fprintf(a.out, "%-9s %v\n", "", err)
		}
	default:
		info, perr := browser.Probe(ctx, path)
		switch {
		case perr == nil:
			line("Chrome", fmt.Sprintf("%s (%s)", info.Path, info.Version))
			line("Sandbox", "available")
		case errors.Is(perr, browser.ErrNoSandbox):
			ok = false
			line("Chrome", fmt.Sprintf("%s (%s)", info.Path, info.Version))
			line("Sandbox", "not available: Chrome starts only without it, and Nosy never runs it that way")
			fix(sandboxFix())
		default:
			ok = false
			line("Chrome", info.Path+" (does not start)")
			fix("run it by hand to see why: " + info.Path + " --headless --disable-gpu --dump-dom about:blank. Error: " + perr.Error())
		}
	}
	line("Rules", "none yet")

	if !ok {
		fmt.Fprintln(a.out, "\nSomething Nosy needs is missing. See the fixes above.")
		return exitFailed
	}
	fmt.Fprintln(a.out, "\nEverything Nosy needs is in place.")
	return exitOK
}

func sandboxFix() string {
	switch runtime.GOOS {
	case "linux":
		return "run Nosy as a normal user, not root, and on Ubuntu 24.04 or later allow unprivileged user namespaces for Chrome: sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0."
	default:
		return "run Nosy as a normal user, not as an administrator."
	}
}
