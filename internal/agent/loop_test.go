package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

type (
	fakeResp  = providertest.Response
	fakeCall  = providertest.ToolCall
	fakeUsage = providertest.Usage
)

// newLoopSession builds a session (temp HOME and start dir) talking to srv.
func newLoopSession(t *testing.T, srv *providertest.Server) (*Session, string) {
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
	if err := os.MkdirAll(filepath.Join(home, ".config", "yipyip"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
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
	s, err := NewSession(Options{Config: cfg, StartDir: dir})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() {
		s.Cancel()
		waitTurn(t, s, 5*time.Second)
	})
	return s, dir
}

// waitTurn waits at most d for the running turn and fails the test otherwise.
func waitTurn(t *testing.T, s *Session, d time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() { s.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("turn did not finish within %v", d)
	}
}

// submitAndWait submits text and waits for the turn; it returns its events.
func submitAndWait(t *testing.T, s *Session, text string) []Event {
	t.Helper()
	if err := s.Submit(text); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitTurn(t, s, 15*time.Second)
	evs := s.PollEvents()
	if len(evs) == 0 || evs[len(evs)-1].Kind != EvTurnDone {
		t.Fatalf("last event is not EvTurnDone: %+v", evs)
	}
	if s.Busy() {
		t.Fatal("Busy after Wait")
	}
	return evs
}

func findEvent(evs []Event, kind EventKind, sub string) bool {
	for _, ev := range evs {
		if ev.Kind == kind && strings.Contains(ev.Text, sub) {
			return true
		}
	}
	return false
}

func TestLoopToolCallThenAnswer(t *testing.T) {
	srv := providertest.New(
		fakeResp{ToolCalls: []fakeCall{{ID: "c1", Name: "read_file", Arguments: `{"path":"notes.txt"}`}}},
		fakeResp{Text: "all done"})
	defer srv.Close()
	s, dir := newLoopSession(t, srv)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("FILE-CONTENT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	evs := submitAndWait(t, s, "read it") // manual mode: read_file needs no approval

	var kinds []EventKind
	text := ""
	for _, ev := range evs {
		kinds = append(kinds, ev.Kind)
		if ev.AgentID != MainAgentID {
			t.Errorf("event agent id %q", ev.AgentID)
		}
		switch ev.Kind {
		case EvText:
			text += ev.Text
		case EvToolCall:
			if ev.Tool != "read_file" || ev.Args != `{"path":"notes.txt"}` {
				t.Errorf("tool call event %+v", ev)
			}
		case EvToolResult:
			if ev.Tool != "read_file" || !strings.Contains(ev.Text, "FILE-CONTENT") || ev.IsError {
				t.Errorf("tool result event %+v", ev)
			}
		case EvError, EvApproval:
			t.Errorf("unexpected event %+v", ev)
		}
	}
	if text != "all done" {
		t.Errorf("text = %q", text)
	}
	if kinds[0] != EvToolCall || kinds[1] != EvToolResult {
		t.Errorf("event order %v", kinds)
	}

	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d", len(reqs))
	}
	if reqs[0].Model != "m1" || len(reqs[0].ToolNames) == 0 {
		t.Errorf("request: model %q tools %v", reqs[0].Model, reqs[0].ToolNames)
	}
	m := reqs[1].Messages
	if len(m) != 4 || m[0].Role != "system" || m[1].Role != "user" || m[1].Content != "read it" ||
		m[2].Role != "assistant" || len(m[2].ToolCalls) != 1 || m[2].ToolCalls[0].ID != "c1" ||
		m[3].Role != "tool" || m[3].ToolCallID != "c1" || !strings.Contains(m[3].Content, "FILE-CONTENT") {
		t.Errorf("second request messages = %+v", m)
	}
	h := s.History()
	if len(h) != 5 || h[4].Role != "assistant" || h[4].Content != "all done" {
		t.Errorf("history = %+v", h)
	}
}

func TestIterationLimit(t *testing.T) {
	srv := providertest.NewFunc(func(n int, _ providertest.Request) fakeResp {
		return fakeResp{ToolCalls: []fakeCall{{ID: "c" + string(rune('a'+n%26)), Name: "glob", Arguments: `{"pattern":"*.txt"}`}}}
	})
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	evs := submitAndWait(t, s, "loop forever")
	if n := len(srv.Requests()); n != MaxIterations || MaxIterations != 50 {
		t.Fatalf("requests = %d, want 50", n)
	}
	if !findEvent(evs, EvError, "iteration limit (50) reached") {
		t.Error("no iteration limit error")
	}
	// The history stays valid: every tool call has a result.
	h := s.History()
	if last := h[len(h)-1]; last.Role != "tool" {
		t.Errorf("last message %+v", last)
	}
}

func TestCancelStopsTurn(t *testing.T) {
	srv := providertest.New(fakeResp{ToolCalls: []fakeCall{
		{ID: "c1", Name: "bash", Arguments: `{"command":"sleep 30"}`},
		{ID: "c2", Name: "glob", Arguments: `{"pattern":"*"}`},
	}})
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	s.SetMode(ModeAuto)
	if err := s.Submit("sleep"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var evs []Event
	for {
		ev, err := s.NextEvent(ctx)
		if err != nil {
			t.Fatalf("no tool call event: %v", err)
		}
		evs = append(evs, ev)
		if ev.Kind == EvToolCall {
			break
		}
	}
	time.Sleep(200 * time.Millisecond) // let bash start
	start := time.Now()
	s.Cancel()
	waitTurn(t, s, 2*time.Second)
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("cancel took %v", el)
	}
	if s.Busy() {
		t.Fatal("still busy")
	}
	evs = append(evs, s.PollEvents()...)
	if evs[len(evs)-1].Kind != EvTurnDone || !findEvent(evs, EvInfo, "cancelled") {
		t.Fatalf("events %+v", evs)
	}
	h := s.History()
	if len(h) != 5 || h[3].Role != "tool" || h[3].ToolCallID != "c1" ||
		h[4].Role != "tool" || h[4].ToolCallID != "c2" || h[4].Content != "cancelled" {
		t.Fatalf("history = %+v", h)
	}
	s.Cancel() // no-op when idle

	// The session stays usable and the next request carries the results.
	submitAndWait(t, s, "again")
	reqs := srv.Requests()
	if len(reqs) != 2 || len(reqs[1].Messages) != 6 {
		t.Fatalf("requests %d", len(reqs))
	}
}

func TestSubmitNoModel(t *testing.T) {
	s, _ := newLoopSession(t, nil)
	if err := s.Submit("hi"); !errors.Is(err, ErrNoModel) {
		t.Fatalf("Submit = %v, want ErrNoModel", err)
	}
	if s.Busy() || len(s.History()) != 1 || len(s.PollEvents()) != 0 {
		t.Error("failed Submit changed the session")
	}
	s.Wait() // idle: returns at once
}

func TestSubmitBusy(t *testing.T) {
	srv := providertest.New(fakeResp{Text: "slow", Delay: 3 * time.Second})
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	if err := s.Submit("one"); err != nil {
		t.Fatal(err)
	}
	if !s.Busy() {
		t.Error("not busy during a turn")
	}
	if err := s.Submit("two"); !errors.Is(err, ErrBusy) {
		t.Fatalf("second Submit = %v, want ErrBusy", err)
	}
	s.Cancel()
	waitTurn(t, s, 2*time.Second)
	if s.Busy() {
		t.Error("busy after the turn")
	}
	h := s.History()
	if len(h) != 2 || h[1].Content != "one" {
		t.Errorf("history = %+v", h)
	}
}

func TestTokenAccounting(t *testing.T) {
	srv := providertest.New(fakeResp{Text: "hi", Usage: &fakeUsage{Prompt: 100, Completion: 20}})
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	submitAndWait(t, s, "hello")
	if tk := s.Tokens(); tk != (Tokens{In: 100, Out: 20, Total: 120}) {
		t.Errorf("tokens = %+v", tk)
	}
	if used, limit := s.ContextSize(); used != 100 || limit != 8000 {
		t.Errorf("context = %d/%d", used, limit)
	}
}

func TestTokenEstimate(t *testing.T) {
	srv := providertest.New(fakeResp{Text: "abcdefg"}) // no usage reported
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	submitAndWait(t, s, "hello world")
	chars := len([]rune(s.SystemPrompt())) + len("hello world")
	in := (chars + 3) / 4
	out := 2 // ceil(7/4)
	if tk := s.Tokens(); tk != (Tokens{In: in, Out: out, Total: in + out}) {
		t.Errorf("tokens = %+v, want %d/%d/%d", tk, in, out, in+out)
	}
	if used, _ := s.ContextSize(); used != in {
		t.Errorf("context used = %d, want %d", used, in)
	}
}

func TestProviderErrorShown(t *testing.T) {
	// A 4xx is final (5xx would be retried by the client).
	srv := providertest.New(fakeResp{Status: 400, Body: "boom SECRET123"}, fakeResp{Text: "recovered"})
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	evs := submitAndWait(t, s, "first")
	if !findEvent(evs, EvError, "400") {
		t.Fatalf("error not shown: %+v", evs)
	}
	for _, ev := range evs {
		if strings.Contains(ev.Text, "SECRET123") {
			t.Errorf("secret in event %+v", ev)
		}
	}
	if h := s.History(); len(h) != 2 || h[1].Content != "first" {
		t.Errorf("history after error = %+v", h)
	}
	evs = submitAndWait(t, s, "second")
	text := ""
	for _, ev := range evs {
		if ev.Kind == EvText {
			text += ev.Text
		}
	}
	if text != "recovered" {
		t.Errorf("next reply = %q", text)
	}
}

func TestEffortSent(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	submitAndWait(t, s, "a")
	if err := s.SetEffort("high"); err != nil {
		t.Fatal(err)
	}
	submitAndWait(t, s, "b")
	if err := s.SetModel("fake/m2"); err != nil {
		t.Fatal(err)
	}
	submitAndWait(t, s, "c")
	reqs := srv.Requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d", len(reqs))
	}
	if reqs[0].ReasoningEffort != "medium" || reqs[1].ReasoningEffort != "high" {
		t.Errorf("effort = %q, %q", reqs[0].ReasoningEffort, reqs[1].ReasoningEffort)
	}
	if _, present := reqs[2].JSON["reasoning_effort"]; present || reqs[2].Model != "m2" {
		t.Errorf("m2 request: effort present=%v model=%q", present, reqs[2].Model)
	}
}

