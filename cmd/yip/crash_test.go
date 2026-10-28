package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/config"
	"yipyip/internal/crashlog"
	"yipyip/internal/tui"
)

// crashEnv prepares a start dir like resumeStartDir, restores the crash log
// default and the guard flag after the test, and returns the start dir.
func crashEnv(t *testing.T) string {
	t.Helper()
	dir := resumeStartDir(t)
	recorded.Store(false)
	t.Cleanup(func() {
		crashlog.SetDefault(nil)
		recorded.Store(false)
		recordedMu.Lock()
		recordedPath, recordedErr = "", nil
		recordedMu.Unlock()
	})
	return dir
}

// setTestPanic sets the panic seam for one test.
func setTestPanic(t *testing.T, mode string) {
	t.Helper()
	old := testPanic
	testPanic = mode
	t.Cleanup(func() { testPanic = old })
}

// readCrashLog returns the crash.log of dir ("" when it is missing).
func readCrashLog(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".yipyip", "crash.log"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ""
		}
		t.Fatal(err)
	}
	return string(b)
}

// crashLaunch is a launch seam that panics with v.
func crashLaunch(v any) func(tui.Options, io.Writer) int {
	return func(o tui.Options, _ io.Writer) int {
		if o.Session != nil {
			o.Session.Cancel()
		}
		panic(v)
	}
}

func TestMainCrashLaunchPanic(t *testing.T) {
	dir := crashEnv(t)
	var stdout, stderr bytes.Buffer
	code := runWith(nil, &stdout, &stderr, deps{launch: crashLaunch("launch boom")})
	if code != 2 {
		t.Fatalf("code = %d, want 2 (stderr %q)", code, stderr.String())
	}
	log := readCrashLog(t, dir)
	for _, want := range []string{"panic: launch boom\n", "\nstack:\n", "crashLaunch", "version: " + version + "\n"} {
		if !strings.Contains(log, want) {
			t.Fatalf("crash.log lacks %q:\n%s", want, log)
		}
	}
	path := filepath.Join(dir, ".yipyip", "crash.log")
	if got, want := stderr.String(), "yip: crashed, details in "+path+"\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	if crashlog.Default() != nil {
		t.Fatalf("the default logger was not restored")
	}
}

