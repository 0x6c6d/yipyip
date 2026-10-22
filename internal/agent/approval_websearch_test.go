package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
)

const webSearchArgs = `{"query":"golang generics"}`

// recordingGate returns a gate that records every request and answers it
// with the decision returned by decide.
func recordingGate(decide func(*ApprovalRequest) Decision) (*Gate, func() []*ApprovalRequest) {
	var mu sync.Mutex
	var reqs []*ApprovalRequest
	g := NewGate(func(r *ApprovalRequest) {
		mu.Lock()
		reqs = append(reqs, r)
		mu.Unlock()
		r.Respond(decide(r))
	})
	return g, func() []*ApprovalRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]*ApprovalRequest(nil), reqs...)
	}
}

func TestWebSearchIsGated(t *testing.T) {
	if !IsGated("web_search") {
		t.Error(`IsGated("web_search") = false`)
	}
	for _, tool := range []string{"Web_Search", "web_search ", "websearch"} {
		if IsGated(tool) {
			t.Errorf("IsGated(%q) = true", tool)
		}
	}
	for _, tool := range []string{"bash", "write_file", "edit_file", "spawn_agent"} {
		if !IsGated(tool) {
			t.Errorf("IsGated(%q) = false", tool)
		}
	}
	for _, tool := range []string{"read_file", "glob", "grep", "wait_agent", "cancel_agent", "question"} {
		if IsGated(tool) {
			t.Errorf("IsGated(%q) = true", tool)
		}
	}
}

func TestWebSearchGateManualPrompt(t *testing.T) {
	g, reqs := recordingGate(func(*ApprovalRequest) Decision { return Allow })
	ok, err := g.Check(context.Background(), ModeManual, MainAgentID, "web_search", webSearchArgs)
	if err != nil || !ok {
		t.Fatalf("Check = %v, %v; want true, nil", ok, err)
	}
	got := reqs()
	if len(got) != 1 {
		t.Fatalf("prompts = %d, want 1", len(got))
	}
	r := got[0]
	if r.Tool != "web_search" || r.AgentID != MainAgentID || !strings.Contains(r.Args, "golang generics") {
		t.Fatalf("request = {AgentID:%q Tool:%q Args:%q}", r.AgentID, r.Tool, r.Args)
	}
}

func TestWebSearchGateDeny(t *testing.T) {
	ctx := context.Background()

	g, n := answering(Deny)
	if ok, _ := g.Check(ctx, ModeManual, MainAgentID, "web_search", webSearchArgs); ok {
		t.Error("Deny allowed web_search")
	}
	if n.Load() != 1 {
		t.Errorf("prompts = %d, want 1", n.Load())
	}

	if ok, _ := NewGate(nil).Check(ctx, ModeManual, MainAgentID, "web_search", webSearchArgs); ok {
		t.Error("nil notify allowed web_search")
	}

	var nilGate *Gate
	if ok, _ := nilGate.Check(ctx, ModeManual, MainAgentID, "web_search", webSearchArgs); ok {
		t.Error("nil gate allowed web_search")
	}

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	ga, na := answering(Allow)
	ok, err := ga.Check(cctx, ModeManual, MainAgentID, "web_search", webSearchArgs)
	if ok {
		t.Error("cancelled context allowed web_search")
	}
	if err == nil {
		t.Error("cancelled context: err = nil")
	}
	if na.Load() != 0 {
		t.Errorf("cancelled context prompted %d times", na.Load())
	}
}

func TestWebSearchGateAlways(t *testing.T) {
	g, n := answering(AllowAlways)
	if !mustCheck(t, g, ModeManual, "web_search") {
		t.Fatal("AllowAlways denied web_search")
	}
	for i := 0; i < 3; i++ {
		if !mustCheck(t, g, ModeManual, "web_search") {
			t.Fatal("remembered web_search denied")
		}
	}
	if n.Load() != 1 {
		t.Fatalf("web_search prompts = %d, want 1", n.Load())
	}
	if !mustCheck(t, g, ModeManual, "bash") {
		t.Fatal("bash denied")
	}
	if n.Load() != 2 {
		t.Fatalf("bash did not prompt after always for web_search: prompts = %d", n.Load())
	}

	// "always" for bash does not cover web_search.
	g2, reqs := recordingGate(func(r *ApprovalRequest) Decision {
		if r.Tool == "bash" {
			return AllowAlways
		}
		return Deny
	})
	if !mustCheck(t, g2, ModeManual, "bash") {
		t.Fatal("bash AllowAlways denied")
	}
	if mustCheck(t, g2, ModeManual, "web_search") {
		t.Fatal("always for bash let web_search through")
	}
	if got := reqs(); len(got) != 2 || got[1].Tool != "web_search" {
		t.Fatalf("web_search was not prompted after always for bash: %d prompts", len(got))
	}

	// After Reset web_search prompts again.
	g.Reset()
	before := n.Load()
	if !mustCheck(t, g, ModeManual, "web_search") {
		t.Fatal("web_search denied after Reset")
	}
	if n.Load() != before+1 {
		t.Fatalf("web_search did not prompt after Reset")
	}
}

func TestWebSearchGateAuto(t *testing.T) {
	g, n := answering(Deny)
	if !mustCheck(t, g, ModeAuto, "web_search") {
		t.Fatal("auto mode denied web_search")
	}
	if n.Load() != 0 {
		t.Fatalf("auto mode prompted %d times", n.Load())
	}
}

func TestWebSearchGateSubagent(t *testing.T) {
	ctx := context.Background()

	g, reqs := recordingGate(func(*ApprovalRequest) Decision { return Allow })
	ok, err := g.Check(ctx, ModeManual, "a1", "web_search", webSearchArgs)
	if err != nil || !ok {
		t.Fatalf("Check = %v, %v", ok, err)
	}
	if got := reqs(); len(got) != 1 || got[0].AgentID != "a1" || got[0].Tool != "web_search" {
		t.Fatalf("subagent request not labelled a1: %d prompts", len(got))
	}

	// One gate per session: "always" from the main agent covers a1.
	ga, n := answering(AllowAlways)
	if ok, err := ga.Check(ctx, ModeManual, MainAgentID, "web_search", webSearchArgs); err != nil || !ok {
		t.Fatalf("main AllowAlways = %v, %v", ok, err)
	}
	if ok, err := ga.Check(ctx, ModeManual, "a1", "web_search", webSearchArgs); err != nil || !ok {
		t.Fatalf("a1 after main always = %v, %v", ok, err)
	}
	if n.Load() != 1 {
		t.Fatalf("prompts = %d, want 1", n.Load())
	}
}
