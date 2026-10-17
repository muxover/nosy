package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"

	"github.com/muxover/nosy/internal/check"
)

const checkUsage = `Usage: nosy check [flags] <url>

Looks at a site without a browser: where its name leads, what its home page
answers, the redirects on the way and the cookies the first answer sets. It
reads only the home page, and only its headers.

Flags:
  --allow-private  check private, loopback and link-local addresses too
  --json           print the result as JSON on stdout

Exit codes: 0 checked, 1 the check could not run, 2 wrong usage.
`

func (a *app) check(ctx context.Context, args []string) int {
	fs := newFlagSet("check")
	allowPrivate := fs.Bool("allow-private", false, "")
	asJSON := fs.Bool("json", false, "")
	pos, err := parseFlags(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(a.out, checkUsage)
		return exitOK
	}
	if err != nil {
		return a.usageError("check", err)
	}
	if len(pos) != 1 {
		return a.usageError("check", errors.New("give one address to check"))
	}
	if *allowPrivate {
		fmt.Fprintln(a.err, allowPrivateWarning)
	}
	r, err := check.Run(ctx, pos[0], check.Options{Version: nosyVersion(), AllowPrivate: *allowPrivate})
	if err != nil {
		return a.scanError(err, *allowPrivate)
	}
	if *asJSON {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(r); err != nil {
			return a.fail(err)
		}
		return exitOK
	}
	printChecks(a.out, r)
	return exitOK
}
