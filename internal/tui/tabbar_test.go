package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"yipyip/internal/agent"
)

// belowTabBar returns the view without its first line (the tab bar).
func belowTabBar(v string) string {
	_, rest, _ := strings.Cut(v, "\n")
	return rest
}

// tabBarLine returns the first line of the plain view, trailing spaces cut.
func tabBarLine(m Model) string {
	first, _, _ := strings.Cut(plainView(m), "\n")
	return strings.TrimRight(first, " ")
}

// sendKey sends one key to the model.
func sendKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	next, cmd := m.Update(k)
	return next.(Model), cmd
}

// namedKey is a key whose String() is name, for keys bubbletea has no key
// type for (ctrl+tab, ctrl+shift+tab).
func namedKey(name string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

func countInfo(m Model, text string) int {
	n := 0
	for _, b := range m.blocks {
		if b.kind == blockInfo && b.text == text {
			n++
		}
	}
	return n
}

func TestTabBarRendersSegments(t *testing.T) {
	withColorProfile(t, termenv.ANSI256)
	m, _ := newSessionModel(t, answerServer(t, "hello"))
	m = tabTurn(t, m, "first question")
	mustAddTab(t, &m)

	if got, want := tabBarLine(m), "1:first question   × 2:new session      × +"; got != want {
		t.Fatalf("tab bar = %q, want %q", got, want)
	}
	first, _, _ := strings.Cut(m.View(), "\n")
	if want := "\x1b[38;5;244m1:first question   ×\x1b[0m"; !strings.Contains(first, want) {
		t.Errorf("first segment not dim: %q", first)
	}
	if want := "\x1b[1m2:new session      ×\x1b[0m"; !strings.Contains(first, want) {
		t.Errorf("current segment not bold: %q", first)
	}
	if strings.Contains(first, "\x1b[1m1:") || strings.Contains(first, "244m2:") {
		t.Errorf("styles on the wrong segments: %q", first)
	}
}

func TestCtrlTOpensEightTabsThenLimit(t *testing.T) {
	m, sess := newSessionModel(t, nil)
	for i := 0; i < 7; i++ {
		var cmd tea.Cmd
		m, cmd = sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlT})
		if cmd == nil {
			t.Fatalf("press %d: no event command for the new tab", i+1)
		}
	}
	if len(m.tabs) != 8 || m.cur != 7 {
		t.Fatalf("tabs = %d, cur = %d, want 8 and 7", len(m.tabs), m.cur)
	}
	if got := strings.Count(tabBarLine(m), "new session"); got != 8 {
		t.Fatalf("tab bar has %d segments, want 8: %q", got, tabBarLine(m))
	}
	for press := 1; press <= 2; press++ {
		before := len(m.blocks)
		m, _ = sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlT})
		if len(m.tabs) != 8 || m.cur != 7 {
			t.Fatalf("press %d at the limit: tabs = %d, cur = %d", press, len(m.tabs), m.cur)
		}
		if len(m.blocks) != before+1 || countInfo(m, tabLimitMsg) != press {
			t.Fatalf("press %d at the limit: %d blocks (was %d), %d limit lines", press, len(m.blocks), before, countInfo(m, tabLimitMsg))
		}
	}
	if n := len(sessionFiles(t, sess.StartDir())); n != 0 {
		t.Errorf("%d session files written, want none", n)
	}
}

func TestCtrlWSavesAndSwitchesLeftNeighbour(t *testing.T) {
	m, sess := newSessionModelOpts(t, answerServer(t, "hello"), "start", noAutosave)
	mustAddTab(t, &m)
	mustAddTab(t, &m)
	m = tabTurn(t, m, "closing question")
	if got, want := tabBarLine(m), "1:new session      × 2:new session      × 3:closing question × +"; got != want {
		t.Fatalf("tab bar = %q, want %q", got, want)
	}
	if n := len(sessionFiles(t, sess.StartDir())); n != 0 {
		t.Fatalf("%d session files before the close", n)
	}

	m, cmd := sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlW})
	if cmd != nil {
		t.Errorf("closing a non-last tab returned a command")
	}
	if n := len(sessionFiles(t, sess.StartDir())); n != 1 {
		t.Fatalf("%d session files after the close, want 1", n)
	}
	if len(m.tabs) != 2 || m.cur != 1 {
		t.Fatalf("tabs = %d, cur = %d, want 2 and 1 (left neighbour)", len(m.tabs), m.cur)
	}
	got := tabBarLine(m)
	if got != "1:new session      × 2:new session      × +" || strings.Contains(got, "closing question") {
		t.Fatalf("tab bar after close = %q", got)
	}

	// Closing the first tab falls back to the right neighbour.
	m.switchTab(0)
	m, _ = sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlW})
	if len(m.tabs) != 1 || m.cur != 0 {
		t.Fatalf("tabs = %d, cur = %d, want 1 and 0", len(m.tabs), m.cur)
	}
}

