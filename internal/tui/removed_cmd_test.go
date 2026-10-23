package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/provider/providertest"
)

// TestUnknownCommandRemoved checks that the removed turn-routing command is an
// ordinary unknown command: it is not listed, not shown and never reaches the
// provider.
func TestUnknownCommandRemoved(t *testing.T) {
	old := "\x6aev"
	name := "/" + old
	want := "unknown command: " + name

	srv := providertest.New()
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	var tm tea.Model = m

	for i, line := range []string{name, name + " stats"} {
		tm = typeText(tm, line)
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
		v := ansi.Strip(tm.View())
		if n := strings.Count(v, want); n != i+1 {
			t.Fatalf("%q: view has %d lines %q, want %d:\n%s", line, n, want, i+1, v)
		}
		if strings.Contains(v, want+" stats") {
			t.Fatalf("%q: unknown-command line includes the argument:\n%s", line, v)
		}
		if sess.Busy() {
			t.Fatalf("%q started a turn", line)
		}
		waitSession(t, sess)
		if n := len(srv.Requests()); n != 0 {
			t.Fatalf("%q reached the provider: %d requests", line, n)
		}
	}

	for _, c := range Commands() {
		if c.Name == name {
			t.Fatalf("Commands() still lists %q", name)
		}
	}
	if strings.Contains(strings.ToLower(HelpText()), old) {
		t.Fatalf("HelpText() mentions %q:\n%s", old, HelpText())
	}

	status := ansi.Strip(tm.(Model).StatusLine())
	view := ansi.Strip(tm.View())
	for _, bad := range []string{old, "route:"} {
		if strings.Contains(strings.ToLower(status), bad) {
			t.Errorf("status line contains %q: %q", bad, status)
		}
	}
	// The view holds the two unknown-command lines; outside them the
	// old name must not appear.
	rest := strings.ReplaceAll(view, want, "")
	for _, bad := range []string{old, "route:"} {
		if strings.Contains(strings.ToLower(rest), bad) {
			t.Errorf("view contains %q outside the unknown-command lines:\n%s", bad, view)
		}
	}
}
