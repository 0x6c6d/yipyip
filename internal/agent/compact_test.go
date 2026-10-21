package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

const testSummary = "SUMMARY-TEXT"

func isSummaryReq(req providertest.Request) bool {
	n := len(req.Messages)
	return n > 0 && req.Messages[n-1].Role == "user" && req.Messages[n-1].Content == summaryRequest
}

func countSummaryReqs(srv *providertest.Server) int {
	n := 0
	for _, r := range srv.Requests() {
		if isSummaryReq(r) {
			n++
		}
	}
	return n
}

// compactServer answers summary requests with testSummary and everything else
// with "answer N"; prompt is the usage prompt reported for normal replies.
func compactServer(t *testing.T, prompt int) *providertest.Server {
	t.Helper()
	srv := providertest.NewFunc(func(n int, req providertest.Request) providertest.Response {
		if isSummaryReq(req) {
			return fakeResp{Text: testSummary, Usage: &fakeUsage{Prompt: 50, Completion: 5}}
		}
		return fakeResp{Text: "answer", Usage: &fakeUsage{Prompt: prompt, Completion: 10}}
	})
	t.Cleanup(srv.Close)
	return srv
}

func compactSession(t *testing.T, srv *providertest.Server, limit int, projectJSON string) (*Session, string) {
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
	if err := os.WriteFile(config.GlobalPath(home), []byte(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", limit)), 0o644); err != nil {
		t.Fatal(err)
	}
	if projectJSON != "" {
		if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(projectJSON), 0o644); err != nil {
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
		t.Fatal(err)
	}
	s.SetMode(ModeAuto)
	t.Cleanup(func() { s.Cancel(); s.Wait() })
	return s, dir
}

func TestCompactKeepsLastTwoTurns(t *testing.T) {
	srv := compactServer(t, 10)
	s, _ := compactSession(t, srv, 8000, "")
	for _, q := range []string{"q1", "q2", "q3", "q4"} {
		submitAndWait(t, s, q)
	}
	if err := s.Compact(context.Background()); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	h := s.History()
	if len(h) != 6 {
		t.Fatalf("history len = %d: %+v", len(h), h)
	}
	if h[0].Role != "system" || h[0].Content != s.SystemPrompt() {
		t.Errorf("h[0] = %+v", h[0])
	}
	if h[1].Role != "user" || h[1].Content != "[Summary of the earlier conversation]\n"+testSummary {
		t.Errorf("h[1] = %+v", h[1])
	}
	want := []struct{ role, content string }{{"user", "q3"}, {"assistant", "answer"}, {"user", "q4"}, {"assistant", "answer"}}
	for i, w := range want {
		if h[2+i].Role != w.role || h[2+i].Content != w.content {
			t.Errorf("h[%d] = %+v, want %v", 2+i, h[2+i], w)
		}
	}
	reqs := srv.Requests()
	last := reqs[len(reqs)-1]
	if !isSummaryReq(last) || len(last.ToolNames) != 0 {
		t.Errorf("summary request wrong: tools=%v", last.ToolNames)
	}
	if !findEvent(s.PollEvents(), EvInfo, "compacted: 9 messages -> 6") {
		t.Error("no compacted info event")
	}
	if used, _ := s.ContextSize(); used != estimateTokens(messageChars(h)) {
		t.Errorf("context used = %d", used)
	}
}

func TestCompactKeepsToolPairs(t *testing.T) {
	srv := providertest.NewFunc(func(n int, req providertest.Request) providertest.Response {
		switch {
		case isSummaryReq(req):
			return fakeResp{Text: testSummary}
		case req.Messages[len(req.Messages)-1].Role == "user" && req.Messages[len(req.Messages)-1].Content == "tools":
			return fakeResp{ToolCalls: []fakeCall{{ID: "c1", Name: "glob", Arguments: `{"pattern":"*"}`}}}
		}
		return fakeResp{Text: "answer"}
	})
	defer srv.Close()
	s, _ := compactSession(t, srv, 8000, "")
	for _, q := range []string{"q1", "q2", "tools", "q4"} {
		submitAndWait(t, s, q)
	}
	if err := s.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := s.History()
	// system, summary, tools, assistant(call), tool, assistant, q4, answer
	if len(h) != 8 || len(h[3].ToolCalls) != 1 || h[4].Role != "tool" || h[4].ToolCallID != "c1" {
		t.Fatalf("history = %+v", h)
	}
}

func TestCompactTokensNotReduced(t *testing.T) {
	srv := compactServer(t, 100)
	s, _ := compactSession(t, srv, 8000, "")
	for _, q := range []string{"q1", "q2", "q3"} {
		submitAndWait(t, s, q)
	}
	before := s.Tokens()
	if err := s.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := s.Tokens()
	if after.In != before.In+50 || after.Out != before.Out+5 || after.Total != before.Total+55 {
		t.Errorf("tokens before %+v after %+v", before, after)
	}
}

func TestCompactNothing(t *testing.T) {
	srv := compactServer(t, 10)
	s, _ := compactSession(t, srv, 8000, "")
	if err := s.Compact(context.Background()); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("empty: %v", err)
	}
	submitAndWait(t, s, "q1")
	submitAndWait(t, s, "q2")
	before := s.History()
	if err := s.Compact(context.Background()); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("two turns: %v", err)
	}
	if len(s.History()) != len(before) || countSummaryReqs(srv) != 0 {
		t.Error("history changed or summary requested")
	}
}

