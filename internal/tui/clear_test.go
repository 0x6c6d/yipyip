package tui

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/provider/providertest"
)

// clearEnter types /clear into the input box and sends it with Enter.
func clearEnter(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	m.input.SetValue("/clear")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return next.(Model), cmd
}

func TestClearKeepsTab(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "old answer"))
	m = tabTurn(t, m, "old question")
	if !strings.Contains(belowTabBar(plainView(m)), "old answer") {
		t.Fatal("the answer is not shown before /clear")
	}
	m, cmd := clearEnter(t, m)
	if cmd == nil {
		t.Error("/clear returned no event command for the fresh session")
	}
	if len(m.tabs) != 1 || m.cur != 0 {
		t.Fatalf("tabs = %d, cur = %d, want 1 and 0", len(m.tabs), m.cur)
	}
	if m.sess == nil || m.sess == sess {
		t.Fatal("/clear did not give the tab a new session")
	}
	if len(m.blocks) != 1 || countInfo(m, "cleared") != 1 {
		t.Errorf("blocks = %d, cleared = %d, want exactly the one info line", len(m.blocks), countInfo(m, "cleared"))
	}
	v := belowTabBar(plainView(m))
	if strings.Contains(v, "old answer") || strings.Contains(v, "old question") {
		t.Errorf("view not cleared:\n%s", v)
	}
	if !strings.Contains(v, "cleared") {
		t.Errorf("view lacks 'cleared':\n%s", v)
	}
	if n := len(m.sess.History()); n != 1 {
		t.Errorf("fresh history = %d messages, want 1", n)
	}
}

func TestClearWithTwoTabs(t *testing.T) {
	m, first := newSessionModel(t, answerServer(t, "first answer"))
	m = tabTurn(t, m, "first question")
	mustAddTab(t, &m)
	second := m.sess
	m = tabTurn(t, m, "second question")
	firstTab := m.tabs[0]
	firstBlocks := len(firstTab.blocks)

	m, _ = clearEnter(t, m)
	if len(m.tabs) != 2 || m.cur != 1 {
		t.Fatalf("tabs = %d, cur = %d, want 2 and 1", len(m.tabs), m.cur)
	}
	if m.sess == second || m.sess == first {
		t.Fatal("tab 1 did not get a new session")
	}
	if m.tabs[0].sess != first || m.tabs[0].hist != firstTab.hist || len(m.tabs[0].blocks) != firstBlocks {
		t.Error("tab 0 was changed by /clear on tab 1")
	}
	if n := len(first.History()); n < 3 {
		t.Errorf("tab 0 history = %d messages, want it kept", n)
	}
	if len(m.blocks) != 1 || countInfo(m, "cleared") != 1 {
		t.Errorf("blocks = %d, cleared = %d", len(m.blocks), countInfo(m, "cleared"))
	}
	m.switchTab(0)
	if !strings.Contains(belowTabBar(plainView(m)), "first answer") {
		t.Error("tab 0 lost its conversation")
	}
}

func TestClearWhileBusy(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "late", Delay: 30 * time.Second})
	t.Cleanup(srv.Close)
	m, sess := newSessionModel(t, srv)
	m.input.SetValue("slow")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	waitBusy(t, sess)

	m, cmd := clearEnter(t, m)
	waitSession(t, sess)
	if cmd == nil {
		t.Error("/clear returned no event command")
	}
	if len(m.tabs) != 1 || m.sess == sess {
		t.Fatalf("tabs = %d, same session %v", len(m.tabs), m.sess == sess)
	}
	if len(m.blocks) != 1 || countInfo(m, "cleared") != 1 {
		t.Errorf("blocks = %d, cleared = %d", len(m.blocks), countInfo(m, "cleared"))
	}
	if len(m.queue) != 0 {
		t.Errorf("queue = %q, want /clear not queued", m.queue)
	}
	if strings.Contains(belowTabBar(plainView(m)), "late") {
		t.Error("the old turn's answer is shown")
	}
}

