package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

// historyChars counts the characters of exported history entries the same way
// messageChars counts provider messages (contents + tool-call arguments).
func historyChars(h []HistoryEntry) int {
	n := 0
	for _, m := range h {
		n += len([]rune(m.Content))
		for _, tc := range m.ToolCalls {
			n += len([]rune(tc.Arguments))
		}
	}
	return n
}

// TestExportAfterTurn runs one scripted turn containing a tool call whose
// result reads a secret from a file: no view ever shows the secret (the
// tool result enters the conversation already redacted), and the tool call
// and result are the last history entries.
func TestExportAfterTurn(t *testing.T) {
	srv := providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		if n := len(req.Messages); n > 1 && req.Messages[n-1].Role == "user" && req.Messages[n-1].Content == "run" {
			return fakeResp{Text: "running", ToolCalls: []fakeCall{
				{ID: "c1", Name: "bash", Arguments: `{"command":"cat note.txt"}`},
			}}
		}
		return fakeResp{Text: "done"}
	})
	defer srv.Close()
	s, dir := newLoopSession(t, srv)
	s.SetMode(ModeAuto)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("the key is SECRET123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	evs := submitAndWait(t, s, "run")
	var sawCall bool
	for _, ev := range evs {
		if ev.Kind == EvToolCall && ev.Tool == "bash" {
			sawCall = true
		}
	}
	if !sawCall || !findEvent(evs, EvToolResult, "[redacted]") {
		t.Fatalf("tool call did not run: %+v", evs)
	}

	// The secret never leaves the session: the tool result that read
	// note.txt (which contains SECRET123) enters the conversation already
	// redacted by the tools layer, so the exported history shows
	// "[redacted]" just like the event view.
	h := s.History()
	if len(h) < 4 {
		t.Fatalf("history = %+v", h)
	}
	for i, m := range h {
		if strings.Contains(m.Content, "SECRET123") {
			t.Errorf("secret leaked into exported history h[%d]: %q", i, m.Content)
		}
		for _, tc := range m.ToolCalls {
			if strings.Contains(tc.Arguments, "SECRET123") {
				t.Errorf("secret leaked into exported tool call %+v", tc)
			}
		}
	}
	last := h[len(h)-1]
	if last.Role != "assistant" || last.Content != "done" || len(last.ToolCalls) != 0 {
		t.Errorf("final assistant entry = %+v", last)
	}
	// The tool call and its result are the entries before the final answer.
	result := h[len(h)-2]
	if result.Role != "tool" || result.ToolCallID != "c1" || !strings.Contains(result.Content, "[redacted]") {
		t.Errorf("tool result entry = %+v", result)
	}
	if !strings.Contains(result.Content, "the key is [redacted]") {
		t.Errorf("tool result not exported with the secret masked: %q", result.Content)
	}
	// The assistant's tool call is exported with its arguments.
	assistant := h[len(h)-3]
	if assistant.Role != "assistant" || len(assistant.ToolCalls) != 1 ||
		assistant.ToolCalls[0].ID != "c1" || assistant.ToolCalls[0].Name != "bash" ||
		!strings.Contains(assistant.ToolCalls[0].Arguments, "cat note.txt") {
		t.Errorf("assistant tool call entry = %+v", assistant)
	}
	// The same result appears redacted in the event stream (the view).
	var sawRedacted, sawPlain bool
	for _, ev := range evs {
		if ev.Kind == EvToolResult && ev.Tool == "bash" {
			if strings.Contains(ev.Text, "SECRET123") {
				sawPlain = true
			}
			if strings.Contains(ev.Text, "[redacted]") {
				sawRedacted = true
			}
		}
	}
	if sawPlain || !sawRedacted {
		t.Errorf("event view redaction wrong: plain=%v redacted=%v", sawPlain, sawRedacted)
	}
}

// TestExportTokenCountsAndModel asserts the exported token counters, last
// context size and model/effort/mode strings match the session's own view.
func TestExportTokenCountsAndModel(t *testing.T) {
	srv := providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		return fakeResp{Text: "answer", Usage: &fakeUsage{Prompt: 11, Completion: 7}}
	})
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	if err := s.SetModel("fake/m2"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if err := s.SetEffort(EffortHigh); err != nil {
		t.Fatalf("SetEffort: %v", err)
	}
	s.SetMode(ModeAuto)
	submitAndWait(t, s, "hello")

	in, out, total := s.TokenCounts()
	tok := s.Tokens()
	if in != int64(tok.In) || out != int64(tok.Out) || total != int64(tok.Total) {
		t.Errorf("TokenCounts = %d/%d/%d, session reports %+v", in, out, total, tok)
	}
	if tok.In == 0 || tok.Out == 0 || tok.Total == 0 {
		t.Errorf("no tokens accounted: %+v", tok)
	}
	if used, _ := s.ContextSize(); used != s.LastContextTokens() {
		t.Errorf("LastContextTokens = %d, context used = %d", s.LastContextTokens(), used)
	}
	if s.ModelName() != s.ModelID() || s.ModelName() != "fake/m2" {
		t.Errorf("ModelName = %q, ModelID = %q", s.ModelName(), s.ModelID())
	}
	if s.Effort() != EffortHigh {
		t.Errorf("Effort = %q", s.Effort())
	}
	if s.Mode() != ModeAuto {
		t.Errorf("Mode = %q", s.Mode())
	}
}