func TestCompactErrorKeepsHistory(t *testing.T) {
	srv := providertest.NewFunc(func(n int, req providertest.Request) providertest.Response {
		if isSummaryReq(req) {
			return fakeResp{Status: 500, Body: "boom"}
		}
		return fakeResp{Text: "answer"}
	})
	defer srv.Close()
	s, _ := compactSession(t, srv, 8000, "")
	for _, q := range []string{"q1", "q2", "q3"} {
		submitAndWait(t, s, q)
	}
	before := s.History()
	if err := s.Compact(context.Background()); err == nil {
		t.Fatal("want error")
	}
	if len(s.History()) != len(before) || s.Busy() {
		t.Error("history changed or still busy")
	}
}

func TestCompactBusy(t *testing.T) {
	srv := compactServer(t, 10)
	s, _ := compactSession(t, srv, 8000, "")
	s.mu.Lock()
	s.busy = true
	s.mu.Unlock()
	if err := s.Compact(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("Compact: %v", err)
	}
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
}

func TestUndoAfterCompact(t *testing.T) {
	srv := providertest.NewFunc(func(n int, req providertest.Request) providertest.Response {
		last := req.Messages[len(req.Messages)-1]
		switch {
		case isSummaryReq(req):
			return fakeResp{Text: testSummary}
		case last.Role == "user":
			return fakeResp{ToolCalls: []fakeCall{writeCall("c"+last.Content, last.Content+".txt", last.Content)}}
		}
		return fakeResp{Text: "done"}
	})
	defer srv.Close()
	s, dir := compactSession(t, srv, 8000, "")
	for _, q := range []string{"q1", "q2", "q3", "q4"} {
		submitAndWait(t, s, q)
	}
	if err := s.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustUndo(t, s)
	if h := s.History(); len(h) != 6 || h[len(h)-1].Role != "assistant" || h[len(h)-2].Role != "tool" || h[2].Content != "q3" {
		t.Fatalf("after first undo: %+v", h)
	}
	if got := readOrMissing(filepath.Join(dir, "q4.txt")); got != "<missing>" {
		t.Errorf("q4.txt = %q", got)
	}
	mustUndo(t, s)
	if h := s.History(); len(h) != 2 || h[1].Role != "user" || !strings.HasPrefix(h[1].Content, summaryPrefix) {
		t.Fatalf("after second undo: %+v", h)
	}
	if got := readOrMissing(filepath.Join(dir, "q3.txt")); got != "<missing>" {
		t.Errorf("q3.txt = %q", got)
	}
	if got := readOrMissing(filepath.Join(dir, "q2.txt")); got != "q2" {
		t.Errorf("q2.txt = %q", got)
	}
	if _, err := s.Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("third undo: %v", err)
	}
}