func TestManualDenyRunsNothing(t *testing.T) {
	srv := providertest.New(fakeResp{ToolCalls: []fakeCall{{ID: "c1", Name: "bash", Arguments: `{"command":"touch marker"}`}}},
		fakeResp{Text: "ok"})
	defer srv.Close()
	s, dir := newLoopSession(t, srv)
	if err := s.Submit("make marker"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	approvals := 0
	var evs []Event
	for {
		ev, err := s.NextEvent(ctx)
		if err != nil {
			t.Fatalf("NextEvent: %v", err)
		}
		evs = append(evs, ev)
		if ev.Kind == EvApproval {
			approvals++
			if ev.Tool != "bash" || ev.AgentID != MainAgentID {
				t.Errorf("approval %+v", ev)
			}
			ev.Approval.Respond(Deny)
		}
		if ev.Kind == EvTurnDone {
			break
		}
	}
	waitTurn(t, s, 5*time.Second)
	if approvals != 1 {
		t.Errorf("approvals = %d", approvals)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker")); !os.IsNotExist(err) {
		t.Fatalf("marker exists: %v", err)
	}
	if !findEvent(evs, EvToolResult, DeniedMessage) {
		t.Error("no denied tool result event")
	}
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d", len(reqs))
	}
	m := reqs[1].Messages
	if last := m[len(m)-1]; last.Role != "tool" || last.ToolCallID != "c1" || last.Content != DeniedMessage {
		t.Errorf("model got %+v", last)
	}
}

// TestResetDuringTurn checks that Reset cancels and waits for a running turn
// and that the stale turn does not write into the new history.
func TestResetDuringTurn(t *testing.T) {
	srv := providertest.New(fakeResp{Text: "late", Delay: 3 * time.Second})
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	if err := s.Submit("one"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.Reset(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Reset did not return")
	}
	if s.Busy() {
		t.Error("busy after Reset")
	}
	if h := s.History(); len(h) != 1 {
		t.Errorf("history after Reset = %+v", h)
	}
	if evs := s.PollEvents(); len(evs) != 0 {
		t.Errorf("events after Reset = %+v", evs)
	}
}
