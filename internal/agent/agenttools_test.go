package agent

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

const mainPrompt = "MAIN-REQUEST"

// newSubSession builds a session talking to srv; project (if not empty) is
// written as the start directory's yipyip.json.
func newSubSession(t *testing.T, srv *providertest.Server, project string) *Session {
	t.Helper()
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
	if project != "" {
		if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(project), 0o644); err != nil {
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
	t.Cleanup(func() {
		s.Cancel()
		waitTurn(t, s, 5*time.Second)
	})
	return s
}

// firstUser returns the first user message of a request: the main prompt for
// main-agent requests, the task for subagent requests.
func firstUser(req providertest.Request) string {
	for _, m := range req.Messages {
		if m.Role == "user" {
			return m.Content
		}
	}
	return ""
}

func isSub(req providertest.Request) bool {
	return firstUser(req) != mainPrompt
}

func countTools(req providertest.Request) int {
	n := 0
	for _, m := range req.Messages {
		if m.Role == "tool" {
			n++
		}
	}
	return n
}

// spawnWaitMain is a main agent that spawns task, waits for a1 and answers.
func spawnWaitMain(task string, req providertest.Request) fakeResp {
	switch countTools(req) {
	case 0:
		return fakeResp{ToolCalls: []fakeCall{{ID: "s1", Name: "spawn_agent", Arguments: `{"task":"` + task + `"}`}}}
	case 1:
		return fakeResp{ToolCalls: []fakeCall{{ID: "w1", Name: "wait_agent", Arguments: `{"id":"a1"}`}}}
	}
	return fakeResp{Text: "main done"}
}

func toolResults(evs []Event, tool string) []Event {
	var out []Event
	for _, ev := range evs {
		if ev.Kind == EvToolResult && ev.Tool == tool {
			out = append(out, ev)
		}
	}
	return out
}

// waitCond polls cond for at most d.
func waitCond(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSpawnToolReturnsImmediately(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	var sp atomic.Pointer[Session]
	var statusAtResult atomic.Value
	srv := providertest.NewFunc(func(n int, req providertest.Request) fakeResp {
		if isSub(req) {
			select {
			case <-release:
			case <-time.After(5 * time.Second):
			}
			return fakeResp{Text: "sub done"}
		}
		if countTools(req) == 1 {
			// The spawn result is in the history; the subagent still blocks.
			if subs := sp.Load().Subagents(); len(subs) == 1 {
				statusAtResult.Store(subs[0].Status)
			}
			once.Do(func() { close(release) })
		}
		return spawnWaitMain("SUB-TASK", req)
	})
	defer srv.Close()
	s := newSubSession(t, srv, "")
	sp.Store(s)
	s.SetMode(ModeAuto)
	evs := submitAndWait(t, s, mainPrompt)

	res := toolResults(evs, "spawn_agent")
	if len(res) != 1 || res[0].IsError || res[0].Text != "subagent a1 started" || res[0].AgentID != MainAgentID {
		t.Fatalf("spawn results %+v", res)
	}
	if st, _ := statusAtResult.Load().(SubStatus); st != SubRunning {
		t.Fatalf("subagent status when the spawn result arrived = %q, want running", st)
	}
	if w := toolResults(evs, "wait_agent"); len(w) != 1 || w[0].Text != "sub done" {
		t.Fatalf("wait results %+v", w)
	}
}

func TestSpawnLimitError(t *testing.T) {
	srv := providertest.NewFunc(func(n int, req providertest.Request) fakeResp {
		if isSub(req) {
			return fakeResp{Text: "slow", Delay: 10 * time.Second}
		}
		if countTools(req) == 0 {
			return fakeResp{ToolCalls: []fakeCall{
				{ID: "s1", Name: "spawn_agent", Arguments: `{"task":"SUB-1"}`},
				{ID: "s2", Name: "spawn_agent", Arguments: `{"task":"SUB-2"}`}}}
		}
		return fakeResp{Text: "main done"}
	})
	defer srv.Close()
	s := newSubSession(t, srv, `{"max_subagents": 1}`)
	s.SetMode(ModeAuto)
	evs := submitAndWait(t, s, mainPrompt)

	res := toolResults(evs, "spawn_agent")
	if len(res) != 2 {
		t.Fatalf("spawn results %+v", res)
	}
	if res[0].IsError || res[0].Text != "subagent a1 started" {
		t.Fatalf("first spawn %+v", res[0])
	}
	if !res[1].IsError || !strings.Contains(res[1].Text, "limit") {
		t.Fatalf("second spawn must be an error result: %+v", res[1])
	}
	subs := s.Subagents()
	if len(subs) != 1 || subs[0].ID != "a1" {
		t.Fatalf("Subagents = %+v", subs)
	}
	// The turn ended normally: its still running subagent was cancelled.
	if subs[0].Status != SubCancelled {
		t.Fatalf("subagent status after the turn = %q", subs[0].Status)
	}
	// An unknown model id is an error result too, and spawns nothing.
	srv2 := providertest.NewFunc(func(n int, req providertest.Request) fakeResp {
		if countTools(req) == 0 {
			return fakeResp{ToolCalls: []fakeCall{{ID: "s1", Name: "spawn_agent", Arguments: `{"task":"X","model":"fake/nope"}`}}}
		}
		return fakeResp{Text: "main done"}
	})
	defer srv2.Close()
	s2 := newSubSession(t, srv2, "")
	s2.SetMode(ModeAuto)
	res = toolResults(submitAndWait(t, s2, mainPrompt), "spawn_agent")
	if len(res) != 1 || !res[0].IsError || !strings.Contains(res[0].Text, "unknown model") || len(s2.Subagents()) != 0 {
		t.Fatalf("unknown model spawn %+v, subagents %+v", res, s2.Subagents())
	}
}

func TestSubagentNoSpawnTool(t *testing.T) {
	srv := providertest.NewFunc(func(n int, req providertest.Request) fakeResp {
		if isSub(req) {
			if countTools(req) == 0 {
				// The model invents spawn_agent although it is not offered.
				return fakeResp{ToolCalls: []fakeCall{{ID: "x1", Name: "spawn_agent", Arguments: `{"task":"NESTED"}`}}}
			}
			return fakeResp{Text: "sub done"}
		}
		return spawnWaitMain("SUB-TASK", req)
	})
	defer srv.Close()
	s := newSubSession(t, srv, "")
	// Manual mode: the main spawn needs approval; the invented one must be
	// refused without a prompt.
	var mu sync.Mutex
	var approvals []string
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	var evs []Event
	go func() {
		for {
			ev, err := s.NextEvent(ctx)
			if err != nil {
				return
			}
			mu.Lock()
			evs = append(evs, ev)
			if ev.Kind == EvApproval {
				approvals = append(approvals, ev.AgentID+":"+ev.Tool)
				ev.Approval.Respond(Allow)
			}
			mu.Unlock()
		}
	}()
	if err := s.Submit(mainPrompt); err != nil {
		t.Fatal(err)
	}
	waitTurn(t, s, 15*time.Second)
	waitCond(t, 5*time.Second, "EvTurnDone", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(evs) > 0 && evs[len(evs)-1].Kind == EvTurnDone
	})
	mu.Lock()
	defer mu.Unlock()

	if !slices.Equal(approvals, []string{"main:spawn_agent"}) {
		t.Fatalf("approvals = %v", approvals)
	}
	subReqs := 0
	for _, r := range srv.Requests() {
		if !isSub(r) {
			if !slices.Contains(r.ToolNames, "spawn_agent") {
				t.Errorf("main request lacks spawn_agent: %v", r.ToolNames)
			}
			continue
		}
		subReqs++
		if slices.Contains(r.ToolNames, "spawn_agent") {
			t.Errorf("subagent request offers spawn_agent: %v", r.ToolNames)
		}
		for _, want := range []string{"read_file", "bash", "wait_agent", "cancel_agent"} {
			if !slices.Contains(r.ToolNames, want) {
				t.Errorf("subagent request lacks %s: %v", want, r.ToolNames)
			}
		}
	}
	if subReqs != 2 {
		t.Fatalf("subagent requests = %d", subReqs)
	}
	refused := false
	for _, ev := range evs {
		if ev.Kind == EvToolResult && ev.AgentID == "a1" && ev.Tool == "spawn_agent" && ev.IsError {
			refused = true
		}
	}
	if !refused {
		t.Fatal("invented spawn_agent of a subagent was not refused with an error result")
	}
	if subs := s.Subagents(); len(subs) != 1 {
		t.Fatalf("Subagents = %+v", subs)
	}
}

func TestWaitAgentReturnsFinal(t *testing.T) {
	srv := providertest.NewFunc(func(n int, req providertest.Request) fakeResp {
		if isSub(req) {
			return fakeResp{Text: "SUB-FINAL key SECRET123"}
		}
		return spawnWaitMain("do the SUB-TASK", req)
	})
	defer srv.Close()
	s := newSubSession(t, srv, "")
	s.SetMode(ModeAuto)
	evs := submitAndWait(t, s, mainPrompt)

	reqs := srv.Requests()
	var subReq *providertest.Request
	for i := range reqs {
		if isSub(reqs[i]) {
			subReq = &reqs[i]
		}
	}
	if subReq == nil {
		t.Fatal("no subagent request")
	}
	if len(subReq.Messages) != 2 || subReq.Messages[0].Role != "system" ||
		!strings.Contains(subReq.Messages[0].Content, "You are subagent a1; finish with a short summary.") ||
		subReq.Messages[1].Role != "user" || subReq.Messages[1].Content != "do the SUB-TASK" {
		t.Fatalf("subagent history %+v", subReq.Messages)
	}
	last := reqs[len(reqs)-1]
	found := false
	for _, m := range last.Messages {
		if m.Role == "tool" && m.ToolCallID == "w1" {
			found = true
			if !strings.Contains(m.Content, "SUB-FINAL") || strings.Contains(m.Content, "SECRET123") {
				t.Fatalf("wait_agent result %q", m.Content)
			}
		}
	}
	if !found {
		t.Fatal("no wait_agent result in the history")
	}
	subs := s.Subagents()
	if len(subs) != 1 || subs[0].Status != SubDone || !strings.Contains(subs[0].Final, "SUB-FINAL") || subs[0].Model != "fake/m1" {
		t.Fatalf("Subagents = %+v", subs)
	}
	tr, ok := s.SubagentTranscript("a1")
	if !ok || len(tr) == 0 || !strings.Contains(strings.Join(tr, "\n"), "SUB-FINAL") {
		t.Fatalf("transcript %v, %v", tr, ok)
	}
	for _, l := range tr {
		if strings.Contains(l, "SECRET123") {
			t.Fatalf("transcript leaks the secret: %q", l)
		}
	}
	var subEvs []string
	textByAgent := map[string]string{}
	for _, ev := range evs {
		switch ev.Kind {
		case EvSubagent:
			subEvs = append(subEvs, ev.Text)
		case EvText:
			textByAgent[ev.AgentID] += ev.Text
		}
	}
	if !slices.Equal(subEvs, []string{"a1 running", "a1 done"}) {
		t.Fatalf("EvSubagent texts %v", subEvs)
	}
	if !strings.Contains(textByAgent["a1"], "SUB-FINAL") || textByAgent[MainAgentID] != "main done" {
		t.Fatalf("text by agent %v", textByAgent)
	}
	// /new starts a new manager: ids restart at a1.
	s.Reset()
	if len(s.Subagents()) != 0 {
		t.Fatalf("Subagents after Reset = %+v", s.Subagents())
	}
	evs = submitAndWait(t, s, mainPrompt)
	if res := toolResults(evs, "spawn_agent"); len(res) != 1 || res[0].Text != "subagent a1 started" {
		t.Fatalf("spawn after Reset %+v", res)
	}
}

func TestCancelAgentTool(t *testing.T) {
	var sp atomic.Pointer[Session]
	var statusAfterCancel atomic.Value
	srv := providertest.NewFunc(func(n int, req providertest.Request) fakeResp {
		if isSub(req) {
			return fakeResp{Text: "never", Delay: 10 * time.Second}
		}
		switch countTools(req) {
		case 0:
			return fakeResp{ToolCalls: []fakeCall{{ID: "s1", Name: "spawn_agent", Arguments: `{"task":"SUB-TASK"}`}}}
		case 1:
			return fakeResp{ToolCalls: []fakeCall{{ID: "c1", Name: "cancel_agent", Arguments: `{"id":"a1"}`}}}
		}
		if subs := sp.Load().Subagents(); len(subs) == 1 {
			statusAfterCancel.Store(subs[0].Status)
		}
		return fakeResp{Text: "main done"}
	})
	defer srv.Close()
	s := newSubSession(t, srv, "")
	sp.Store(s)
	s.SetMode(ModeAuto)
	evs := submitAndWait(t, s, mainPrompt)

	res := toolResults(evs, "cancel_agent")
	if len(res) != 1 || res[0].IsError || res[0].Text != "cancelled a1" {
		t.Fatalf("cancel results %+v", res)
	}
	if st, _ := statusAfterCancel.Load().(SubStatus); st != SubCancelled {
		t.Fatalf("status after cancel_agent = %q", st)
	}
	if err := s.CancelSubagent("a9"); err == nil {
		t.Fatal("CancelSubagent of an unknown id must fail")
	}
	if err := s.CancelSubagent("a1"); err != nil {
		t.Fatalf("CancelSubagent of a finished subagent: %v", err)
	}
}

func TestSubagentTokensAdded(t *testing.T) {
	srv := providertest.NewFunc(func(n int, req providertest.Request) fakeResp {
		if isSub(req) {
			return fakeResp{Text: "sub done", Usage: &fakeUsage{Prompt: 10, Completion: 5}}
		}
		r := spawnWaitMain("SUB-TASK", req)
		k := countTools(req) + 1
		r.Usage = &fakeUsage{Prompt: 100 * k, Completion: 10 * k}
		return r
	})
	defer srv.Close()
	s := newSubSession(t, srv, "")
	s.SetMode(ModeAuto)
	submitAndWait(t, s, mainPrompt)

	tk := s.Tokens()
	if tk.In != 610 || tk.Out != 65 || tk.Total != 675 {
		t.Fatalf("Tokens = %+v, want In 610 Out 65 Total 675", tk)
	}
	if subs := s.Subagents(); len(subs) != 1 || subs[0].Tokens != 15 {
		t.Fatalf("Subagents = %+v", subs)
	}
	if used, _ := s.ContextSize(); used != 300 {
		t.Fatalf("context used = %d, want the main agent's 300", used)
	}
}

func TestSubagentApprovalLabelled(t *testing.T) {
	srv := providertest.NewFunc(func(n int, req providertest.Request) fakeResp {
		if isSub(req) {
			if countTools(req) == 0 {
				return fakeResp{ToolCalls: []fakeCall{{ID: "b1", Name: "bash", Arguments: `{"command":"echo sub-ok"}`}}}
			}
			return fakeResp{Text: "sub done"}
		}
		return spawnWaitMain("SUB-TASK", req)
	})
	defer srv.Close()
	s := newSubSession(t, srv, "") // manual mode
	var mu sync.Mutex
	var evs []Event
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() {
		for {
			ev, err := s.NextEvent(ctx)
			if err != nil {
				return
			}
			mu.Lock()
			evs = append(evs, ev)
			mu.Unlock()
			if ev.Kind == EvApproval {
				ev.Approval.Respond(Allow)
			}
		}
	}()
	if err := s.Submit(mainPrompt); err != nil {
		t.Fatal(err)
	}
	waitTurn(t, s, 15*time.Second)
	waitCond(t, 5*time.Second, "EvTurnDone", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(evs) > 0 && evs[len(evs)-1].Kind == EvTurnDone
	})
	mu.Lock()
	defer mu.Unlock()
	var approvals []string
	bashOK := false
	for _, ev := range evs {
		if ev.Kind == EvApproval {
			approvals = append(approvals, ev.AgentID+":"+ev.Tool)
			if ev.Approval.AgentID != ev.AgentID {
				t.Errorf("approval request agent %q, event agent %q", ev.Approval.AgentID, ev.AgentID)
			}
		}
		if ev.Kind == EvToolResult && ev.Tool == "bash" {
			bashOK = ev.AgentID == "a1" && strings.Contains(ev.Text, "sub-ok") && !ev.IsError
		}
	}
	if !slices.Equal(approvals, []string{"main:spawn_agent", "a1:bash"}) {
		t.Fatalf("approvals = %v", approvals)
	}
	if !bashOK {
		t.Fatal("subagent bash result missing or not labelled a1")
	}
}