// primed returns a session with three finished turns, whose last prompt was
// `prompt` tokens (limit 1000).
func primed(t *testing.T, prompt int, projectJSON string) (*Session, *providertest.Server) {
	t.Helper()
	srv := compactServer(t, prompt)
	s, _ := compactSession(t, srv, 1000, projectJSON)
	for _, q := range []string{"q1", "q2", "q3"} {
		submitAndWait(t, s, q)
	}
	if n := countSummaryReqs(srv); n != 0 {
		t.Fatalf("summary requests while priming: %d", n)
	}
	return s, srv
}

func TestAutoCompactAt80(t *testing.T) {
	s, srv := primed(t, 800, "")
	before := len(srv.Requests())
	evs := submitAndWait(t, s, "q4")
	reqs := srv.Requests()[before:]
	if len(reqs) != 2 || !isSummaryReq(reqs[0]) || isSummaryReq(reqs[1]) {
		t.Fatalf("want summary request then turn request, got %d requests", len(reqs))
	}
	if !findEvent(evs, EvInfo, "auto-compacted") {
		t.Errorf("no auto-compacted event: %+v", evs)
	}
	// The turn request holds the summary, the last two turns and the new user message.
	m := reqs[1].Messages
	if m[1].Content != summaryPrefix+testSummary || m[len(m)-1].Content != "q4" || len(m) != 2+4+1 {
		t.Errorf("turn request messages = %+v", m)
	}
	h := s.History()
	if len(h) != 8 || h[len(h)-2].Content != "q4" {
		t.Fatalf("history = %+v", h)
	}
	// Undo of the new turn truncates exactly before its user message.
	mustUndo(t, s)
	if h := s.History(); len(h) != 6 || h[len(h)-2].Content != "q3" {
		t.Fatalf("after undo: %+v", h)
	}
}

func TestAutoCompactBelowThreshold(t *testing.T) {
	s, srv := primed(t, 799, "")
	submitAndWait(t, s, "q4")
	if n := countSummaryReqs(srv); n != 0 {
		t.Fatalf("summary requests: %d", n)
	}
}

func TestAutoCompactDisabled(t *testing.T) {
	s, srv := primed(t, 800, `{"auto_compact": false}`)
	submitAndWait(t, s, "q4")
	if n := countSummaryReqs(srv); n != 0 {
		t.Fatalf("summary requests: %d", n)
	}
}

func TestAutoCompactNothingToCompactContinues(t *testing.T) {
	srv := compactServer(t, 900)
	s, _ := compactSession(t, srv, 1000, "")
	submitAndWait(t, s, "q1")
	evs := submitAndWait(t, s, "q2") // only one earlier turn: ignored silently
	if n := countSummaryReqs(srv); n != 0 || findEvent(evs, EvError, "") {
		t.Fatalf("summary requests %d, events %+v", n, evs)
	}
}

func TestAutoCompactFailureContinues(t *testing.T) {
	srv := providertest.NewFunc(func(n int, req providertest.Request) providertest.Response {
		if isSummaryReq(req) {
			return fakeResp{Status: 500, Body: "boom"}
		}
		return fakeResp{Text: "answer", Usage: &fakeUsage{Prompt: 900, Completion: 1}}
	})
	defer srv.Close()
	s, _ := compactSession(t, srv, 1000, "")
	for _, q := range []string{"q1", "q2", "q3"} {
		submitAndWait(t, s, q)
	}
	evs := submitAndWait(t, s, "q4")
	if !findEvent(evs, EvError, "auto-compact failed") {
		t.Errorf("no error event: %+v", evs)
	}
	h := s.History()
	if len(h) != 9 || h[len(h)-1].Content != "answer" {
		t.Fatalf("history len %d", len(h))
	}
}
