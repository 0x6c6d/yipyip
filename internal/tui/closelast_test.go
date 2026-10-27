package tui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

// yieldsQuit runs cmd in a goroutine and reports whether it returns a
// tea.QuitMsg within 200 ms; a command that blocks longer (an event pump)
// counts as no quit and is not waited for.
func yieldsQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	select {
	case msg := <-got:
		_, ok := msg.(tea.QuitMsg)
		return ok
	case <-time.After(200 * time.Millisecond):
		return false
	}
}

// checkFreshLastTab asserts that closing the only tab left one fresh tab in
// place of the old session, without quitting and without saving.
func checkFreshLastTab(t *testing.T, m Model, cmd tea.Cmd, old *agent.Session) {
	t.Helper()
	if len(m.tabs) != 1 || m.cur != 0 {
		t.Fatalf("tabs = %d, cur = %d, want 1 and 0", len(m.tabs), m.cur)
	}
	if m.sess == nil || m.sess == old {
		t.Fatal("the last tab did not get a new session")
	}
	if len(m.blocks) != 0 {
		t.Errorf("blocks = %d, want 0", len(m.blocks))
	}
	if m.hist == nil || len(m.hist.entries) != 0 {
		t.Error("the input history is not empty")
	}
	if m.ctx.Err() != nil {
		t.Error("the program context was cancelled (quit)")
	}
	if cmd == nil {
		t.Error("no event command for the fresh session")
	}
	if yieldsQuit(cmd) {
		t.Error("closing the last tab quit")
	}
	if files := sessionFiles(t, old.StartDir()); len(files) != 0 {
		t.Errorf("the discarded tab was saved: %v", files)
	}
}

func TestCloseLastTabCtrlW(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "old answer"))
	m = tabTurn(t, m, "only question")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	checkFreshLastTab(t, next.(Model), cmd, sess)
}

func TestCloseLastTabClick(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "old answer"))
	m = tabTurn(t, m, "only question")
	m, cmd := clickBar(m, zoneCol(t, m, tabZoneClose, 0))
	checkFreshLastTab(t, m, cmd, sess)
}

func TestCloseLastTabBusy(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "late", Delay: 30 * time.Second})
	t.Cleanup(srv.Close)
	m, sess := newSessionModel(t, srv)
	m.input.SetValue("slow")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	waitBusy(t, sess)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	m = next.(Model)
	waitSession(t, sess)
	checkFreshLastTab(t, m, cmd, sess)
	if m.turnActive || len(m.queue) != 0 {
		t.Errorf("turnActive %v, queue %q, want a fresh tab", m.turnActive, m.queue)
	}
}

func TestCloseLastTabTwoTabs(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "tab zero answer"))
	m = tabTurn(t, m, "tab zero question")
	mustAddTab(t, &m)
	other := m.sess
	m.switchTab(0)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	m = next.(Model)
	if m.ctx.Err() != nil || yieldsQuit(cmd) {
		t.Fatal("closing one of two tabs quit")
	}
	if len(m.tabs) != 1 || m.sess != other {
		t.Fatalf("tabs = %d, other tab current %v", len(m.tabs), m.sess == other)
	}
	if files := sessionFiles(t, sess.StartDir()); len(files) != 1 {
		t.Errorf("session files = %v, want the closed tab saved", files)
	}
}

func TestCloseLastTabNewSessionError(t *testing.T) {
	m, sess := newSessionModelOpts(t, answerServer(t, "a"), "start", func(o *Options) {
		o.NewSession = func() (*agent.Session, error) { return nil, errors.New("boom") }
	})
	m = tabTurn(t, m, "keep")
	blocks := len(m.blocks)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	m = next.(Model)
	if m.ctx.Err() != nil || yieldsQuit(cmd) {
		t.Fatal("a failing NewSession quit")
	}
	if len(m.tabs) != 1 || m.sess != sess {
		t.Fatalf("tabs = %d, same session %v, want the tab unchanged", len(m.tabs), m.sess == sess)
	}
	if len(m.blocks) != blocks+1 || m.blocks[blocks].kind != blockError {
		t.Fatalf("blocks = %d (was %d), want one error block added", len(m.blocks), blocks)
	}
	if files := sessionFiles(t, sess.StartDir()); len(files) != 0 {
		t.Errorf("the failed close saved: %v", files)
	}
}

func TestCloseLastTabExitStillQuits(t *testing.T) {
	m, _ := newSessionModel(t, answerServer(t, "x"))
	_, cmd := m.Command("/exit")
	if cmd == nil {
		t.Fatal("/exit returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("/exit does not quit")
	}
}
