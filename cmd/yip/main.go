package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/crashlog"
	"yipyip/internal/session"
	"yipyip/internal/tools"
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
// --help, a usage error), starts the UI through d. A panic anywhere in the
// run is written to the crash log (see crashExit) and gives exit code 2.
func runWith(args []string, stdout, stderr io.Writer, d deps) (code int) {
	prev := crashlog.Default()
	defer crashlog.SetDefault(prev)
	cs := &crashState{}
	defer crashExit(cs, stderr, &code)
	f, code, done := parseArgs(args, stdout, stderr)
	if done {
		return code
	}
	return runUI(f, d, stderr, cs)
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
// (including what to resume) and runs the UI through d.launch. Once the
// configuration is loaded, cs gets the crash logger (also the package default).
func runUI(f flags, d deps, stderr io.Writer, cs *crashState) int {
	forcePanic("main")
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
	if guard, gerr := tools.NewPathGuard(startDir); gerr == nil {
		cs.logger = crashlog.New(crashlog.Options{
			Version:   version,
			Redact:    cfg.Redactor.Redact,
			Resolve:   guard.Resolve,
			SessionID: func() string { return "" }, // the UI does not expose it yet
		})
		crashlog.SetDefault(cs.logger)
	}
	sess, err := agent.NewSession(agent.Options{Config: cfg, StartDir: startDir})
	if err != nil {
		fmt.Fprintf(stderr, "yip: %v\n", err)
		return 1
	}
	opts := newOptions(startDir, sess)
	applyStoredSettings(&opts, cfg, sess)
	opts.StdinTTY = d.stdinTTY
	opts.Home = home
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
	pruneSessions(startDir, f)
	return d.launch(opts, stderr)
}

// keepSessions is how many sessions a start keeps in its sessions directory.
const keepSessions = 25

// pruneSessions trims the sessions of startDir to the newest keepSessions
// (session.Store.Prune), protecting the session reopened by --resume <id>.
// It runs synchronously before the UI exists, after applyResume loaded the
// resumed session. A store that cannot be created skips it; a prune error
// is ignored. A resume picker (--resume without id) protects nothing.
func pruneSessions(startDir string, f flags) {
	store, err := session.NewStore(startDir)
	if err != nil {
		return
	}
	var protect []string
	if f.resume && f.resumeID != "" {
		protect = append(protect, f.resumeID)
	}
	_ = store.Prune(keepSessions, protect...)
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
	return launchWith(opts, stderr, func(m tea.Model) error {
		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
		_, err := p.Run()
		return err
	})
}

// launchWith runs the guarded UI model of opts through runProgram. Bubble
// Tea keeps catching panics (and restores the terminal); a panic it reports
// gives exit code 2 with one crash record (the guard's, or a fallback).
// Other errors print "yip: <err>" and give exit code 1.
func launchWith(opts tui.Options, stderr io.Writer, runProgram func(tea.Model) error) int {
	forcePanic("launch", "leak")
	m := tui.Guard(tui.New(opts), recordGuardPanic)
	err := runProgram(m)
	if err == nil {
		return 0
	}
	if errors.Is(err, tea.ErrProgramPanic) {
		return programPanicExit(err, stderr)
	}
	fmt.Fprintf(stderr, "yip: %v\n", err)
	return 1
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
