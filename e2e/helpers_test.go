package e2e

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
	"yipyip/internal/session"
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
	return newEnvServer(t, providertest.New(resps...), envOpts{contextLimit: 8000})
}

// envOpts tunes newEnvServer.
type envOpts struct {
	contextLimit int                // context limit of the fake model
	projectJSON  string             // content of the start directory's yipyip.json, "" = none
	noModel      bool               // global config without providers: no model is configured
	fetchClient  *http.Client       // web_fetch client seam (agent.Options.FetchClient), nil = default
	clipboard    func(string) error // clipboard writer (tui.Options.Clipboard), nil = default
	tuiOpts      func(*tui.Options) // edits the TUI options before the model is built, nil = none
}

// newEnvServer builds the environment around an already started fake srv
// (closed on cleanup).
func newEnvServer(t *testing.T, srv *providertest.Server, o envOpts) *env {
	t.Helper()
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
	cfgJSON := providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", o.contextLimit)
	if o.noModel {
		cfgJSON = "{}"
	}
	if err := os.WriteFile(config.GlobalPath(home), []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if o.projectJSON != "" {
		if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(o.projectJSON), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(home, dir, func(k string) string {
		if k == "FAKE_KEY" {
			return "SECRET123"
		}
		if k == "BRAVE_API_KEY" {
			return "BRAVEKEY-E2E"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	sess, err := agent.NewSession(agent.Options{Config: cfg, StartDir: dir, FetchClient: o.fetchClient})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess.SetMode(agent.ModeAuto)
	t.Cleanup(func() {
		sess.Cancel()
		waitSession(t, sess)
	})
	topts := tui.Options{StartDir: dir, Home: home, Session: sess, Clipboard: o.clipboard}
	if o.tuiOpts != nil {
		o.tuiOpts(&topts)
	}
	next, _ := tui.New(topts).Update(tea.WindowSizeMsg{Width: 160, Height: 50})
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

// sessionIDs returns the ids (file names without .json) of the session files
// in <dir>/.yipyip/sessions, sorted; a missing directory gives none.
func sessionIDs(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(dir, ".yipyip", "sessions"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var ids []string
	for _, e := range ents {
		if name, ok := strings.CutSuffix(e.Name(), ".json"); ok {
			ids = append(ids, name)
		}
	}
	sort.Strings(ids)
	return ids
}

// fakeClock is a settable clock for tui.Options.Now.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

// Now returns the clock's time.
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Set moves the clock to t.
func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

// saveCounter wraps the default Store save of start and counts the calls.
type saveCounter struct {
	mu sync.Mutex
	n  int
}

// save is a tui.Options.Autosave that stores data in the Store of start.
func (c *saveCounter) save(start string) func(*session.SessionData) error {
	return func(d *session.SessionData) error {
		c.mu.Lock()
		c.n++
		c.mu.Unlock()
		st, err := session.NewStore(start)
		if err != nil {
			return err
		}
		return st.Save(d)
	}
}

// count returns how many saves ran.
func (c *saveCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}