// TestInterruptedTurnExportedAsFinished cancels a running turn: TurnState
// reports it finished and interrupted (never resumable), and History keeps
// the messages produced until the cancel.
func TestInterruptedTurnExportedAsFinished(t *testing.T) {
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
	waitFor := func(kind EventKind, tool string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			for _, ev := range s.PollEvents() {
				if ev.Kind == kind && (tool == "" || ev.Tool == tool) {
					return
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("no %v event for %q", kind, tool)
	}
	waitFor(EvToolCall, "bash")
	time.Sleep(200 * time.Millisecond) // let bash start
	if _, started, _ := s.TurnState(); started == "" {
		t.Error("TurnState reports no turn start while running")
	}
	s.Cancel()
	waitTurn(t, s, 2*time.Second)

	active, started, interrupted := s.TurnState()
	if active || !interrupted {
		t.Errorf("TurnState = active=%v interrupted=%v, want finished+interrupted", active, interrupted)
	}
	if started == "" {
		t.Error("interrupted turn lost its start marker")
	}
	h := s.History()
	if len(h) != 5 || h[1].Role != "user" || h[2].Role != "assistant" ||
		h[3].Role != "tool" || h[3].ToolCallID != "c1" || h[4].Role != "tool" || h[4].ToolCallID != "c2" {
		t.Fatalf("history after cancel = %+v", h)
	}
	if !strings.Contains(h[4].Content, "cancelled") {
		t.Errorf("open call not closed as cancelled: %+v", h[4])
	}
	// The session keeps working: a new submit is not refused.
	submitAndWait(t, s, "again")
}

// TestRestoreRoundTrip exports a used session's data, restores it into a
// fresh session, and restores it again over the used session's own state.
func TestRestoreRoundTrip(t *testing.T) {
	srv := providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		if n := len(req.Messages); n > 1 && req.Messages[n-1].Role == "user" && req.Messages[n-1].Content == "run" {
			return fakeResp{Text: "calling", ToolCalls: []fakeCall{
				{ID: "c1", Name: "bash", Arguments: `{"command":"echo hi"}`},
			}, Usage: &fakeUsage{Prompt: 13, Completion: 5}}
		}
		return fakeResp{Text: "answer", Usage: &fakeUsage{Prompt: 9, Completion: 3}}
	})
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	s.SetMode(ModeAuto)
	submitAndWait(t, s, "run")
	submitAndWait(t, s, "more")

	exported := s.History()
	in, out, total := s.TokenCounts()
	lastCtx := s.LastContextTokens()
	model, effort, mode := s.ModelName(), s.Effort(), s.Mode()
	if len(exported) < 7 {
		t.Fatalf("exported history too short: %d entries", len(exported))
	}

	// Restore into a fresh session built the same way.
	other, _ := newLoopSession(t, srv)
	other.RestoreHistory(exported)
	other.RestoreTokens(in, out, total, lastCtx)
	other.RestoreModelEffortMode(model, effort, mode)
	got := other.History()
	if len(got) != len(exported) {
		t.Fatalf("restored history len = %d, want %d", len(got), len(exported))
	}
	for i := range exported {
		if got[i].Role != exported[i].Role || got[i].Content != exported[i].Content ||
			got[i].ToolCallID != exported[i].ToolCallID || len(got[i].ToolCalls) != len(exported[i].ToolCalls) {
			t.Errorf("restored h[%d] = %+v, want %+v", i, got[i], exported[i])
			break
		}
		for j := range exported[i].ToolCalls {
			if got[i].ToolCalls[j] != exported[i].ToolCalls[j] {
				t.Errorf("restored h[%d].ToolCalls[%d] = %+v, want %+v", i, j, got[i].ToolCalls[j], exported[i].ToolCalls[j])
			}
		}
	}
	if gin, gout, gtotal := other.TokenCounts(); gin != in || gout != out || gtotal != total {
		t.Errorf("restored tokens = %d/%d/%d, want %d/%d/%d", gin, gout, gtotal, in, out, total)
	}
	if other.LastContextTokens() != lastCtx {
		t.Errorf("restored last context = %d, want %d", other.LastContextTokens(), lastCtx)
	}
	if other.ModelName() != model || other.Effort() != effort || other.Mode() != mode {
		t.Errorf("restored model/effort/mode = %q/%q/%q, want %q/%q/%q",
			other.ModelName(), other.Effort(), other.Mode(), model, effort, mode)
	}
	// A restored session is usable: the next request carries the restored
	// history to the provider.
	submitAndWait(t, other, "after restore")
	reqs := srv.Requests()
	last := reqs[len(reqs)-1]
	if len(last.Messages) != len(exported)+1 {
		t.Errorf("last request has %d messages, want %d", len(last.Messages), len(exported)+1)
	}

	// Restore again over the used session's own state: no panic, applied.
	s.RestoreHistory(exported)
	s.RestoreTokens(in, out, total, lastCtx)
	s.RestoreModelEffortMode(model, effort, mode)
	if len(s.History()) != len(exported) {
		t.Errorf("re-restored history len = %d", len(s.History()))
	}
	if gin, gout, gtotal := s.TokenCounts(); gin != in || gout != out || gtotal != total {
		t.Errorf("re-restored tokens = %d/%d/%d, want %d/%d/%d", gin, gout, gtotal, in, out, total)
	}

	// Restored history is a copy: mutating it does not touch the session.
	h := other.History()
	h[0].Content = "changed"
	if other.History()[0].Content == "changed" {
		t.Error("restored History aliases exported data")
	}
	// The unused provider import keeps the package honest about types.
	var _ []provider.Message
}
