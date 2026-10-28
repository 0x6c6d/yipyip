package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"yipyip/internal/crashlog"
	"yipyip/internal/tools"
)

// testPanic forces a panic for the crash tests; it is set only with
// -ldflags "-X main.testPanic=<mode>" and empty in the shipped binary.
// Modes: "main" (start of runUI, before the config is loaded), "launch" and
// "leak" (inside launchUI, before the program starts).
var testPanic string

// forcedPanicValue is the panic value of the "launch" and "leak" test modes.
const forcedPanicValue = "forced test panic"

// recorded reports that the UI guard wrote a fatal record in this process;
// recordedPath and recordedErr hold the result of that write.
var (
	recorded     atomic.Bool
	recordedMu   sync.Mutex
	recordedPath string
	recordedErr  error
)

// crashState carries the crash logger of one run; runUI replaces the
// startup logger once the configuration is loaded.
type crashState struct {
	logger *crashlog.Logger
}

// startupLogger returns the logger used before the configuration is loaded:
// no redaction, crash.log confined to the working directory. If no start
// directory can be resolved, every record goes to stderr.
func startupLogger() *crashlog.Logger {
	o := crashlog.Options{Version: version}
	if wd, err := os.Getwd(); err == nil {
		if g, err := tools.NewPathGuard(wd); err == nil {
			o.Resolve = g.Resolve
		}
	}
	if o.Resolve == nil {
		o.Resolve = func(string) (string, error) { return "", errors.New("no start directory") }
	}
	return crashlog.New(o)
}

// crashExit is deferred around a whole run: on a panic it writes a fatal
// record, prints the one-line notice to stderr and sets *code to 2.
func crashExit(cs *crashState, stderr io.Writer, code *int) {
	r := recover()
	if r == nil {
		return
	}
	l := cs.logger
	if l == nil {
		l = startupLogger()
	}
	path, err := l.Fatal(r)
	reportCrash(stderr, path, err)
	*code = 2
}

// reportCrash prints the single stderr line after a fatal record was
// written to path (or, on err, to stderr itself).
func reportCrash(stderr io.Writer, path string, err error) {
	if err != nil || path == "" {
		fmt.Fprintln(stderr, "yip: crashed (could not write the crash log)")
		return
	}
	fmt.Fprintf(stderr, "yip: crashed, details in %s\n", path)
}

// recordGuardPanic is the recorder of tui.Guard: the first panic of the
// process is written with the default logger and remembered for the
// launcher; later ones are ignored (the program is going down anyway).
func recordGuardPanic(v any) {
	if !recorded.CompareAndSwap(false, true) {
		return
	}
	path, err := defaultLogger().Fatal(v)
	recordedMu.Lock()
	recordedPath, recordedErr = path, err
	recordedMu.Unlock()
}

// defaultLogger returns the package default crash logger, or the startup
// logger while none is set.
func defaultLogger() *crashlog.Logger {
	if l := crashlog.Default(); l != nil {
		return l
	}
	return startupLogger()
}

// programPanicExit handles a Run error wrapping tea.ErrProgramPanic: if the
// guard already recorded the panic it only reports it, otherwise it writes a
// fallback record. It returns exit code 2.
func programPanicExit(runErr error, stderr io.Writer) int {
	if recorded.Load() {
		recordedMu.Lock()
		path, err := recordedPath, recordedErr
		recordedMu.Unlock()
		reportCrash(stderr, path, err)
		return 2
	}
	path, err := defaultLogger().Fatal("program panic caught by Bubble Tea (value unavailable): " + runErr.Error())
	reportCrash(stderr, path, err)
	return 2
}

// forcePanic panics when testPanic selects one of modes.
func forcePanic(modes ...string) {
	if testPanic == "" {
		return
	}
	for _, m := range modes {
		if testPanic != m {
			continue
		}
		if m == "leak" {
			panic(forcedPanicValue + " " + os.Getenv("FAKE_KEY"))
		}
		panic(forcedPanicValue)
	}
}
