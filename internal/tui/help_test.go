package tui

import (
	"strings"
	"testing"
)

func TestHelpTextListsCommands(t *testing.T) {
	want := []string{"/model", "/effort", "/mode", "/agents", "/init", "/compact",
		"/undo", "/sessions", "/new", "/clear", "/exit", "/quit", "/help"}
	cmds := Commands()
	if len(cmds) != len(want) {
		t.Fatalf("Commands() has %d entries, want %d", len(cmds), len(want))
	}
	text := HelpText()
	for i, name := range want {
		if cmds[i].Name != name {
			t.Errorf("Commands()[%d].Name = %q, want %q", i, cmds[i].Name, name)
		}
		if !strings.Contains(text, name) {
			t.Errorf("HelpText() does not list %q", name)
		}
	}
}

func TestHelpTextNoCost(t *testing.T) {
	lower := strings.ToLower(HelpText())
	for _, bad := range []string{"cost", "price"} {
		if strings.Contains(lower, bad) {
			t.Errorf("HelpText() contains %q", bad)
		}
	}
	if strings.Contains(HelpText(), "$") {
		t.Errorf("HelpText() contains '$'")
	}
}

func TestHelpTextListsNewKeys(t *testing.T) {
	text := HelpText()
	for _, want := range []string{"Up/Down", "Ctrl+Home", "Ctrl+End", "Tab", "Mouse drag",
		"copies", "queue", "restores queued", "auto is the default"} {
		if !strings.Contains(text, want) {
			t.Errorf("HelpText() does not contain %q", want)
		}
	}
}

func TestHelpMentionsShiftEnter(t *testing.T) {
	text := HelpText()
	for _, want := range []string{"Alt+Enter", "Shift+Enter"} {
		if !strings.Contains(text, want) {
			t.Errorf("HelpText() does not contain %q", want)
		}
	}
}

func TestHelpListsNewCommands(t *testing.T) {
	text := HelpText()
	for _, want := range []string{"/sessions", "Ctrl+T", "Ctrl+W", "Alt+1", "Shift+Enter"} {
		if !strings.Contains(text, want) {
			t.Errorf("HelpText() does not contain %q", want)
		}
	}
}

func TestHelpStillListsOldEntries(t *testing.T) {
	text := HelpText()
	for _, want := range []string{"/model", "/compact", "Alt+Enter"} {
		if !strings.Contains(text, want) {
			t.Errorf("HelpText() does not contain %q", want)
		}
	}
}

func TestHelpShiftEnterOneLine(t *testing.T) {
	text := HelpText()
	if !strings.Contains(text, "Alt+Enter") {
		t.Fatal("HelpText() does not contain Alt+Enter")
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "Alt+Enter") && strings.Contains(line, "Shift+Enter") {
			return
		}
	}
	t.Error("no help line contains both Alt+Enter and Shift+Enter")
}
