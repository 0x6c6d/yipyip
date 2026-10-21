package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// answering returns a gate whose notify answers every request with d and
// counts the prompts.
func answering(d Decision) (*Gate, *atomic.Int64) {
	var n atomic.Int64
	g := NewGate(func(r *ApprovalRequest) {
		n.Add(1)
		r.Respond(d)
	})
	return g, &n
}

func mustCheck(t *testing.T, g *Gate, mode Mode, tool string) bool {
	t.Helper()
	ok, err := g.Check(context.Background(), mode, MainAgentID, tool, "{}")
	if err != nil {
		t.Fatalf("Check(%s, %s): %v", mode, tool, err)
	}
	return ok
}

func TestGateManualPromptsGated(t *testing.T) {
	for _, tool := range []string{"bash", "write_file", "edit_file", "spawn_agent"} {
		if !IsGated(tool) {
			t.Errorf("IsGated(%q) = false", tool)
		}
		g, n := answering(Allow)
		if !mustCheck(t, g, ModeManual, tool) {
			t.Errorf("%s: allowed answer was not honoured", tool)
		}
		if n.Load() != 1 {
			t.Errorf("%s: prompted %d times, want 1", tool, n.Load())
		}
		// An unknown or empty mode is treated like manual.
		for _, m := range []Mode{"", "bogus"} {
			g, n := answering(Deny)
			if mustCheck(t, g, m, tool) || n.Load() != 1 {
				t.Errorf("%s in mode %q: must prompt and deny (asked %d)", tool, m, n.Load())
			}
		}
	}
}

func TestGateReadsNeverPrompt(t *testing.T) {
	g, n := answering(Deny)
	for _, tool := range []string{"read_file", "glob", "grep", "wait_agent", "cancel_agent"} {
		if IsGated(tool) {
			t.Errorf("IsGated(%q) = true", tool)
		}
		if !mustCheck(t, g, ModeManual, tool) {
			t.Errorf("%s denied", tool)
		}
	}
	if n.Load() != 0 {
		t.Errorf("prompted %d times for reads", n.Load())
	}
}

func TestGateDeny(t *testing.T) {
	g, n := answering(Deny)
	if mustCheck(t, g, ModeManual, "bash") {
		t.Fatal("denied bash was allowed")
	}
	if mustCheck(t, g, ModeManual, "bash") || n.Load() != 2 {
		t.Fatalf("deny must not be remembered as allow; asked %d", n.Load())
	}
	// Out-of-range decisions are treated as Deny.
	for _, d := range []Decision{-1, 3, 99} {
		g, _ := answering(d)
		if mustCheck(t, g, ModeManual, "bash") {
			t.Errorf("decision %d allowed bash", d)
		}
	}
	// A nil notify denies.
	if mustCheck(t, NewGate(nil), ModeManual, "bash") {
		t.Error("nil notify allowed bash")
	}
	// A nil Gate denies gated tools in every mode but allows ungated ones.
	var ng *Gate
	for _, m := range []Mode{ModeManual, ModeAuto} {
		if mustCheck(t, ng, m, "bash") {
			t.Errorf("nil gate allowed bash in %s mode", m)
		}
	}
	if !mustCheck(t, ng, ModeManual, "read_file") {
		t.Error("nil gate denied read_file")
	}
	ng.Reset() // must not panic
	if DeniedMessage != "denied by user" {
		t.Errorf("DeniedMessage = %q", DeniedMessage)
	}
}

func TestGateAlwaysPerTool(t *testing.T) {
	var answer atomic.Int64
	var n atomic.Int64
	g := NewGate(func(r *ApprovalRequest) {
		n.Add(1)
		go r.Respond(Decision(answer.Load()))
	})
	answer.Store(int64(AllowAlways))
	if !mustCheck(t, g, ModeManual, "bash") || !mustCheck(t, g, ModeManual, "bash") || n.Load() != 1 {
		t.Fatalf("always must allow bash and suppress later prompts (asked %d)", n.Load())
	}
	// Shared by subagents.
	if ok, err := g.Check(context.Background(), ModeManual, "a1", "bash", "{}"); !ok || err != nil || n.Load() != 1 {
		t.Fatalf("always must cover subagents: ok=%v err=%v asked=%d", ok, err, n.Load())
	}
	answer.Store(int64(Allow))
	if !mustCheck(t, g, ModeManual, "write_file") || n.Load() != 2 {
		t.Fatalf("always for bash must not cover write_file (asked %d)", n.Load())
	}
	if !mustCheck(t, g, ModeManual, "write_file") || n.Load() != 3 {
		t.Fatalf("plain allow must not be remembered (asked %d)", n.Load())
	}

	// Concurrent checks each get their own request.
	answer.Store(int64(Allow))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, err := g.Check(context.Background(), ModeManual, "a2", "edit_file", "{}"); !ok || err != nil {
				t.Errorf("concurrent check: ok=%v err=%v", ok, err)
			}
		}()
	}
	wg.Wait()
	if n.Load() != 11 {
		t.Fatalf("concurrent prompts = %d, want 8 more than 3", n.Load())
	}
}

