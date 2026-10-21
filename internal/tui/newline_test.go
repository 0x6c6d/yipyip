package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// shiftEnter is Shift+Enter. bubbletea v1 has no key type for it, so the key
// is built with the name bubbletea reports for it (the way namedKey does).
func shiftEnter() tea.KeyMsg { return namedKey("shift+enter") }

func TestShiftEnterInsertsNewline(t *testing.T) {
	var m tea.Model = New(Options{StartDir: "/work"})
	m = typeText(m, "a")
	m, cmd := m.Update(shiftEnter())
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("Shift+Enter quit the program")
		}
	}
	got := m.(Model).input.Value()
	if got != "a\n" {
		t.Fatalf("input after Shift+Enter = %q, want %q", got, "a\n")
	}
}

func TestAltEnterUnchanged(t *testing.T) {
	var m tea.Model = New(Options{StartDir: "/work"})
	m = typeText(m, "a")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	mm := m.(Model)
	if got := mm.input.Value(); got != "a\n" {
		t.Fatalf("input after Alt+Enter = %q, want %q", got, "a\n")
	}
	if len(mm.blocks) != 0 {
		t.Fatalf("Alt+Enter submitted the input (%d blocks)", len(mm.blocks))
	}
}
