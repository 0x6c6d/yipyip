package session

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

// newAgentSession builds an agent session (temp HOME and start dir) talking
// to srv.
func newAgentSession(t *testing.T, srv *providertest.Server, dir string) *agent.Session {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home")
	if dir == "" {
		dir = filepath.Join(base, "start")
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "yipyip"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
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
	s, err := agent.NewSession(agent.Options{Config: cfg, StartDir: dir})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() {
		s.Cancel()
		s.Wait()
	})
	return s
}

// runTurn submits text and waits (bounded) for the turn to end.
func runTurn(t *testing.T, s *agent.Session, text string) {
	t.Helper()
	if err := s.Submit(text); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	done := make(chan struct{})
	go func() { s.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("turn did not finish")
	}
	s.PollEvents()
}

// toolTurnServer scripts one turn: a read_file tool call, then text.
func toolTurnServer() *providertest.Server {
	return providertest.New(
		providertest.Response{
			ToolCalls: []providertest.ToolCall{{ID: "c1", Name: "read_file", Arguments: `{"path":"notes.txt"}`}},
			Usage:     &providertest.Usage{Prompt: 100, Completion: 10},
		},
		providertest.Response{Text: "all done", Usage: &providertest.Usage{Prompt: 150, Completion: 5}},
	)
}

func setupToolTurn(t *testing.T) (*agent.Session, *providertest.Server) {
	t.Helper()
	srv := toolTurnServer()
	t.Cleanup(srv.Close)
	s := newAgentSession(t, srv, "")
	if err := os.WriteFile(filepath.Join(s.StartDir(), "notes.txt"), []byte("FILE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTurn(t, s, "read the\nnotes")
	return s, srv
}

func TestFromAgent(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	fresh := newAgentSession(t, srv, "")
	if d, ok := FromAgent(fresh, "x", "y"); ok || d != nil {
		t.Fatalf("fresh session: ok=%v d=%v", ok, d)
	}

	s, _ := setupToolTurn(t)
	d, ok := FromAgent(s, "abc123abc123", "2026-01-02T03:04:05Z")
	if !ok {
		t.Fatal("FromAgent: ok = false after a turn")
	}
	if d.ID != "abc123abc123" || d.Created != "2026-01-02T03:04:05Z" {
		t.Errorf("id/created = %q/%q", d.ID, d.Created)
	}
	if d.Version != FileVersion {
		t.Errorf("version = %d", d.Version)
	}
	if d.StartDir != s.StartDir() || d.Model != s.ModelName() || d.Effort != s.Effort() || d.Mode != string(s.Mode()) {
		t.Errorf("startdir/model/effort/mode = %q %q %q %q", d.StartDir, d.Model, d.Effort, d.Mode)
	}
	in, out, total := s.TokenCounts()
	if d.InTokens != in || d.OutTokens != out || d.TotalTokens != total || d.LastContext != s.LastContextTokens() {
		t.Errorf("tokens = %d %d %d %d", d.InTokens, d.OutTokens, d.TotalTokens, d.LastContext)
	}
	if total == 0 {
		t.Error("expected non-zero token counts")
	}
	if d.Title != "read the notes" {
		t.Errorf("title = %q", d.Title)
	}
	hist := s.History()
	if len(d.Conversation) != len(hist) {
		t.Fatalf("conversation len %d, history len %d", len(d.Conversation), len(hist))
	}
	foundCall := false
	for i, h := range hist {
		c := d.Conversation[i]
		if c.Role != h.Role || c.Content != h.Content || c.ToolCallID != h.ToolCallID || len(c.ToolCalls) != len(h.ToolCalls) {
			t.Errorf("entry %d: %+v vs %+v", i, c, h)
			continue
		}
		for j, tc := range h.ToolCalls {
			got := c.ToolCalls[j]
			if got.ID != tc.ID || got.Type != "function" || got.Function.Name != tc.Name || got.Function.Arguments != tc.Arguments {
				t.Errorf("tool call %d/%d: %+v vs %+v", i, j, got, tc)
			}
			if tc.Name == "read_file" && tc.ID == "c1" {
				foundCall = true
			}
		}
	}
	if !foundCall {
		t.Error("read_file tool call not preserved")
	}
	if d.History == nil || len(d.History) != 0 || d.Queue == nil || len(d.Queue) != 0 {
		t.Errorf("history/queue = %#v %#v", d.History, d.Queue)
	}
	if d.Pastes.Entries == nil || len(d.Pastes.Entries) != 0 || d.Pastes.N != 0 {
		t.Errorf("pastes = %#v", d.Pastes)
	}
	_, started, _ := s.TurnState()
	if d.Turn.Started != started || started == "" || d.Turn.Interrupted {
		t.Errorf("turn = %+v (started %q)", d.Turn, started)
	}
}

func TestFromAgentRoundTrip(t *testing.T) {
	s, srv := setupToolTurn(t)
	d, ok := FromAgent(s, "", "2026-01-02T03:04:05Z")
	if !ok {
		t.Fatal("FromAgent: ok = false")
	}
	store, err := NewStore(s.StartDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(d); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := store.Load(d.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	target := newAgentSession(t, srv, s.StartDir())
	ApplyTo(loaded, target)
	if got, want := target.History(), s.History(); !reflect.DeepEqual(got, want) {
		t.Errorf("history differs:\n got %+v\nwant %+v", got, want)
	}
	gi, go_, gt := target.TokenCounts()
	wi, wo, wt := s.TokenCounts()
	if gi != wi || go_ != wo || gt != wt || target.LastContextTokens() != s.LastContextTokens() {
		t.Errorf("tokens = %d %d %d %d, want %d %d %d %d", gi, go_, gt, target.LastContextTokens(), wi, wo, wt, s.LastContextTokens())
	}
}

func TestApplyToKeepsModelAndMode(t *testing.T) {
	s, srv := setupToolTurn(t)
	d, ok := FromAgent(s, "", "2026-01-02T03:04:05Z")
	if !ok {
		t.Fatal("FromAgent: ok = false")
	}
	target := newAgentSession(t, srv, "")
	model, mode, effort := target.ModelName(), target.Mode(), target.Effort()
	d.Model = model + "-other"
	if mode == agent.ModeAuto {
		d.Mode = string(agent.ModeManual)
	} else {
		d.Mode = string(agent.ModeAuto)
	}
	d.Effort = agent.EffortHigh
	if effort == agent.EffortHigh {
		d.Effort = agent.EffortLow
	}
	ApplyTo(d, target)
	if target.ModelName() != model || target.Mode() != mode || target.Effort() != effort {
		t.Errorf("model/mode/effort = %q %q %q, want %q %q %q",
			target.ModelName(), target.Mode(), target.Effort(), model, mode, effort)
	}
	if len(target.History()) != len(s.History()) {
		t.Errorf("history not applied")
	}
}
