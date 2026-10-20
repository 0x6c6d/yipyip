package tui

import (
	"strings"
	"testing"
)

func TestHelpTextListsCommands(t *testing.T) {
	want := []string{"/model", "/effort", "/mode", "/agents", "/init", "/compact",
		"/undo", "/new", "/clear", "/exit", "/quit", "/help"}
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
