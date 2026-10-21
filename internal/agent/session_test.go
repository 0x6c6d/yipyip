package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"yipyip/internal/config"
	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

// newTestSession builds a session in a temp start directory with a temp HOME
// whose config uses the fake provider (models fake/m1 and fake/m2). The API
// key comes from {env:FAKE_KEY} = SECRET123 via an injected getenv.
func newTestSession(t *testing.T, agentsMD string) (*Session, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	if err := os.MkdirAll(filepath.Join(home, ".config", "yipyip"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgJSON := providertest.ConfigJSON("http://127.0.0.1:9/v1", "{env:FAKE_KEY}", 8000)
	if err := os.WriteFile(config.GlobalPath(home), []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if agentsMD != "" {
		if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agentsMD), 0o644); err != nil {
			t.Fatal(err)
		}
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
	s, err := NewSession(Options{Config: cfg, StartDir: dir})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return s, dir
}

func TestNewSessionDefaults(t *testing.T) {
	s, dir := newTestSession(t, "")
	if s.ModelID() != "fake/m1" {
		t.Errorf("ModelID = %q", s.ModelID())
	}
	if !reflect.DeepEqual(s.Models(), []string{"fake/m1", "fake/m2"}) {
		t.Errorf("Models = %v", s.Models())
	}
	if s.Effort() != "medium" || s.Mode() != ModeManual || s.Tokens() != (Tokens{}) {
		t.Errorf("effort=%q mode=%q tokens=%+v", s.Effort(), s.Mode(), s.Tokens())
	}
	if s.StartDir() != dir {
		t.Errorf("StartDir = %q, want %q", s.StartDir(), dir)
	}
	if used, limit := s.ContextSize(); used != 0 || limit != 8000 {
		t.Errorf("ContextSize = %d/%d", used, limit)
	}
	h := s.History()
	if len(h) != 1 || h[0].Role != "system" || h[0].Content != s.SystemPrompt() {
		t.Fatalf("History = %+v", h)
	}
	p := s.SystemPrompt()
	if !strings.Contains(p, dir) || !strings.Contains(p, "yip") || strings.Contains(p, "AGENTS.md\n") {
		t.Errorf("system prompt: %q", p)
	}
	if strings.Contains(p, "SECRET123") {
		t.Error("system prompt contains the secret")
	}
	if got := s.Redact("key SECRET123"); strings.Contains(got, "SECRET123") {
		t.Errorf("Redact = %q", got)
	}
	// History is a copy.
	h[0].Content = "changed"
	if s.History()[0].Content == "changed" {
		t.Error("History is not a copy")
	}
	if len(s.PollEvents()) != 0 {
		t.Error("new session has queued events")
	}
}

func TestSetModelUnknown(t *testing.T) {
	s, _ := newTestSession(t, "")
	for _, id := range []string{"fake/nope", "nope/m1", "m1", ""} {
		err := s.SetModel(id)
		if !errors.Is(err, ErrUnknownModel) {
			t.Errorf("SetModel(%q) = %v, want ErrUnknownModel", id, err)
		}
		if s.ModelID() != "fake/m1" {
			t.Fatalf("model changed to %q", s.ModelID())
		}
	}
	if err := s.SetModel("fake/m2"); err != nil || s.ModelID() != "fake/m2" {
		t.Fatalf("SetModel(fake/m2): %v, model %q", err, s.ModelID())
	}
	if _, limit := s.ContextSize(); limit != 0 {
		t.Errorf("m2 context limit = %d, want 0", limit)
	}
}

func TestSetEffort(t *testing.T) {
	s, _ := newTestSession(t, "")
	for _, e := range []string{"low", "high", "medium"} {
		if err := s.SetEffort(e); err != nil || s.Effort() != e {
			t.Errorf("SetEffort(%q): %v, effort %q", e, err, s.Effort())
		}
	}
	for _, e := range []string{"", "extreme", "LOW", " high"} {
		if err := s.SetEffort(e); !errors.Is(err, ErrInvalidEffort) {
			t.Errorf("SetEffort(%q) = %v, want ErrInvalidEffort", e, err)
		}
		if s.Effort() != "medium" {
			t.Fatalf("effort changed to %q", s.Effort())
		}
	}
}

func TestToggleMode(t *testing.T) {
	s, _ := newTestSession(t, "")
	if m := s.ToggleMode(); m != ModeAuto || s.Mode() != ModeAuto {
		t.Fatalf("first toggle: %q", m)
	}
	if m := s.ToggleMode(); m != ModeManual || s.Mode() != ModeManual {
		t.Fatalf("second toggle: %q", m)
	}
	s.SetMode(ModeAuto)
	if s.Mode() != ModeAuto {
		t.Fatal("SetMode(auto) not applied")
	}
	s.SetMode(ModeManual)
	if s.Mode() != ModeManual {
		t.Fatal("SetMode(manual) not applied")
	}
}

func TestSystemPromptAgentsMD(t *testing.T) {
	s, dir := newTestSession(t, "Use tabs. token=SECRET123\n")
	p := s.SystemPrompt()
	if !strings.Contains(p, "\n\n# AGENTS.md\nUse tabs.") {
		t.Errorf("AGENTS.md not appended: %q", p)
	}
	if strings.Contains(p, "SECRET123") {
		t.Error("AGENTS.md secret not redacted")
	}
	if s.History()[0].Content != p {
		t.Error("history[0] is not the system prompt")
	}

	// Re-read on Reset, truncated to 30,000 characters.
	long := strings.Repeat("ä", 40000)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Reset()
	p = s.SystemPrompt()
	_, notes, ok := strings.Cut(p, "\n\n# AGENTS.md\n")
	if !ok || notes != strings.Repeat("ä", 30000) {
		t.Errorf("truncated AGENTS.md: ok=%v len=%d chars", ok, len([]rune(notes)))
	}
	if s.History()[0].Content != p {
		t.Error("history[0] not updated on Reset")
	}

	// Removing AGENTS.md drops the section on Reset.
	if err := os.Remove(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	s.Reset()
	if strings.Contains(s.SystemPrompt(), "# AGENTS.md") {
		t.Error("AGENTS.md section kept after removal")
	}
}

func TestEventsNeverBlock(t *testing.T) {
	s, _ := newTestSession(t, "")
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10000; i++ {
			s.emit(Event{Kind: EvText, AgentID: MainAgentID, Text: "x SECRET123", Args: `{"k":"SECRET123"}`})
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("emit blocked without a reader")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ev, err := s.NextEvent(ctx)
	if err != nil || ev.Kind != EvText {
		t.Fatalf("NextEvent = %+v, %v", ev, err)
	}
	if strings.Contains(ev.Text, "SECRET123") || strings.Contains(ev.Args, "SECRET123") {
		t.Errorf("event not redacted: %+v", ev)
	}
	if evs := s.PollEvents(); len(evs) != 9999 {
		t.Fatalf("PollEvents = %d events, want 9999", len(evs))
	}
	if evs := s.PollEvents(); len(evs) != 0 {
		t.Fatalf("second PollEvents = %d events", len(evs))
	}

	// NextEvent honours ctx when the queue is empty.
	short, cancelShort := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelShort()
	if _, err := s.NextEvent(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("NextEvent on empty queue = %v", err)
	}

	// A blocked NextEvent wakes up on emit.
	got := make(chan Event, 1)
	go func() {
		ev, _ := s.NextEvent(ctx)
		got <- ev
	}()
	s.emit(Event{Kind: EvInfo, Text: "hello"})
	select {
	case ev := <-got:
		if ev.Kind != EvInfo || ev.Text != "hello" {
			t.Fatalf("woken with %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("NextEvent did not wake up")
	}
}

func TestResetClearsState(t *testing.T) {
	s, _ := newTestSession(t, "")
	if err := s.SetModel("fake/m2"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEffort("high"); err != nil {
		t.Fatal(err)
	}
	s.SetMode(ModeAuto)

	cancelled := false
	s.mu.Lock()
	s.history = append(s.history,
		provider.Message{Role: "user", Content: "hi"}, provider.Message{Role: "assistant", Content: "hello"})
	s.tokens = Tokens{In: 10, Out: 5, Total: 15}
	s.lastPromptTokens = 10
	s.cancelTurn = func() { cancelled = true }
	s.mu.Unlock()
	s.gate.mu.Lock()
	s.gate.always["bash"] = true
	s.gate.mu.Unlock()
	s.emit(Event{Kind: EvInfo, Text: "queued"})

	s.Reset()

	if !cancelled {
		t.Error("running turn not cancelled")
	}
	h := s.History()
	if len(h) != 1 || h[0].Role != "system" || h[0].Content != s.SystemPrompt() {
		t.Errorf("history = %+v", h)
	}
	if s.Tokens() != (Tokens{}) {
		t.Errorf("tokens = %+v", s.Tokens())
	}
	if used, _ := s.ContextSize(); used != 0 {
		t.Errorf("context used = %d", used)
	}
	if s.gate.isAlways("bash") {
		t.Error("gate not reset")
	}
	if evs := s.PollEvents(); len(evs) != 0 {
		t.Errorf("queued events kept: %+v", evs)
	}
	if s.ModelID() != "fake/m2" || s.Effort() != "high" || s.Mode() != ModeAuto {
		t.Errorf("kept settings changed: model=%q effort=%q mode=%q", s.ModelID(), s.Effort(), s.Mode())
	}

	// The gate reports prompts as EvApproval events.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res := make(chan bool, 1)
	go func() {
		ok, _ := s.gate.Check(ctx, ModeManual, MainAgentID, "bash", `{"command":"ls"}`)
		res <- ok
	}()
	ev, err := s.NextEvent(ctx)
	if err != nil || ev.Kind != EvApproval || ev.Approval == nil || ev.Tool != "bash" || ev.AgentID != MainAgentID {
		t.Fatalf("approval event = %+v, %v", ev, err)
	}
	ev.Approval.Respond(Allow)
	if !<-res {
		t.Error("allowed approval not honoured")
	}
}

func TestReasoningDisabled(t *testing.T) {
	s, _ := newTestSession(t, "")
	if !s.ReasoningEnabled() {
		t.Error("m1 should have reasoning enabled (default)")
	}
	if err := s.SetModel("fake/m2"); err != nil {
		t.Fatal(err)
	}
	if s.ReasoningEnabled() {
		t.Error("m2 has reasoning: false")
	}

	// No model configured: reasoning off, empty model, no context limit.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(base, "home"), base, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	empty, err := NewSession(Options{Config: cfg, StartDir: base})
	if err != nil {
		t.Fatalf("NewSession without models: %v", err)
	}
	if empty.ModelID() != "" || empty.ReasoningEnabled() || len(empty.Models()) != 0 {
		t.Errorf("empty session: model=%q reasoning=%v models=%v", empty.ModelID(), empty.ReasoningEnabled(), empty.Models())
	}
	if _, _, err := empty.newClient(); !errors.Is(err, ErrNoModel) {
		t.Errorf("newClient without model = %v, want ErrNoModel", err)
	}
	if _, limit := empty.ContextSize(); limit != 0 {
		t.Errorf("limit = %d", limit)
	}
}
