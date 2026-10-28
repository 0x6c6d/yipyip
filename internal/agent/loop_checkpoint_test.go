package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
	"yipyip/internal/tools"
)

// toolsSinceUser counts the tool messages after the last user message of req.
func toolsSinceUser(req providertest.Request) int {
	n := 0
	for i := len(req.Messages) - 1; i >= 0 && req.Messages[i].Role != "user"; i-- {
		if req.Messages[i].Role == "tool" {
			n++
		}
	}
	return n
}

// roundsResp answers with tool rounds of two read_file calls each until
// rounds rounds are complete, then with a final text. Call ids name the round
// (r<k>a, r<k>b, k from 1).
func roundsResp(rounds int, req providertest.Request) fakeResp {
	k := toolsSinceUser(req)/2 + 1
	if k > rounds {
		return fakeResp{Text: "final"}
	}
	return fakeResp{ToolCalls: []fakeCall{
		{ID: fmt.Sprintf("r%da", k), Name: "read_file", Arguments: `{"path":"missing-a.txt"}`},
		{ID: fmt.Sprintf("r%db", k), Name: "read_file", Arguments: `{"path":"missing-b.txt"}`},
	}}
}

// checkNoDangling fails when an assistant tool call in h has no tool result
// or when h ends with an assistant message holding tool calls.
func checkNoDangling(t *testing.T, what string, h []HistoryEntry) {
	t.Helper()
	if n := len(h); n > 0 && h[n-1].Role == "assistant" && len(h[n-1].ToolCalls) > 0 {
		t.Errorf("%s: ends with unanswered tool calls: %+v", what, h)
	}
	for i, m := range h {
		for _, tc := range m.ToolCalls {
			found := false
			for _, r := range h[i+1:] {
				if r.Role == "tool" && r.ToolCallID == tc.ID {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: tool call %s has no result: %+v", what, tc.ID, h)
			}
		}
	}
}

// countKind counts the events of kind.
func countKind(evs []Event, kind EventKind) int {
	n := 0
	for _, ev := range evs {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

func TestCheckpointRounds(t *testing.T) {
	ack := make(chan struct{}, 8)
	srv := providertest.NewFunc(func(n int, req providertest.Request) fakeResp {
		if n > 0 {
			// The previous round must have been observed first.
			select {
			case <-ack:
			case <-time.After(5 * time.Second):
			}
		}
		return roundsResp(2, req)
	})
	defer srv.Close()
	s, _ := newLoopSession(t, srv)

	var lens []int
	var snaps [][]HistoryEntry
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for {
			ev, err := s.NextEvent(ctx)
			if err != nil {
				return
			}
			switch ev.Kind {
			case EvCheckpoint:
				h := s.History()
				lens = append(lens, len(h))
				snaps = append(snaps, h)
				ack <- struct{}{}
			case EvTurnDone:
				return
			}
		}
	}()
	if err := s.Submit("go"); err != nil {
		t.Fatal(err)
	}
	<-done
	waitTurn(t, s, 5*time.Second)

	// system + user, then 3 messages per tool round, then the final answer.
	want := []int{5, 8, 9}
	if fmt.Sprint(lens) != fmt.Sprint(want) {
		t.Fatalf("history lengths at checkpoints = %v, want %v", lens, want)
	}
	for k, h := range snaps[:2] {
		last := h[len(h)-3]
		if last.Role != "assistant" || len(last.ToolCalls) != 2 || last.ToolCalls[0].ID != fmt.Sprintf("r%da", k+1) {
			t.Errorf("checkpoint %d does not end with round %d: %+v", k+1, k+1, h)
		}
		checkNoDangling(t, fmt.Sprintf("checkpoint %d", k+1), h)
	}
	if h := snaps[2]; h[len(h)-1].Role != "assistant" || h[len(h)-1].Content != "final" {
		t.Errorf("final checkpoint = %+v", h)
	}
	if h := s.History(); len(h) != 9 || h[1].Content != "go" {
		t.Errorf("history after turn = %+v", h)
	}
}

func TestCheckpointPanicKeepsRounds(t *testing.T) {
	srv := providertest.NewFunc(func(_ int, req providertest.Request) fakeResp {
		if n := len(req.Messages); req.Messages[n-1].Role == "user" && req.Messages[n-1].Content == "next" {
			return fakeResp{Text: "next done"}
		}
		return roundsResp(5, req)
	})
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	s.roundHook = func(agentID string, round int) {
		if agentID == MainAgentID && round == 2 {
			panic("round 3 exploded")
		}
	}
	evs := submitAndWait(t, s, "first")
	if !findEvent(evs, EvError, "internal error: round 3 exploded") {
		t.Errorf("no internal error line: %+v", evs)
	}
	if n := countKind(evs, EvCheckpoint); n != 2 {
		t.Errorf("checkpoints = %d, want 2", n)
	}
	h := s.History()
	if len(h) != 8 || h[1].Role != "user" || h[1].Content != "first" {
		t.Fatalf("history after panic = %+v", h)
	}
	if h[2].ToolCalls[0].ID != "r1a" || h[5].ToolCalls[0].ID != "r2a" {
		t.Errorf("kept rounds = %+v", h)
	}
	checkNoDangling(t, "after panic", h)

	s.roundHook = nil
	evs = submitAndWait(t, s, "next")
	if findEvent(evs, EvError, "") {
		t.Errorf("error on the next turn: %+v", evs)
	}
	reqs := srv.Requests()
	last := reqs[len(reqs)-1]
	var ids []string
	for _, m := range last.Messages {
		if m.Role == "tool" {
			ids = append(ids, m.ToolCallID)
		}
	}
	if strings.Join(ids, ",") != "r1a,r1b,r2a,r2b" {
		t.Errorf("tool results in the next request = %v", ids)
	}
	if len(last.Messages) != 9 || last.Messages[1].Content != "first" || last.Messages[8].Content != "next" {
		t.Errorf("next request messages = %+v", last.Messages)
	}
}

func TestCheckpointNoDanglingCall(t *testing.T) {
	srv := providertest.NewFunc(func(_ int, req providertest.Request) fakeResp {
		return roundsResp(3, req)
	})
	defer srv.Close()
	s, _ := newLoopSession(t, srv)

	// At the top of every round History() holds the last checkpoint.
	var mu sync.Mutex
	var snaps [][]HistoryEntry
	s.roundHook = func(agentID string, round int) {
		if agentID == MainAgentID && round > 0 {
			mu.Lock()
			snaps = append(snaps, s.History())
			mu.Unlock()
		}
	}
	submitAndWait(t, s, "go")
	mu.Lock()
	if len(snaps) != 3 {
		t.Errorf("snapshots = %d, want 3", len(snaps))
	}
	for i, h := range snaps {
		checkNoDangling(t, fmt.Sprintf("snapshot %d", i+1), h)
	}
	mu.Unlock()
	checkNoDangling(t, "after turn", s.History())
	s.roundHook = nil

	// A panic raised while the tools of round 2 run (after its first call):
	// the session keeps the checkpoint of round 1, never the half round.
	s.mu.Lock()
	gen := s.gen
	s.history = append(s.history, provider.Message{Role: "user", Content: "again"})
	hist := copyHistory(s.history)
	base := len(hist)
	s.mu.Unlock()
	ts := s.mainTools()
	run := ts.run
	calls := 0
	ts.run = func(ctx context.Context, name, args string) tools.Result {
		calls++
		if calls == 3 {
			panic("tool round exploded")
		}
		return run(ctx, name, args)
	}
	func() {
		defer func() {
			if p := recover(); p == nil {
				t.Error("no panic")
			}
		}()
		s.runLoop(context.Background(), gen, MainAgentID, hist, ts)
	}()
	h := s.History()
	checkNoDangling(t, "after tool-round panic", h)
	if len(h) != base+3 || h[base].ToolCalls[0].ID != "r1a" {
		t.Errorf("history after tool-round panic = %+v", h)
	}
	evs := s.PollEvents()
	if n := countKind(evs, EvCheckpoint); n != 1 {
		t.Errorf("checkpoints = %d, want 1", n)
	}
}

func TestCheckpointNotForSubagent(t *testing.T) {
	srv := providertest.NewFunc(func(_ int, req providertest.Request) fakeResp {
		if isSub(req) {
			if countTools(req) == 0 {
				return fakeResp{ToolCalls: []fakeCall{{ID: "x1", Name: "read_file", Arguments: `{"path":"missing.txt"}`}}}
			}
			return fakeResp{Text: "sub done"}
		}
		return spawnWaitMain("SUB-TASK", req)
	})
	defer srv.Close()
	s := newSubSession(t, srv, "")
	s.SetMode(ModeAuto)
	var mu sync.Mutex
	subRounds := 0
	s.roundHook = func(agentID string, _ int) {
		if agentID != MainAgentID {
			mu.Lock()
			subRounds++
			mu.Unlock()
		}
	}
	evs := submitAndWait(t, s, mainPrompt)
	mu.Lock()
	if subRounds != 2 {
		t.Errorf("subagent rounds = %d, want 2", subRounds)
	}
	mu.Unlock()
	n := 0
	for _, ev := range evs {
		if ev.Kind == EvCheckpoint {
			n++
			if ev.AgentID != MainAgentID {
				t.Errorf("checkpoint for %q", ev.AgentID)
			}
		}
	}
	if n != 3 {
		t.Errorf("checkpoints = %d, want 3 (spawn, wait, final)", n)
	}
	for _, m := range s.History() {
		if m.Role == "user" && m.Content == "SUB-TASK" || m.ToolCallID == "x1" {
			t.Errorf("subagent message in the main history: %+v", m)
		}
	}
}

func TestCheckpointEvent(t *testing.T) {
	srv := providertest.NewFunc(func(_ int, req providertest.Request) fakeResp {
		return roundsResp(2, req)
	})
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	evs := submitAndWait(t, s, "go")
	if n := countKind(evs, EvCheckpoint); n != 3 {
		t.Errorf("checkpoints = %d, want 3", n)
	}
	for _, ev := range evs {
		if ev.Kind == EvCheckpoint && (ev.AgentID != MainAgentID || ev.Text != "") {
			t.Errorf("checkpoint event = %+v", ev)
		}
	}

	// A failed first round (a final 4xx) completes no round.
	bad := providertest.New(fakeResp{Status: 400, Body: "boom"})
	defer bad.Close()
	s2, _ := newLoopSession(t, bad)
	evs = submitAndWait(t, s2, "go")
	if !findEvent(evs, EvError, "400") {
		t.Errorf("no error: %+v", evs)
	}
	if n := countKind(evs, EvCheckpoint); n != 0 {
		t.Errorf("checkpoints after a failed first round = %d, want 0", n)
	}
	if h := s2.History(); len(h) != 2 {
		t.Errorf("history after failed round = %+v", h)
	}
}
