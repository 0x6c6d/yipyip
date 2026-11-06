package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

// waitWithin reports whether s.Wait returns within d.
func waitWithin(s *agent.Session, d time.Duration) bool {
	done := make(chan struct{})
	go func() { s.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

func TestQuitShutsDownSessions(t *testing.T) {
	srv := providertest.NewFunc(func(int, providertest.Request) providertest.Response {
		return providertest.Response{Text: "late", Delay: 30 * time.Second}
	})
	t.Cleanup(srv.Close)
	m, first := newSessionModel(t, srv)
	m, _ = m.Command("first turn")
	waitBusy(t, first)
	mustAddTab(t, &m)
	second := m.sess
	if second == nil || second == first {
		t.Fatal("the new tab has no session of its own")
	}
	m, _ = m.Command("second turn")
	waitBusy(t, second)

	_, cmd := m.Command("/quit")
	if cmd == nil {
		t.Fatal("/quit returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("/quit did not quit")
	}
	for i, s := range []*agent.Session{first, second} {
		if !waitWithin(s, 3*time.Second) {
			t.Fatalf("the turn of tab %d did not end within 3 s of /quit", i+1)
		}
	}
}

func TestSignalQuitMsgSaves(t *testing.T) {
	srv := toolTurnServer()
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	m = runToolTurn(t, m, sess)

	next, cmd := m.Update(SignalQuitMsg{})
	if _, ok := next.(Model); !ok {
		t.Fatalf("Update returned %T, want Model", next)
	}
	if cmd == nil {
		t.Fatal("SignalQuitMsg returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("SignalQuitMsg did not quit")
	}
	doc, _ := readSavedFile(t, sess.StartDir())
	if title, _ := doc["title"].(string); title != "read the file" {
		t.Errorf("title = %q", doc["title"])
	}
}