func TestEscCancelsSubagents(t *testing.T) {
	srv := providertest.NewFunc(func(n int, req providertest.Request) fakeResp {
		if isSub(req) {
			return fakeResp{Text: "never", Delay: 30 * time.Second}
		}
		return spawnWaitMain("SUB-TASK", req)
	})
	defer srv.Close()
	s := newSubSession(t, srv, "")
	s.SetMode(ModeAuto)
	if err := s.Submit(mainPrompt); err != nil {
		t.Fatal(err)
	}
	// Wait until the main agent is inside wait_agent and the subagent's
	// request is pending.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var evs []Event
	for {
		ev, err := s.NextEvent(ctx)
		if err != nil {
			t.Fatalf("no wait_agent call: %v", err)
		}
		evs = append(evs, ev)
		if ev.Kind == EvToolCall && ev.Tool == "wait_agent" {
			break
		}
	}
	waitCond(t, 5*time.Second, "subagent request", func() bool {
		for _, r := range srv.Requests() {
			if isSub(r) {
				return true
			}
		}
		return false
	})
	start := time.Now()
	s.Cancel()
	waitTurn(t, s, 3*time.Second)
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("cancel took %v", d)
	}
	subs := s.Subagents()
	if len(subs) != 1 || subs[0].Status != SubCancelled {
		t.Fatalf("Subagents after Esc = %+v", subs)
	}
	evs = append(evs, s.PollEvents()...)
	if evs[len(evs)-1].Kind != EvTurnDone {
		t.Fatalf("last event %+v", evs[len(evs)-1])
	}
	if !findEvent(evs, EvInfo, CancelledMessage) || !findEvent(evs, EvSubagent, "a1 cancelled") {
		t.Fatalf("events %+v", evs)
	}
	if w := toolResults(evs, "wait_agent"); len(w) != 1 || !w[0].IsError {
		t.Fatalf("wait_agent results %+v", w)
	}
}