func TestMainCrashMainPanic(t *testing.T) {
	dir := crashEnv(t)
	setTestPanic(t, "main")
	launched := false
	var stdout, stderr bytes.Buffer
	code := runWith(nil, &stdout, &stderr, deps{launch: func(tui.Options, io.Writer) int { launched = true; return 0 }})
	if code != 2 || launched {
		t.Fatalf("code = %d launched = %v, want 2 and no launch", code, launched)
	}
	log := readCrashLog(t, dir)
	if !strings.Contains(log, "panic: "+forcedPanicValue+"\n") || !strings.Contains(log, "forcePanic") {
		t.Fatalf("crash.log:\n%s", log)
	}
	if !strings.HasPrefix(stderr.String(), "yip: crashed, details in ") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestMainCrashLeakRedacted(t *testing.T) {
	dir := crashEnv(t)
	cfg := `{"default_model":"fake/m1","provider":{"fake":{"options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"{env:FAKE_KEY}"},"models":{"m1":{}}}}}`
	if err := os.WriteFile(config.GlobalPath(os.Getenv("HOME")), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_KEY", "SECRET123")
	setTestPanic(t, "leak")
	var stdout, stderr bytes.Buffer
	code := runWith(nil, &stdout, &stderr, deps{launch: launchUI})
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	log := readCrashLog(t, dir)
	if !strings.Contains(log, "panic: "+forcedPanicValue) {
		t.Fatalf("crash.log:\n%s", log)
	}
	if strings.Contains(log, "SECRET123") || strings.Contains(stderr.String(), "SECRET123") {
		t.Fatalf("secret leaked: log %q stderr %q", log, stderr.String())
	}
}

func TestMainCrashProgramPanicFallback(t *testing.T) {
	dir := crashEnv(t)
	runErr := fmt.Errorf("%w: %w", tea.ErrProgramKilled, tea.ErrProgramPanic)
	var stdout, stderr bytes.Buffer
	code := runWith(nil, &stdout, &stderr, deps{launch: func(o tui.Options, errw io.Writer) int {
		defer o.Session.Cancel()
		return launchWith(o, errw, func(tea.Model) error { return runErr })
	}})
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	log := readCrashLog(t, dir)
	if !strings.Contains(log, "panic: program panic caught by Bubble Tea (value unavailable): "+runErr.Error()) {
		t.Fatalf("crash.log:\n%s", log)
	}
	if strings.Count(log, "=== yip crash ") != 1 {
		t.Fatalf("want one record:\n%s", log)
	}
	if !strings.HasPrefix(stderr.String(), "yip: crashed, details in ") {
		t.Fatalf("stderr = %q", stderr.String())
	}

	// When the guard already recorded the panic, no second record is written.
	stderr.Reset()
	code = runWith(nil, &stdout, &stderr, deps{launch: func(o tui.Options, errw io.Writer) int {
		defer o.Session.Cancel()
		return launchWith(o, errw, func(tea.Model) error {
			recordGuardPanic("guard boom")
			return runErr
		})
	}})
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	log = readCrashLog(t, dir)
	if strings.Count(log, "=== yip crash ") != 2 || !strings.Contains(log, "panic: guard boom\n") {
		t.Fatalf("want the guard record only:\n%s", log)
	}
	if got := strings.Count(stderr.String(), "\n"); got != 1 || !strings.HasPrefix(stderr.String(), "yip: crashed, details in ") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestMainCrashWriteFailureStderr(t *testing.T) {
	dir := crashEnv(t)
	if err := os.WriteFile(filepath.Join(dir, ".yipyip"), []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	errFile, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = errFile
	t.Cleanup(func() { os.Stderr = oldStderr; errFile.Close() })

	var stdout, stderr bytes.Buffer
	code := runWith(nil, &stdout, &stderr, deps{launch: crashLaunch("write fail boom")})
	os.Stderr = oldStderr
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if got, want := stderr.String(), "yip: crashed (could not write the crash log)\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	rec, err := os.ReadFile(errFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rec), "panic: write fail boom\n") || !strings.Contains(string(rec), "stack:\n") {
		t.Fatalf("record not on stderr: %q", rec)
	}
}

func TestMainNoCrashNoLog(t *testing.T) {
	dir := crashEnv(t)
	var stdout, stderr bytes.Buffer
	if code := runWith([]string{"--version"}, &stdout, &stderr, deps{}); code != 0 {
		t.Fatalf("--version code = %d", code)
	}
	code := runWith(nil, &stdout, &stderr, deps{launch: func(o tui.Options, _ io.Writer) int {
		o.Session.Cancel()
		return 0
	}})
	if code != 0 {
		t.Fatalf("clean launch code = %d", code)
	}
	// A clean program run and a non-panic Run error keep today's behaviour.
	code = runWith(nil, &stdout, &stderr, deps{launch: func(o tui.Options, errw io.Writer) int {
		defer o.Session.Cancel()
		return launchWith(o, errw, func(tea.Model) error { return nil })
	}})
	if code != 0 {
		t.Fatalf("clean program code = %d", code)
	}
	stderr.Reset()
	code = runWith(nil, &stdout, &stderr, deps{launch: func(o tui.Options, errw io.Writer) int {
		defer o.Session.Cancel()
		return launchWith(o, errw, func(tea.Model) error { return errors.New("tty gone") })
	}})
	if code != 1 || stderr.String() != "yip: tty gone\n" {
		t.Fatalf("Run error: code = %d stderr = %q", code, stderr.String())
	}
	if log := readCrashLog(t, dir); log != "" {
		t.Fatalf("crash.log written without a crash:\n%s", log)
	}
}
