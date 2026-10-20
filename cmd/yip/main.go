package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/config"
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
		return runUI(stderr)
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

// runUI loads the configuration and runs the interactive UI.
func runUI(stderr io.Writer) int {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "yip: %v\n", err)
		return 1
	}
	startDir, err := filepath.EvalSymlinks(wd)
	if err != nil {
		fmt.Fprintf(stderr, "yip: %v\n", err)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "yip: %v\n", err)
		return 1
	}
	cfg, err := config.Load(home, startDir, os.Getenv)
	if err != nil {
		fmt.Fprintf(stderr, "yip: %v\n", err)
		return 1
	}
	p := tea.NewProgram(tui.New(tui.Options{StartDir: startDir, ModelID: cfg.DefaultModel}), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(stderr, "yip: %v\n", err)
		return 1
	}
	return 0
}
