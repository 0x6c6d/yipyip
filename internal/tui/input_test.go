package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"yipyip/internal/provider/providertest"
)

func suggestSet(m Model, v string) Model {
	m.input.SetValue(v)
	m.input.CursorEnd()
	return m
}

func suggestServer(t *testing.T) *providertest.Server {
	t.Helper()
	srv := providertest.New()
	t.Cleanup(srv.Close)
	return srv
}

func TestSuggestCommand(t *testing.T) {
	m := suggestSet(New(Options{StartDir: "/w"}), "/eff")
	ghost, accept := m.suggestion()
	if ghost != "ort" || accept != "/effort" {
		t.Fatalf("ghost, accept = %q, %q", ghost, accept)
	}
	if g, _ := suggestSet(m, "/effort").suggestion(); g != "" {
		t.Fatalf("exact match has ghost %q", g)
	}
}

func TestSuggestEffortHint(t *testing.T) {
	m := New(Options{StartDir: "/w"})
	for in, want := range map[string]string{
		"/effort ": "[low|medium|high]",
		"/mode ":   "[auto|manual]",
		"/model ":  "[provider/model]",
	} {
		ghost, accept := suggestSet(m, in).suggestion()
		if ghost != want || accept != "" {
			t.Errorf("%q: ghost, accept = %q, %q; want %q, \"\"", in, ghost, accept, want)
		}
	}
}

func TestSuggestEffortPrefix(t *testing.T) {
	m := suggestSet(New(Options{StartDir: "/w"}), "/effort m")
	ghost, accept := m.suggestion()
	if ghost != "edium" || accept != "/effort medium" {
		t.Fatalf("ghost, accept = %q, %q", ghost, accept)
	}
	for _, in := range []string{"/effort medium", "/effort x", "/mode a"} {
		g, _ := suggestSet(m, in).suggestion()
		if in == "/mode a" {
			if g != "uto" {
				t.Errorf("%q: ghost %q", in, g)
			}
		} else if g != "" {
			t.Errorf("%q: ghost %q, want none", in, g)
		}
	}
}

func TestSuggestModelIDs(t *testing.T) {
	srv := suggestServer(t)
	m, sess := newSessionModel(t, srv)
	ids := sess.Models()
	if len(ids) == 0 {
		t.Fatal("no model ids configured")
	}
	first := ids[0]
	m = suggestSet(m, "/model "+first[:1])
	ghost, accept := m.suggestion()
	if ghost != first[1:] || accept != "/model "+first {
		t.Fatalf("ghost, accept = %q, %q; want %q", ghost, accept, first[1:])
	}
	if g, _ := suggestSet(New(Options{StartDir: "/w"}), "/model f").suggestion(); g != "" {
		t.Fatalf("model ghost without session: %q", g)
	}
}

func TestSuggestTabAccepts(t *testing.T) {
	var tm tea.Model = suggestSet(New(Options{StartDir: "/w"}), "/effort m")
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := tm.(Model).input.Value(); got != "/effort medium" {
		t.Fatalf("Tab: input = %q", got)
	}
	if g, _ := tm.(Model).suggestion(); g != "" {
		t.Fatalf("ghost after accept: %q", g)
	}

	tm = suggestSet(New(Options{StartDir: "/w"}), "/eff")
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := tm.(Model).input.Value(); got != "/effort" {
		t.Fatalf("Right: input = %q", got)
	}
}

func TestSuggestHintNotAccepted(t *testing.T) {
	for _, k := range []tea.KeyType{tea.KeyTab, tea.KeyRight} {
		var tm tea.Model = suggestSet(New(Options{StartDir: "/w"}), "/effort ")
		tm, _ = tm.Update(tea.KeyMsg{Type: k})
		if got := tm.(Model).input.Value(); got != "/effort " {
			t.Fatalf("key %v: input = %q", k, got)
		}
	}
}

func TestSuggestNoGhost(t *testing.T) {
	base := New(Options{StartDir: "/w"})
	if g, a := suggestSet(base, "/eff\nx").suggestion(); g != "" || a != "" {
		t.Errorf("multi-line: %q %q", g, a)
	}
	m := suggestSet(base, "/eff")
	m.input.SetCursor(2)
	if g, a := m.suggestion(); g != "" || a != "" {
		t.Errorf("cursor not at end: %q %q", g, a)
	}
	if g, a := suggestSet(base, "eff").suggestion(); g != "" || a != "" {
		t.Errorf("no slash: %q %q", g, a)
	}
	pm := suggestSet(base, "/eff")
	pm.picker.open = true
	if g, _ := pm.suggestion(); g != "" {
		t.Errorf("picker open: %q", g)
	}
}

func TestSuggestColor(t *testing.T) {
	withColorProfile(t, termenv.ANSI256)
	m := suggestSet(New(Options{StartDir: "/w"}), "/eff")
	m.resize(80, 24)
	want := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("ort")
	if v := m.View(); !strings.Contains(v, want) {
		t.Fatalf("ghost %q missing:\n%q", want, v)
	}
	if v := m.input.Value(); v != "/eff" {
		t.Fatalf("ghost leaked into the value: %q", v)
	}
}

func TestSuggestNeverSent(t *testing.T) {
	srv := suggestServer(t)
	m, sess := newSessionModel(t, srv)
	m = suggestSet(m, "hello")
	if g, _ := m.suggestion(); g != "" {
		t.Fatalf("ghost on plain text: %q", g)
	}
	m = suggestSet(m, "/eff")
	if g, _ := m.suggestion(); g != "ort" {
		t.Fatalf("ghost = %q", g)
	}
	histLen := len(sess.History())
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if v := ansi.Strip(tm.View()); !strings.Contains(v, "unknown command: /eff") || strings.Contains(v, "unknown command: /effort") {
		t.Fatalf("Enter did not run /eff:\n%s", v)
	}
	if h := sess.History(); len(h) != histLen {
		t.Fatalf("history changed: %d -> %d", histLen, len(h))
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("provider got %d requests", n)
	}
	for _, msg := range sess.History()[1:] {
		if strings.Contains(msg.Content, "ort") {
			t.Fatalf("history contains the ghost: %q", msg.Content)
		}
	}
}
