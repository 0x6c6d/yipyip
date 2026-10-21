package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
	"yipyip/internal/tui"
)

// env is one end-to-end setup: a TUI model around a real agent session that
// talks to a fake provider, in a temporary HOME and start directory.
type env struct {
	Model   tui.Model
	Session *agent.Session
	Server  *providertest.Server
	Dir     string // start directory
	Home    string
}

// newEnv starts a fake provider playing resps and builds the session (auto
// mode) and the TUI model (160x50) on top of it.
func newEnv(t *testing.T, resps ...providertest.Response) *env {
	t.Helper()
	srv := providertest.New(resps...)
	t.Cleanup(srv.Close)

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
	cfgJSON := providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000)
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
	sess.SetMode(agent.ModeAuto)
	t.Cleanup(func() {
		sess.Cancel()
		waitSession(t, sess)
	})
	next, _ := tui.New(tui.Options{StartDir: dir, Session: sess}).Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	return &env{Model: next.(tui.Model), Session: sess, Server: srv, Dir: dir, Home: home}
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

// submit sends text as a user message (or slash command), waits for the turn
// to end and applies the resulting events to the model.
func (e *env) submit(t *testing.T, text string) {
	t.Helper()
	m, _ := e.Model.Command(text)
	waitSession(t, e.Session)
	e.Model = m.Drain()
}

// path returns the absolute path of rel in the start directory.
func (e *env) path(rel string) string { return filepath.Join(e.Dir, rel) }

func (e *env) write(t *testing.T, rel, content string) {
	t.Helper()
	if err := os.WriteFile(e.path(rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// read returns the file content, or "<missing>".
func (e *env) read(rel string) string {
	b, err := os.ReadFile(e.path(rel))
	if err != nil {
		return "<missing>"
	}
	return string(b)
}
