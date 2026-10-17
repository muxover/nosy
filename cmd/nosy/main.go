package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"runtime/debug"
	"syscall"
)

// version is set at build time; a build from `go install` falls back to the
// module version.
var version = "dev"

// a version of the form v0.1.0 or v0.1.0-rc1, not the pseudo-version of an untagged build
var taggedVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$`)
var pseudoVersion = regexp.MustCompile(`\d{14}-[0-9a-f]{12}`)

func nosyVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && taggedVersion.MatchString(bi.Main.Version) && !pseudoVersion.MatchString(bi.Main.Version) {
		return bi.Main.Version
	}
	return version
}

const usage = `nosy shows what a website does to its visitors, from your own machine.

Usage:
  nosy <command> [flags]

Commands:
  scan <url>      open a site in a clean browser and report what it does
  check <url>     look at a site without a browser
  config          show or change settings (list, get, set, path)
  doctor          check that Nosy has what it needs
  version         print the version
  help            show this help

Run "nosy <command> -h" for the flags of a command.

Exit codes: 0 done, 1 the scan or check failed, 2 wrong usage, 130 interrupted.
`

// Exit codes.
const (
	exitOK          = 0
	exitFailed      = 1
	exitUsage       = 2
	exitInterrupted = 130
)

func main() { os.Exit(realMain()) }

func realMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return newApp().run(ctx, os.Args[1:])
}

// app is everything a command needs from the machine it runs on, so tests can
// stand in for it.
type app struct {
	out, err   io.Writer
	getenv     func(string) string
	configPath func() (string, error)
	// progress is true when status lines may be drawn on err: it is a terminal.
	progress bool
}

func newApp() *app {
	return &app{
		out:        os.Stdout,
		err:        os.Stderr,
		getenv:     os.Getenv,
		configPath: defaultConfigPath,
		progress:   isTerminal(os.Stderr),
	}
}

func (a *app) run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.out, usage)
		return exitOK
	}
	switch cmd, rest := args[0], args[1:]; cmd {
	case "scan":
		return a.scan(ctx, rest)
	case "check":
		return a.check(ctx, rest)
	case "config":
		return a.config(rest)
	case "doctor":
		return a.doctor(ctx, rest)
	case "version", "--version", "-version":
		fmt.Fprintf(a.out, "nosy %s\n", nosyVersion())
		return exitOK
	case "help", "-h", "--help", "-help":
		return a.help(rest)
	default:
		fmt.Fprintf(a.err, "nosy: unknown command %q\n\nRun 'nosy help' for the commands.\n", cmd)
		return exitUsage
	}
}

// help shows the top-level help, or the help of one command.
func (a *app) help(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.out, usage)
		return exitOK
	}
	switch args[0] {
	case "scan", "check", "config", "doctor":
		return a.run(context.Background(), []string{args[0], "-h"})
	case "version", "help":
		fmt.Fprintf(a.out, "nosy %s: no flags\n", args[0])
		return exitOK
	}
	fmt.Fprintf(a.err, "nosy: no help for %q\n\nRun 'nosy help' for the commands.\n", args[0])
	return exitUsage
}

func (a *app) usageError(cmd string, err error) int {
	fmt.Fprintf(a.err, "nosy %s: %v\n\nRun 'nosy %s -h' for usage.\n", cmd, err, cmd)
	return exitUsage
}
