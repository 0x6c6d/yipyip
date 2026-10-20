package main

import (
	"fmt"
	"io"
	"os"

	"yipyip/internal/tui"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = "usage: yip [--version] [--help]"

const helpUsage = "Usage: yip [--version] [--help]\n\n" +
	"yip is a terminal coding assistant that works in the current directory.\n\n" +
	"Commands:\n"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "yip: interactive UI not built yet")
		return 0
	}
	for _, arg := range args {
		switch arg {
		case "--version", "-version":
			fmt.Fprintf(stdout, "yip %s\n", version)
			return 0
		case "--help", "-help", "-h":
			fmt.Fprint(stdout, helpUsage)
			fmt.Fprint(stdout, tui.HelpText())
			return 0
		default:
			fmt.Fprintf(stderr, "yip: unknown argument %q\n%s\n", arg, usage)
			return 2
		}
	}
	return 0
}
