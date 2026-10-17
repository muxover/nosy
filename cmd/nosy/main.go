package main

import (
	"fmt"
	"io"
	"os"
)

var version = "dev"

const usage = `nosy investigates what a website does to its visitors.

Usage:
  nosy <command>

Commands:
  version   print the nosy version
  help      show this help
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}
	switch args[0] {
	case "version", "--version":
		fmt.Fprintf(stdout, "nosy %s\n", version)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
	default:
		fmt.Fprintf(stderr, "nosy: unknown command %q\n\nRun 'nosy help' for usage.\n", args[0])
		return 2
	}
	return 0
}
