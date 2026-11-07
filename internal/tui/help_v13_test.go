package tui

import (
	"strings"
	"testing"
)

func TestHelpGlobalAgents(t *testing.T) {
	text := HelpText()
	if !strings.Contains(text, "~/.config/yipyip/AGENTS.md") {
		t.Errorf("HelpText() does not name ~/.config/yipyip/AGENTS.md:\n%s", text)
	}
	if !strings.Contains(text, "global") {
		t.Errorf("HelpText() does not describe the file as global:\n%s", text)
	}
	instr := strings.Index(text, "Instructions:\n")
	exit := strings.Index(text, "On exit:")
	if instr < 0 {
		t.Fatalf("HelpText() missing Instructions: block:\n%s", text)
	}
	if exit < 0 {
		t.Fatalf("HelpText() missing On exit: block:\n%s", text)
	}
	if instr > exit {
		t.Errorf("Instructions: block does not come before On exit:")
	}
}
