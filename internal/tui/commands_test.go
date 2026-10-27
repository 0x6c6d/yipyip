package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

func TestHelpCommand(t *testing.T) {
	m, _ := newSessionModel(t, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 60})
	m = next.(Model)
	m, _ = m.Command("/help")
	v := m.View()
	for _, c := range Commands() {
		if !strings.Contains(v, c.Name) {
			t.Errorf("view lacks %s:\n%s", c.Name, v)
		}
	}
}

func TestNewClearsSession(t *testing.T) {
	for _, name := range []string{"/new"} {
		srv := providertest.New(providertest.Response{
			Text:  "an answer to forget",
			Usage: &providertest.Usage{Prompt: 10, Completion: 5},
		})
		m, sess := newSessionModel(t, srv)
		m, _ = m.Command("remember this question")
		waitSession(t, sess)
		m = m.Drain()
		if n := len(sess.History()); n < 3 {
			t.Fatalf("%s: history = %d messages before reset", name, n)
		}
		if sess.Tokens().Total == 0 {
			t.Fatalf("%s: no tokens before reset", name)
		}
		// v3: the fresh session opens in a new tab; the old tab keeps its
		// conversation.
		m, cmd := m.Command(name)
		if cmd == nil {
			t.Errorf("%s returned no event command for the new tab", name)
		}
		if len(m.tabs) != 2 || m.cur != 1 || m.sess == sess {
			t.Fatalf("%s: tabs = %d, cur = %d, want a new current tab", name, len(m.tabs), m.cur)
		}
		if n := len(m.sess.History()); n != 1 {
			t.Errorf("%s: history = %d messages, want 1", name, n)
		}
		if tok := m.sess.Tokens(); tok.Total != 0 || tok.In != 0 || tok.Out != 0 {
			t.Errorf("%s: tokens = %+v, want zero", name, tok)
		}
		if n := len(sess.History()); n < 3 {
			t.Errorf("%s: old tab history = %d messages, want it kept", name, n)
		}
		v := belowTabBar(m.View())
		if strings.Contains(v, "remember this question") || strings.Contains(v, "an answer to forget") {
			t.Errorf("%s: view not cleared:\n%s", name, v)
		}
		if !strings.Contains(v, "new session") {
			t.Errorf("%s: view lacks 'new session':\n%s", name, v)
		}
		srv.Close()
	}
	// Without a session the view is just cleared.
	m := New(Options{StartDir: "/work"})
	m, _ = m.Command("hello")
	m, _ = m.Command("/new")
	if v := m.View(); strings.Contains(v, "> hello") || !strings.Contains(v, "new session") {
		t.Errorf("nil session view:\n%s", v)
	}
}

func TestExitQuit(t *testing.T) {
	m, _ := newSessionModel(t, nil)
	for _, line := range []string{"/exit", "/quit"} {
		_, cmd := m.Command(line)
		if cmd == nil {
			t.Fatalf("%s returned no command", line)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s did not quit", line)
		}
	}
}

func TestEffortCommand(t *testing.T) {
	m, sess := newSessionModel(t, nil)
	m, _ = m.Command("/effort")
	if v := m.View(); !strings.Contains(v, "effort: medium") {
		t.Fatalf("show:\n%s", v)
	}
	m, _ = m.Command("/effort high")
	if sess.Effort() != "high" {
		t.Fatalf("effort = %q, want high", sess.Effort())
	}
	if v := m.View(); !strings.Contains(v, "effort: high") {
		t.Fatalf("set:\n%s", v)
	}
	m, _ = m.Command("/effort extreme")
	if sess.Effort() != "high" {
		t.Fatalf("invalid effort changed it to %q", sess.Effort())
	}
	if v := m.View(); !strings.Contains(v, "error: effort must be low, medium or high") {
		t.Fatalf("invalid:\n%s", v)
	}
	m, _ = m.Command("/effort low")
	if sess.Effort() != "low" || !strings.Contains(m.StatusLine(), "low") {
		t.Fatalf("effort = %q, status %q", sess.Effort(), m.StatusLine())
	}
}

