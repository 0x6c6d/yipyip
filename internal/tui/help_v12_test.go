package tui

import (
	"strings"
	"testing"
)

func TestHelpExitCleanup(t *testing.T) {
	text := HelpText()
	const line = "processes started by commands (also background jobs and servers) are terminated after a 2 s grace"
	if !strings.Contains(text, line) {
		t.Errorf("HelpText() missing exit cleanup line %q", line)
	}
	if !strings.Contains(text, "On exit:") {
		t.Errorf("HelpText() missing heading %q", "On exit:")
	}
}
