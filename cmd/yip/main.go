package main

import (
	"fmt"
	"io"
	"os"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = "usage: yip [--version] [--help]"

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
			fmt.Fprintln(stdout, usage)
			return 0
		default:
			fmt.Fprintf(stderr, "yip: unknown argument %q\n%s\n", arg, usage)
			return 2
		}
	}
	return 0
}