func TestModeCommand(t *testing.T) {
	m, sess := newSessionModel(t, nil)
	m, _ = m.Command("/mode")
	if sess.Mode() != agent.ModeAuto || !strings.Contains(m.View(), "mode: auto") {
		t.Fatalf("toggle: mode %q\n%s", sess.Mode(), m.View())
	}
	m, _ = m.Command("/mode manual")
	if sess.Mode() != agent.ModeManual || !strings.Contains(m.View(), "mode: manual") {
		t.Fatalf("manual: mode %q", sess.Mode())
	}
	m, _ = m.Command("/mode auto")
	if sess.Mode() != agent.ModeAuto {
		t.Fatalf("auto: mode %q", sess.Mode())
	}
	m, _ = m.Command("/mode bogus")
	if sess.Mode() != agent.ModeAuto {
		t.Fatalf("invalid mode changed it to %q", sess.Mode())
	}
	if v := m.View(); !strings.Contains(v, "error: mode must be auto or manual") {
		t.Fatalf("invalid:\n%s", v)
	}
}

func TestShiftTabTogglesMode(t *testing.T) {
	m, sess := newSessionModel(t, nil)
	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if sess.Mode() != agent.ModeAuto || !strings.Contains(tm.(Model).StatusLine(), "auto") {
		t.Fatalf("after one Shift+Tab: mode %q, status %q", sess.Mode(), tm.(Model).StatusLine())
	}
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if sess.Mode() != agent.ModeManual || !strings.Contains(tm.(Model).StatusLine(), "manual") {
		t.Fatalf("after two Shift+Tab: mode %q", sess.Mode())
	}
}

func TestUnknownCommand(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	for _, line := range []string{"/foo", "/foo bar baz", "/", "/Help", "/effort extreme"} {
		var cmd tea.Cmd
		m, cmd = m.Command(line)
		if cmd != nil {
			t.Errorf("%q returned a command", line)
		}
	}
	v := m.View()
	for _, want := range []string{"unknown command: /foo", "unknown command: /Help", "error: effort must be"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if strings.Count(v, "unknown command: /") != 4 {
		t.Errorf("want 4 unknown-command lines:\n%s", v)
	}
	if sess.Busy() {
		t.Fatal("a slash command started a turn")
	}
	waitSession(t, sess)
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("slash commands reached the provider: %d requests", n)
	}
}

func TestCompletion(t *testing.T) {
	m, _ := newSessionModel(t, nil)
	var tm tea.Model = m
	tm = typeText(tm, "/mo")
	v := tm.View()
	if !strings.Contains(v, "/model /mode") {
		t.Fatalf("completion line missing:\n%s", v)
	}
	if strings.Contains(v, "/model /mode /") {
		t.Fatalf("completion shows more than /mo matches:\n%s", v)
	}

	// Tab on an ambiguous prefix completes the longest common prefix only.
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := tm.(Model).input.Value(); got != "/mode" {
		t.Fatalf("Tab on /mo: input = %q", got)
	}

	tm = New(Options{StartDir: "/w"})
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm = typeText(tm, "/he")
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := tm.(Model).input.Value(); got != "/help" {
		t.Fatalf("Tab on /he: input = %q, want /help", got)
	}

	// Common prefix: /e matches /effort /exit; /ex-> /exit.
	tm = New(Options{StartDir: "/w"})
	tm = typeText(tm, "/e")
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := tm.(Model).input.Value(); got != "/e" {
		t.Fatalf("Tab on /e: input = %q", got)
	}
	tm = typeText(tm, "x")
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := tm.(Model).input.Value(); got != "/exit" {
		t.Fatalf("Tab on /ex: input = %q", got)
	}

	// Hidden with a space or without a slash.
	tm = typeText(New(Options{StartDir: "/w"}), "/mode a")
	if strings.Contains(tm.View(), "/model /mode") {
		t.Errorf("completion shown after a space:\n%s", tm.View())
	}
	tm = typeText(New(Options{StartDir: "/w"}), "mo")
	if strings.Contains(tm.View(), "/model /mode") {
		t.Errorf("completion shown without a slash:\n%s", tm.View())
	}
}
