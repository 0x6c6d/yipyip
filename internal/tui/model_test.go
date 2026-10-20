package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeText(m tea.Model, s string) tea.Model {
	for _, r := range s {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestSkeletonExit(t *testing.T) {
	for _, cmdText := range []string{"/exit", "/quit"} {
		var m tea.Model = New(Options{StartDir: "/work"})
		m = typeText(m, cmdText)
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatalf("%s: Enter returned no command", cmdText)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s: command message is not tea.QuitMsg", cmdText)
		}
	}
}

func TestSkeletonStatusLine(t *testing.T) {
	m := New(Options{StartDir: "/work/dir"})
	want := "/work/dir · no model · medium · manual · in 0 / out 0 / total 0 · ctx 0"
	if got := m.StatusLine(); got != want {
		t.Fatalf("StatusLine() = %q, want %q", got, want)
	}
	if !strings.Contains(m.View(), want) {
		t.Fatalf("View() does not contain the status line:\n%s", m.View())
	}
	for _, bad := range []string{"cost", "price", "$"} {
		if strings.Contains(strings.ToLower(m.View()), bad) {
			t.Errorf("View() contains %q", bad)
		}
	}
	m2 := New(Options{StartDir: "/x", ModelID: "p/m", Effort: "high", Mode: "auto"})
	if got := m2.StatusLine(); got != "/x · p/m · high · auto · in 0 / out 0 / total 0 · ctx 0" {
		t.Fatalf("StatusLine() = %q", got)
	}
}

func TestSkeletonEcho(t *testing.T) {
	var m tea.Model = New(Options{StartDir: "/work"})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = typeText(m, "hello world")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("ordinary text must not quit")
		}
	}
	v := m.View()
	if !strings.Contains(v, "> hello world") {
		t.Fatalf("View() missing echoed line:\n%s", v)
	}
	if !strings.Contains(v, "no model configured") {
		t.Fatalf("View() missing 'no model configured':\n%s", v)
	}
	if got := m.(Model).input.Value(); got != "" {
		t.Fatalf("input not cleared: %q", got)
	}
}

func TestSkeletonAltEnterNewline(t *testing.T) {
	var m tea.Model = New(Options{StartDir: "/work"})
	m = typeText(m, "a")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = typeText(m, "b")
	if got := m.(Model).input.Value(); got != "a\nb" {
		t.Fatalf("input = %q, want %q", got, "a\nb")
	}
}
