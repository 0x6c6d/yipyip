package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

func TestResetTabKeepHistory(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "old answer"))
	m = tabTurn(t, m, "old question")
	mustAddTab(t, &m)
	m.switchTab(0)
	model, effort, mode := m.globals()
	hist, pastes := m.hist, m.pastes
	m.input.SetValue("draft text")

	if err := m.resetTab(true); err != nil {
		t.Fatalf("resetTab(true): %v", err)
	}
	if len(m.tabs) != 2 || m.cur != 0 {
		t.Fatalf("tabs = %d, cur = %d, want 2 and 0", len(m.tabs), m.cur)
	}
	if m.sess == nil || m.sess == sess {
		t.Fatal("the current tab did not get a new session")
	}
	if m.tabs[0].sess != m.sess {
		t.Error("the tab list does not hold the new session")
	}
	if len(m.blocks) != 0 || len(m.queue) != 0 || m.turnActive {
		t.Errorf("tab not fresh: %d blocks, queue %q, turnActive %v", len(m.blocks), m.queue, m.turnActive)
	}
	if m.hist != hist || m.pastes != pastes {
		t.Error("resetTab(true) replaced the input history or the pastes")
	}
	if len(m.hist.entries) != 1 {
		t.Errorf("kept history = %v, want the one submitted input", m.hist.entries)
	}
	if got := m.input.Value(); got != "draft text" {
		t.Errorf("input = %q, want the draft kept", got)
	}
	if m.tabOf(sess) != -1 {
		t.Error("the old session still belongs to a tab")
	}
	if a, b, c := m.globals(); a != model || b != effort || c != mode {
		t.Errorf("globals %s/%s/%s, want %s/%s/%s", a, b, c, model, effort, mode)
	}
	if n := len(m.sess.History()); n != 1 {
		t.Errorf("fresh history = %d entries, want 1", n)
	}
	if files := sessionFiles(t, sess.StartDir()); len(files) != 0 {
		t.Errorf("resetTab wrote session files: %v", files)
	}
	if m.saved == nil || m.saved.id != "" {
		t.Error("the fresh tab did not get a new save state")
	}
}

func TestResetTabFresh(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "answer"))
	m = tabTurn(t, m, "question")
	hist := m.hist
	m.input.SetValue("draft text")

	if err := m.resetTab(false); err != nil {
		t.Fatalf("resetTab(false): %v", err)
	}
	if m.sess == nil || m.sess == sess || len(m.tabs) != 1 || m.cur != 0 {
		t.Fatalf("no in-place reset: same session %v, tabs %d, cur %d", m.sess == sess, len(m.tabs), m.cur)
	}
	if m.hist == hist || m.hist == nil || len(m.hist.entries) != 0 {
		t.Error("resetTab(false) did not start a new empty history")
	}
	if got := m.input.Value(); got != "" {
		t.Errorf("input = %q, want empty", got)
	}
	if len(m.blocks) != 0 {
		t.Errorf("blocks = %d, want 0", len(m.blocks))
	}
}

func TestResetTabBusy(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "late", Delay: 30 * time.Second})
	t.Cleanup(srv.Close)
	m, sess := newSessionModel(t, srv)
	m.input.SetValue("slow")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	waitBusy(t, sess)

	if err := m.resetTab(true); err != nil {
		t.Fatalf("resetTab: %v", err)
	}
	waitSession(t, sess)
	if m.turnActive {
		t.Error("turn still active after the reset")
	}
	if len(m.queue) != 0 {
		t.Errorf("queue = %q, want empty", m.queue)
	}
	if m.sess == sess || len(m.tabs) != 1 {
		t.Errorf("same session %v, tabs %d", m.sess == sess, len(m.tabs))
	}
}

func TestResetTabDeniesApproval(t *testing.T) {
	srv := providertest.New(bashCall("c1", "touch marker"), providertest.Response{Text: "done"})
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	m, _ = m.Command("touch it")
	m = waitPrompt(t, m)
	if len(m.pending) == 0 {
		t.Fatal("no pending approval")
	}

	if err := m.resetTab(true); err != nil {
		t.Fatalf("resetTab: %v", err)
	}
	waitSession(t, sess)
	if fileExists(filepath.Join(sess.StartDir(), "marker")) {
		t.Fatal("the tool ran after the reset")
	}
	if len(m.pending) != 0 {
		t.Errorf("pending = %d, want 0", len(m.pending))
	}
}

func TestResetTabNewSessionError(t *testing.T) {
	m, sess := newSessionModelOpts(t, answerServer(t, "a"), "start", func(o *Options) {
		o.NewSession = func() (*agent.Session, error) { return nil, errors.New("boom") }
	})
	m = tabTurn(t, m, "keep")
	blocks, tabs := m.blocks, m.tabs

	if err := m.resetTab(true); err == nil {
		t.Fatal("resetTab returned no error")
	}
	if m.sess != sess {
		t.Error("the session changed")
	}
	if len(m.blocks) != len(blocks) || (len(blocks) > 0 && &m.blocks[0] != &blocks[0]) {
		t.Error("the blocks changed")
	}
	if len(m.tabs) != len(tabs) || &m.tabs[0] != &tabs[0] {
		t.Error("the tab list changed")
	}
	if sess.Busy() {
		t.Error("the old session is busy")
	}
}

func TestResetTabKeepsSavedFile(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "answer"))
	m = tabTurn(t, m, "question")
	m.saveTab()
	files := sessionFiles(t, sess.StartDir())
	if len(files) != 1 {
		t.Fatalf("session files = %v, want one", files)
	}
	before, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}

	if err := m.resetTab(true); err != nil {
		t.Fatalf("resetTab: %v", err)
	}
	after := sessionFiles(t, sess.StartDir())
	if len(after) != 1 || after[0] != files[0] {
		t.Fatalf("session files = %v, want %v", after, files)
	}
	got, err := os.ReadFile(after[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(before) {
		t.Error("the saved file changed")
	}
}
