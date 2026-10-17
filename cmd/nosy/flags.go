package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// newFlagSet makes a flag set that reports nothing itself: parse errors and
// -h are handled by the caller.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseFlags parses args with flags allowed before and after the positional
// arguments, which the standard flag package does not do on its own.
func parseFlags(fs *flag.FlagSet, args []string) (pos []string, err error) {
	for {
		if err = fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// setFlags lists the flags the user gave.
func setFlags(fs *flag.FlagSet) map[string]bool {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}

const (
	minDuration = 10 * time.Second
	maxDuration = 120 * time.Second
)

// parseDuration reads "30s", "1m" or a bare number of seconds.
func parseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		n, nerr := strconv.Atoi(strings.TrimSpace(s))
		if nerr != nil {
			return 0, fmt.Errorf("%q is not a duration; write 30, 30s or 1m", s)
		}
		d = time.Duration(n) * time.Second
	}
	if d < minDuration || d > maxDuration {
		return 0, fmt.Errorf("a duration must be between %d and %d seconds", minDuration/time.Second, maxDuration/time.Second)
	}
	return d, nil
}

func parseDevice(s string) (mobile bool, err error) {
	switch s {
	case "desktop":
		return false, nil
	case "mobile":
		return true, nil
	}
	return false, errors.New(`the device must be "desktop" or "mobile"`)
}