func TestGateAutoNeverPrompts(t *testing.T) {
	g, n := answering(Deny)
	for _, tool := range []string{"bash", "write_file", "edit_file", "spawn_agent", "read_file"} {
		if !mustCheck(t, g, ModeAuto, tool) {
			t.Errorf("%s denied in auto mode", tool)
		}
	}
	if n.Load() != 0 {
		t.Errorf("prompted %d times in auto mode", n.Load())
	}
	// Switching back to manual prompts again.
	if mustCheck(t, g, ModeManual, "bash") || n.Load() != 1 {
		t.Errorf("manual after auto must prompt (asked %d)", n.Load())
	}
}

func TestGateReset(t *testing.T) {
	g, n := answering(AllowAlways)
	mustCheck(t, g, ModeManual, "bash")
	mustCheck(t, g, ModeManual, "bash")
	if n.Load() != 1 {
		t.Fatalf("asked %d, want 1", n.Load())
	}
	g.Reset()
	if !mustCheck(t, g, ModeManual, "bash") || n.Load() != 2 {
		t.Fatalf("Reset must forget always (asked %d)", n.Load())
	}
}

func TestGateCancel(t *testing.T) {
	var req atomic.Pointer[ApprovalRequest]
	g := NewGate(func(r *ApprovalRequest) { req.Store(r) })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	ok, err := g.Check(ctx, ModeManual, MainAgentID, "bash", "{}")
	if ok || err == nil {
		t.Fatalf("unanswered prompt: ok=%v err=%v", ok, err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Check took %v", d)
	}
	// A late answer is ignored and must not block.
	if r := req.Load(); r != nil {
		r.Respond(Allow)
	}

	// An answer racing with an already cancelled context never allows.
	cctx, ccancel := context.WithCancel(context.Background())
	g2 := NewGate(func(r *ApprovalRequest) {
		ccancel()
		r.Respond(Allow)
	})
	if ok, err := g2.Check(cctx, ModeManual, MainAgentID, "bash", "{}"); ok || err == nil {
		t.Fatalf("answer after cancel: ok=%v err=%v", ok, err)
	}
	// Already cancelled context: no prompt, error.
	var n atomic.Int64
	g3 := NewGate(func(r *ApprovalRequest) { n.Add(1); r.Respond(Allow) })
	if ok, err := g3.Check(cctx, ModeManual, MainAgentID, "bash", "{}"); ok || err == nil || n.Load() != 0 {
		t.Fatalf("cancelled ctx: ok=%v err=%v asked=%d", ok, err, n.Load())
	}
}

func TestGateLabel(t *testing.T) {
	reqs := make(chan *ApprovalRequest, 1)
	g := NewGate(func(r *ApprovalRequest) { reqs <- r })
	done := make(chan bool, 1)
	go func() {
		ok, _ := g.Check(context.Background(), ModeManual, "a1", "bash", `{"command":"ls"}`)
		done <- ok
	}()
	var r *ApprovalRequest
	select {
	case r = <-reqs:
	case <-time.After(2 * time.Second):
		t.Fatal("no request")
	}
	if r.AgentID != "a1" || r.Tool != "bash" || r.Args != `{"command":"ls"}` {
		t.Fatalf("request = %+v", r)
	}
	r.Respond(Allow)
	r.Respond(Deny)
	r.Respond(Deny)
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("first answer (Allow) did not win")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Check did not return after Respond")
	}
	// Respond on a request not created by a Gate is a harmless no-op.
	(&ApprovalRequest{}).Respond(Allow)
	var nr *ApprovalRequest
	nr.Respond(Allow)
}
