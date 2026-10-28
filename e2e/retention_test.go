package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
	"yipyip/internal/session"
	"yipyip/internal/tui"
)

// seedOldSessions writes n valid session files with ids 000000000000-style
// (index in hex) and Updated stamps in 2020, index 0 the oldest. It returns
// the ids, oldest first.
func seedOldSessions(t *testing.T, dir string, n int) []string {
	t.Helper()
	sdir := filepath.Join(dir, ".yipyip", "sessions")
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	var ids []string
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%012x", 0xabc000+i)
		stamp := base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339)
		d := session.SessionData{Version: session.FileVersion, ID: id, Created: stamp, Updated: stamp, StartDir: dir, Title: "old " + id}
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sdir, id+".json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// TestRetentionStartup runs the real binary without a TTY: the startup prune
// runs before the UI is built, so 30 old sessions are cut to the newest 25
// even though the UI cannot run (the program ends with an error or EOF).
func TestRetentionStartup(t *testing.T) {
	home, dir := crashDirs(t)
	ids := seedOldSessions(t, dir, 30)
	res := runBinary(t, crashBinary(t, ""), home, dir, []string{"FAKE_KEY=" + secret})
	t.Logf("exit %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	got := sessionIDs(t, dir)
	if len(got) != 25 {
		t.Fatalf("%d session files after startup, want 25: %v", len(got), got)
	}
	for _, id := range ids[:5] {
		if slices.Contains(got, id) {
			t.Errorf("oldest session %s survived the startup prune", id)
		}
	}
	for _, id := range ids[5:] {
		if !slices.Contains(got, id) {
			t.Errorf("newer session %s was deleted", id)
		}
	}
}

func TestRetentionOpenTabProtected(t *testing.T) {
	srv := providertest.NewFunc(func(int, providertest.Request) providertest.Response { return resp{Text: "ok"} })
	var sessions []*agent.Session
	e := newEnvServer(t, srv, envOpts{contextLimit: 8000, tuiOpts: func(o *tui.Options) {
		start, home := o.StartDir, o.Home
		o.NewSession = func() (*agent.Session, error) {
			cfg, err := config.Load(home, start, func(k string) string {
				if k == "FAKE_KEY" {
					return secret
				}
				return ""
			})
			if err != nil {
				return nil, err
			}
			s, err := agent.NewSession(agent.Options{Config: cfg, StartDir: start})
			if err == nil {
				s.SetMode(agent.ModeAuto)
				sessions = append(sessions, s)
			}
			return s, err
		}
	}})
	t.Cleanup(func() {
		for _, s := range sessions {
			s.Cancel()
			waitSession(t, s)
		}
	})
	e.submit(t, "first tab")
	first := sessionIDs(t, e.Dir)
	if len(first) != 1 {
		t.Fatalf("session files after tab 1 = %v, want one", first)
	}
	next, _ := e.Model.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	e.Model = next.(tui.Model)
	if len(sessions) != 1 {
		t.Fatalf("Ctrl+T built %d sessions, want 1", len(sessions))
	}
	next, _ = e.Model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("second tab")})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnter})
	waitSession(t, sessions[0])
	e.Model = next.(tui.Model).Drain()
	all := sessionIDs(t, e.Dir)
	if len(all) != 2 {
		t.Fatalf("session files after tab 2 = %v, want two", all)
	}

	// Make both open tabs' files the oldest of 32, then force more saves.
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, id := range all {
		p := filepath.Join(e.Dir, ".yipyip", "sessions", id+".json")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var d session.SessionData
		if err := json.Unmarshal(b, &d); err != nil {
			t.Fatal(err)
		}
		d.Created, d.Updated = old.Format(time.RFC3339), old.Format(time.RFC3339)
		b, _ = json.Marshal(d)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	seedOldSessions(t, e.Dir, 30)
	// A save of the current tab prunes; the other tab's file must stay too.
	// The current tab is the second one; its turn runs in sessions[0].
	e.Model, _ = e.Model.Command("third message")
	waitSession(t, sessions[0])
	e.Model = e.Model.Drain()
	got := sessionIDs(t, e.Dir)
	for _, id := range all {
		if !slices.Contains(got, id) {
			t.Errorf("open tab session %s was pruned (files: %v)", id, got)
		}
	}
	if len(got) > 27 {
		t.Errorf("%d files remain, want at most 25 plus the open tabs: %v", len(got), got)
	}
}
