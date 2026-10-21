package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/session"
	"yipyip/internal/tui"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = "usage: yip [--version] [--help] [--resume [id]]"

const helpUsage = "Usage: yip [--version] [--help] [--resume [id]]\n\n" +
	"yip is a terminal coding assistant that works in the current directory.\n\n" +
	"Options:\n" +
	"  --resume [id], -c [id]  resume a saved session of this directory (without id: pick one)\n\n" +
	"Commands:\n"

// flags is the parsed command line of an interactive start.
type flags struct {
	resume   bool   // --resume / -c given
	resumeID string // its id; "" = none (picker or fresh start)
}

// deps is what an interactive start needs from the outside world: whether
// stdin is a terminal and how the UI is run. Tests replace launch.
type deps struct {
	stdinTTY bool
	launch   func(opts tui.Options, stderr io.Writer) int
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWith(args, stdout, stderr, deps{stdinTTY: stdinIsTTY(), launch: launchUI})
}

// runWith parses args and, unless a flag finished the run (--version,
// --help, a usage error), starts the UI through d.
func runWith(args []string, stdout, stderr io.Writer, d deps) int {
	f, code, done := parseArgs(args, stdout, stderr)
	if done {
		return code
	}
	return runUI(f, d, stderr)
}

// parseArgs parses the command line. done reports that the run is over with
// exit code code (--version, --help, unknown argument).
func parseArgs(args []string, stdout, stderr io.Writer) (f flags, code int, done bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--version" || arg == "-version":
			fmt.Fprintf(stdout, "yip %s\n", version)
			return f, 0, true
		case arg == "--help" || arg == "-help" || arg == "-h":
			fmt.Fprint(stdout, helpUsage)
			fmt.Fprint(stdout, tui.HelpText())
			return f, 0, true
		case arg == "--resume" || arg == "-resume" || arg == "-c":
			f.resume, f.resumeID = true, ""
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				f.resumeID = args[i]
			}
		case strings.HasPrefix(arg, "--resume="), strings.HasPrefix(arg, "-resume="), strings.HasPrefix(arg, "-c="):
			_, id, _ := strings.Cut(arg, "=")
			f.resume, f.resumeID = true, id
		default:
			fmt.Fprintf(stderr, "yip: unknown argument %q\n%s\n", arg, usage)
			return f, 2, true
		}
	}
	return f, 0, false
}

// runUI loads the configuration, builds the session and the UI options
// (including what to resume) and runs the UI through d.launch.
func runUI(f flags, d deps, stderr io.Writer) int {
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
	sess, err := agent.NewSession(agent.Options{Config: cfg, StartDir: startDir})
	if err != nil {
		fmt.Fprintf(stderr, "yip: %v\n", err)
		return 1
	}
	opts := newOptions(startDir, sess)
	applyStoredSettings(&opts, cfg, sess)
	opts.StdinTTY = d.stdinTTY
	// Model, effort and mode changes are saved to the user config file.
	opts.SettingsPath = config.GlobalPath(home)
	// New tabs get sessions with the same config and start directory; the
	// UI gives them the current model, effort and mode.
	opts.NewSession = func() (*agent.Session, error) {
		return agent.NewSession(agent.Options{Config: cfg, StartDir: startDir})
	}
	if err := applyResume(&opts, f); err != nil {
		sess.Cancel()
		fmt.Fprintf(stderr, "unknown session %s\n", f.resumeID)
		return 1
	}
	return d.launch(opts, stderr)
}

// applyResume adds what --resume asks for to opts. With an id the session
// is loaded from the store of opts.StartDir; any load error is returned
// (the caller prints only the id, never the error, which names a path).
// Without id the picker is requested only when stdin is a tty and the store
// holds at least one session; otherwise the start is fresh.
func applyResume(opts *tui.Options, f flags) error {
	if !f.resume {
		return nil
	}
	store, storeErr := session.NewStore(opts.StartDir)
	if f.resumeID != "" {
		if storeErr != nil {
			return storeErr
		}
		data, err := store.Load(f.resumeID)
		if err != nil {
			return err
		}
		opts.ResumeData = data
		return nil
	}
	if storeErr != nil || !opts.StdinTTY {
		return nil
	}
	if list, err := store.List(); err == nil && len(list) > 0 {
		opts.ResumePick = true
	}
	return nil
}

// launchUI runs the Bubble Tea program for opts.
func launchUI(opts tui.Options, stderr io.Writer) int {
	defer enableKeyReporting(os.Stdout, stdoutIsTTY())()
	p := tea.NewProgram(tui.New(opts), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(stderr, "yip: %v\n", err)
		return 1
	}
	return 0
}

// keyReportingLevel is the xterm modifyOtherKeys level yip requests. Level 1
// reports only modified keys that have no distinct legacy encoding, which
// includes Shift+Enter, and leaves Ctrl+<letter> and Alt+<key> as they are,
// so the existing bindings keep working. Level 2 would also report those
// (for example Alt+1 or Ctrl+C as CSI 27;...~) and bubbletea 1.3 would not
// understand them. The kitty keyboard protocol is not requested at all:
// bubbletea 1.3 cannot parse its CSI-u keys.
const keyReportingLevel = 1

// enableKeyReporting asks a terminal to report modified keys such as
// Shift+Enter (ESC [ > 4 ; level m) and returns the function that restores
// the default (ESC [ > 4 ; 0 m). If tty is false (stdout is a pipe or a
// file) nothing is written and the returned function does nothing.
func enableKeyReporting(w io.Writer, tty bool) (restore func()) {
	if !tty {
		return func() {}
	}
	fmt.Fprintf(w, "\x1b[>4;%dm", keyReportingLevel)
	return func() { fmt.Fprint(w, "\x1b[>4;0m") }
}

// stdoutIsTTY reports whether stdout is a character device.
func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// stdinIsTTY reports whether stdin is a character device.
func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// newOptions switches the session to auto mode, which is the mode yip
// starts in, and returns the UI options around it.
func newOptions(startDir string, sess *agent.Session) tui.Options {
	sess.SetMode(agent.ModeAuto)
	return tui.Options{StartDir: startDir, Session: sess, Mode: string(agent.ModeAuto)}
}

// applyStoredSettings applies the "effort" and "mode" keys of the merged
// config (user config, project yipyip.json on top) to sess and opts, as the
// /effort and /mode commands would, but without saving or printing anything.
// A missing or invalid value (wrong type, unknown string) is ignored silently.
func applyStoredSettings(opts *tui.Options, cfg *config.Config, sess *agent.Session) {
	if e, ok := cfg.Raw[config.SettingsKeyEffort].(string); ok {
		if sess.SetEffort(e) == nil {
			opts.Effort = e
		}
	}
	if m, ok := cfg.Raw[config.SettingsKeyMode].(string); ok {
		switch agent.Mode(m) {
		case agent.ModeAuto, agent.ModeManual:
			sess.SetMode(agent.Mode(m))
			opts.Mode = m
		}
	}
}

// newModel builds the UI model of a fresh start (see newOptions).
func newModel(startDir string, sess *agent.Session) tui.Model {
	return tui.New(newOptions(startDir, sess))
}
