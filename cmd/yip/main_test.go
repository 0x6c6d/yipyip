package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/agent"
	"yipyip/internal/config"
)

func TestVersionFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(--version) = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if !regexp.MustCompile(`^yip `).MatchString(stdout.String()) {
		t.Fatalf("stdout = %q, want prefix %q", stdout.String(), "yip ")
	}
}

func TestHelpFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(--help) = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	for _, c := range []string{"/model", "/effort", "/mode", "/agents", "/init", "/compact",
		"/undo", "/new", "/clear", "/exit", "/quit", "/help"} {
		if !strings.Contains(stdout.String(), c) {
			t.Errorf("help output does not contain %q", c)
		}
	}
}

func TestHelpFlagNewKeys(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(--help) = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	for _, want := range []string{"Up/Down", "Ctrl+Home", "Ctrl+End", "Tab", "queue"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help output does not contain %q", want)
		}
	}
}

func TestUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run(--bogus) = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage") {
		t.Fatalf("stderr = %q, want it to mention usage", stderr.String())
	}
}

func TestConfigErrorExitCode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "yipyip.json"), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run(nil) = %d, want 1 (stderr: %q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "yipyip.json") {
		t.Fatalf("stderr = %q, want it to name yipyip.json", stderr.String())
	}
}

func TestNewModelStartsAuto(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	for _, d := range []string{filepath.Dir(config.GlobalPath(home)), dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(config.GlobalPath(home), []byte(`{"provider": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(home, dir, func(string) string { return "" })
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	sess, err := agent.NewSession(agent.Options{Config: cfg, StartDir: dir})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(sess.Cancel)
	m := newModel(dir, sess)
	if sess.Mode() != agent.ModeAuto {
		t.Fatalf("mode = %q, want %q", sess.Mode(), agent.ModeAuto)
	}
	if line := ansi.Strip(m.StatusLine()); !strings.Contains(line, "auto (shift+tab to toggle)") {
		t.Errorf("status line = %q, want it to show auto mode", line)
	}
}
