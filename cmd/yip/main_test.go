package main

import (
	"bytes"
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
