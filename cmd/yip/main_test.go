package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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