func TestCtrlWLastTabLeavesFreshTab(t *testing.T) {
	m, sess := newSessionModelOpts(t, answerServer(t, "hello"), "start", noAutosave)
	m = tabTurn(t, m, "only question")

	m, cmd := sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlW})
	checkFreshLastTab(t, m, cmd, sess)
	if got := tabBarLine(m); strings.Contains(got, "only question") {
		t.Errorf("tab bar after the close = %q", got)
	}
}

func TestTabKeys(t *testing.T) {
	m, _ := newSessionModel(t, nil)
	mustAddTab(t, &m)
	mustAddTab(t, &m)
	m.switchTab(1)
	m.input.SetValue("abc")
	blocks := len(m.blocks)
	for _, k := range []tea.KeyMsg{
		namedKey("ctrl+tab"), namedKey("ctrl+shift+tab"),
		{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true},
		{Type: tea.KeyRunes, Runes: []rune{'3'}, Alt: true},
	} {
		m, _ = sendKey(m, k)
		if m.cur != 1 || len(m.tabs) != 3 || m.input.Value() != "abc" || len(m.blocks) != blocks {
			t.Fatalf("%q: cur = %d, tabs = %d, input = %q, blocks = %d (was %d)",
				k.String(), m.cur, len(m.tabs), m.input.Value(), len(m.blocks), blocks)
		}
	}
	m, _ = sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if len(m.tabs) != 4 || m.cur != 3 {
		t.Fatalf("Ctrl+T: tabs = %d, cur = %d, want 4 and 3", len(m.tabs), m.cur)
	}
	m, _ = sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlW})
	if len(m.tabs) != 3 {
		t.Fatalf("Ctrl+W: tabs = %d, want 3", len(m.tabs))
	}
	before := m.sess.Mode()
	m, _ = sendKey(m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.sess.Mode() == before {
		t.Errorf("Shift+Tab left the mode at %s", before)
	}
	help := HelpText()
	for _, want := range []string{"Ctrl+T", "Ctrl+W"} {
		if !strings.Contains(help, want) {
			t.Errorf("help lacks %q", want)
		}
	}
	for _, bad := range []string{"Ctrl+Tab", "Alt+1"} {
		if strings.Contains(help, bad) {
			t.Errorf("help still lists %q", bad)
		}
	}
}

func TestTabKeyStillCompletes(t *testing.T) {
	m := suggestSet(New(Options{StartDir: "/w"}), "/effort m")
	m, _ = sendKey(m, tea.KeyMsg{Type: tea.KeyTab})
	if got := m.input.Value(); got != "/effort medium" {
		t.Fatalf("Tab: input = %q", got)
	}
	if len(m.tabs) != 1 || m.cur != 0 {
		t.Fatalf("Tab changed tabs: %d, cur %d", len(m.tabs), m.cur)
	}

	sm, _ := newSessionModel(t, nil)
	if got := sm.sess.Mode(); got != agent.ModeManual {
		t.Fatalf("start mode = %q, want manual", got)
	}
	sm, _ = sendKey(sm, tea.KeyMsg{Type: tea.KeyShiftTab})
	if got := sm.sess.Mode(); got != agent.ModeAuto {
		t.Fatalf("Shift+Tab: mode = %q, want auto", got)
	}
}

// A single fresh tab still shows its one segment, and the bar takes one of
// the window's lines.
func TestTabBarNotShownWithOneFreshTab(t *testing.T) {
	m, _ := newSessionModel(t, nil)
	if got, want := tabBarLine(m), "1:new session      × +"; got != want {
		t.Fatalf("tab bar = %q, want %q", got, want)
	}
	if h := lipgloss.Height(m.View()); h != 40 {
		t.Errorf("view height = %d, want the window height 40", h)
	}
	mustAddTab(t, &m)
	if h := lipgloss.Height(m.View()); h != 40 {
		t.Errorf("view height with 2 tabs = %d, want 40", h)
	}
	if got := ansi.StringWidth(strings.Split(m.View(), "\n")[0]); got > 160 {
		t.Errorf("tab bar is %d columns wide", got)
	}
}
