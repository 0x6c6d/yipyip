package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

// newSessionModel builds a Model (width 160) around a real agent session that
// talks to srv; a nil srv gives a config without any provider.
func newSessionModel(t *testing.T, srv *providertest.Server) (Model, *agent.Session) {
	t.Helper()
	cfgJSON := `{"provider": {}}`
	if srv != nil {
		cfgJSON = providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000)
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	for _, d := range []string{filepath.Join(home, ".config", "yipyip"), dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(config.GlobalPath(home), []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(home, dir, func(k string) string {
		if k == "FAKE_KEY" {
			return "SECRET123"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	sess, err := agent.NewSession(agent.Options{Config: cfg, StartDir: dir})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() {
		sess.Cancel()
		waitSession(t, sess)
	})
	m := New(Options{StartDir: dir, Session: sess})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return next.(Model), sess
}

func waitSession(t *testing.T, s *agent.Session) {
	t.Helper()
	done := make(chan struct{})
	go func() { s.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("turn did not finish within 10s")
	}
}

func waitBusy(t *testing.T, s *agent.Session) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !s.Busy() {
		if time.Now().After(deadline) {
			t.Fatal("session never became busy")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSendMessageStreams(t *testing.T) {
	srv := providertest.New(providertest.Response{
		Text:  "hello from the fake provider",
		Usage: &providertest.Usage{Prompt: 100, Completion: 20},
	})
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	m, cmd := m.Command("say hi")
	if cmd != nil {
		t.Fatal("a message must not return a command")
	}
	waitSession(t, sess)
	m = m.Drain()
	v := m.View()
	if !strings.Contains(v, "> say hi") {
		t.Errorf("view lacks the echoed input:\n%s", v)
	}
	if !strings.Contains(v, "hello from the fake provider") {
		t.Errorf("view lacks the assistant text:\n%s", v)
	}
	if n := strings.Count(v, "hello from the fake provider"); n != 1 {
		t.Errorf("assistant text shown %d times, want one streamed block", n)
	}
	if reqs := srv.Requests(); len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	if !strings.Contains(m.StatusLine(), "fake/m1") || !strings.Contains(m.StatusLine(), "in 100 / out 20 / total 120") {
		t.Errorf("status line = %q", m.StatusLine())
	}
}

func TestEscCancels(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "late", Delay: 30 * time.Second})
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	m, _ = m.Command("slow")
	waitBusy(t, sess)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	waitSession(t, sess)
	if sess.Busy() {
		t.Fatal("still busy after Esc")
	}
	m = m.Drain()
	if v := m.View(); !strings.Contains(v, agent.CancelledMessage) {
		t.Errorf("view lacks the cancelled message:\n%s", v)
	}
	// Esc while idle does nothing.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil || next.(Model).View() != m.View() {
		t.Error("Esc while idle must be a no-op")
	}
}

func TestDoubleCtrlCQuits(t *testing.T) {
	m, _ := newSessionModel(t, nil)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("first Ctrl+C must not quit")
	}
	if v := m.View(); !strings.Contains(v, "press Ctrl+C again to quit") {
		t.Fatalf("view lacks the hint:\n%s", v)
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("second Ctrl+C returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("second Ctrl+C did not quit")
	}
	if m.ctx.Err() == nil {
		t.Error("the event context was not cancelled on quit")
	}

	// A second press after the window has passed only re-arms.
	m2, _ := newSessionModel(t, nil)
	now := time.Now()
	m2.now = func() time.Time { return now }
	next, _ = m2.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m2 = next.(Model)
	m2.now = func() time.Time { return now.Add(3 * time.Second) }
	if _, cmd = m2.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd != nil {
		t.Error("Ctrl+C after 3 s must not quit")
	}
}

func TestNoModelMessage(t *testing.T) {
	m, sess := newSessionModel(t, nil)
	if sess.ModelID() != "" {
		t.Fatalf("model = %q, want none", sess.ModelID())
	}
	m, _ = m.Command("hello")
	v := m.View()
	if !strings.Contains(v, "> hello") || !strings.Contains(v, "no model configured") {
		t.Fatalf("view:\n%s", v)
	}
	if !strings.Contains(m.StatusLine(), "no model") {
		t.Errorf("status line = %q", m.StatusLine())
	}
}

func TestSlashNotSentToModel(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	m, cmd := m.Command("/bogus arg")
	if cmd != nil {
		t.Fatal("unknown command returned a command")
	}
	if v := m.View(); !strings.Contains(v, "unknown command: /bogus") {
		t.Fatalf("view:\n%s", v)
	}
	for _, line := range []string{"/exit", "/quit"} {
		_, cmd = m.Command(line)
		if cmd == nil {
			t.Fatalf("%s returned no command", line)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s did not quit", line)
		}
	}
	// Typed through the input box too.
	var tm tea.Model = m
	tm = typeText(tm, "/other")
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if v := tm.View(); !strings.Contains(v, "unknown command: /other") {
		t.Fatalf("view:\n%s", v)
	}
	if sess.Busy() {
		t.Fatal("a slash command started a turn")
	}
	waitSession(t, sess)
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("slash commands reached the provider: %d requests", n)
	}
}

func TestSecretTUIView(t *testing.T) {
	srv := providertest.New(
		providertest.Response{Text: "the key is SECRET123, honest"},
		providertest.Response{Status: 401, Body: `{"error":"bad key SECRET123"}`},
	)
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	m, _ = m.Command("first")
	waitSession(t, sess)
	m, _ = m.Command("second")
	waitSession(t, sess)
	m = m.Drain()
	v := m.View()
	if strings.Contains(v, "SECRET123") {
		t.Fatalf("view leaks the key:\n%s", v)
	}
	if !strings.Contains(v, "[redacted]") {
		t.Fatalf("view lacks [redacted]:\n%s", v)
	}
	if !strings.Contains(v, "error:") {
		t.Fatalf("view lacks the error line:\n%s", v)
	}
	// Defence in depth: the model itself redacts what it is given.
	m.apply(agent.Event{Kind: agent.EvInfo, Text: "raw SECRET123"})
	if strings.Contains(m.View(), "SECRET123") {
		t.Fatal("the model rendered an unredacted event")
	}
}