func TestClearKeepsHistory(t *testing.T) {
	m, _ := newSessionModel(t, answerServer(t, "answer"))
	m = tabTurn(t, m, "one")
	m = tabTurn(t, m, "two")
	hist := m.hist
	m, _ = clearEnter(t, m)
	if m.hist != hist {
		t.Fatal("/clear replaced the input history")
	}
	histSteps(t, m,
		[]tea.KeyType{tea.KeyUp, tea.KeyUp, tea.KeyUp},
		[]string{"/clear", "two", "one"})
}

func TestClearNoSave(t *testing.T) {
	t.Run("nothing saved", func(t *testing.T) {
		m, sess := newSessionModelOpts(t, answerServer(t, "answer"), "start", noAutosave)
		m = tabTurn(t, m, "question")
		_, _ = clearEnter(t, m)
		if files := sessionFiles(t, sess.StartDir()); len(files) != 0 {
			t.Errorf("/clear wrote session files: %v", files)
		}
	})
	t.Run("saved before", func(t *testing.T) {
		m, sess := newSessionModel(t, answerServer(t, "answer"))
		m = tabTurn(t, m, "question")
		m.saveTab()
		files := sessionFiles(t, sess.StartDir())
		if len(files) != 1 {
			t.Fatalf("session files before /clear = %v, want one", files)
		}
		before, err := os.ReadFile(files[0])
		if err != nil {
			t.Fatal(err)
		}
		m, _ = clearEnter(t, m)
		after := sessionFiles(t, sess.StartDir())
		if len(after) != 1 || after[0] != files[0] {
			t.Fatalf("session files after /clear = %v, want %v", after, files)
		}
		got, err := os.ReadFile(after[0])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, before) {
			t.Error("/clear changed the saved session file")
		}
		if countInfo(m, "cleared") != 1 {
			t.Error("no 'cleared' line")
		}
	})
}

func TestClearKeepsGlobals(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "answer"))
	m, _ = m.Command("/mode auto")
	m, _ = m.Command("/effort low")
	m = tabTurn(t, m, "question")
	model, effort, mode := sess.ModelID(), sess.Effort(), sess.Mode()
	m, _ = clearEnter(t, m)
	if m.sess == sess {
		t.Fatal("no new session")
	}
	if m.sess.ModelID() != model || m.sess.Effort() != effort || m.sess.Mode() != mode {
		t.Errorf("new session %s/%s/%s, want %s/%s/%s",
			m.sess.ModelID(), m.sess.Effort(), m.sess.Mode(), model, effort, mode)
	}
	if effort != "low" || string(mode) != "auto" {
		t.Errorf("setup: effort %s, mode %s", effort, mode)
	}
}

func TestClearNewStillAddsTab(t *testing.T) {
	m, _ := newSessionModel(t, answerServer(t, "answer"))
	m = tabTurn(t, m, "question")
	m, _ = clearEnter(t, m)
	if len(m.tabs) != 1 {
		t.Fatalf("/clear: %d tabs, want 1", len(m.tabs))
	}
	m, cmd := m.Command("/new")
	if len(m.tabs) != 2 || m.cur != 1 {
		t.Errorf("/new after /clear: tabs = %d, cur = %d, want 2 and 1", len(m.tabs), m.cur)
	}
	if cmd == nil || countInfo(m, "new session") != 1 {
		t.Error("/new did not open its new session")
	}
}

func TestClearHelpEntry(t *testing.T) {
	found := false
	for _, l := range strings.Split(HelpText(), "\n") {
		if !strings.Contains(l, "/clear") {
			continue
		}
		found = true
		if strings.Contains(l, "alias") {
			t.Errorf("help line still an alias: %q", l)
		}
	}
	if !found {
		t.Error("/help has no /clear line")
	}
}
