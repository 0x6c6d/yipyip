package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

const secretValue = "SECRET123"

// secretFake plays the secret-audit scenario. Every reply that carries text
// echoes the request's Authorization header (the API key):
//   - subagent "echo": a text reply with the header;
//   - subagent "fail": HTTP 401 whose body contains the header;
//   - main, user message starting with "go": text plus bash (echoing the env
//     variable and the literal key), spawn of both subagents and waits;
//   - main, after tool results: HTTP 401 whose body contains the header;
//   - the compaction summary request: a summary containing the header;
//   - anything else: a text reply with the header.
func secretFake() *providertest.Server {
	return providertest.NewFunc(func(_ int, req providertest.Request) fakeResp {
		auth := req.Header.Get("Authorization")
		msgs := req.Messages
		if len(msgs) == 0 {
			return fakeResp{Text: "empty " + auth}
		}
		last := msgs[len(msgs)-1]
		if strings.Contains(msgs[0].Content, "You are subagent") {
			if strings.Contains(msgs[1].Content, "fail") {
				return fakeResp{Status: 401, Body: `{"error":"invalid key ` + auth + `"}`}
			}
			return fakeResp{Text: "sub sees " + auth}
		}
		switch {
		case last.Role == "tool":
			return fakeResp{Status: 401, Body: `{"error":{"message":"bad header ` + auth + `"}}`}
		case last.Role == "user" && last.Content == summaryRequest:
			return fakeResp{Text: "summary: key was " + auth}
		case last.Role == "user" && strings.HasPrefix(last.Content, "go"):
			return fakeResp{Text: "auth=" + auth, ToolCalls: []fakeCall{
				{ID: "c1", Name: "bash", Arguments: `{"command":"echo $FAKE_KEY; echo SECRET123"}`},
				{ID: "c2", Name: "spawn_agent", Arguments: `{"task":"echo the header"}`},
				{ID: "c3", Name: "spawn_agent", Arguments: `{"task":"fail please"}`},
				{ID: "c4", Name: "wait_agent", Arguments: `{"id":"a1"}`},
				{ID: "c5", Name: "wait_agent", Arguments: `{"id":"a2"}`},
			}}
		}
		return fakeResp{Text: "reply " + auth}
	})
}

func noSecret(t *testing.T, what, text string) {
	t.Helper()
	if strings.Contains(text, secretValue) {
		t.Errorf("%s contains the secret: %q", what, text)
	}
}

func TestSecretAgentEvents(t *testing.T) {
	// The real process environment holds the key too, so a bash call would
	// see it unless the environment is stripped.
	t.Setenv("FAKE_KEY", secretValue)
	srv := secretFake()
	defer srv.Close()
	s, dir := newLoopSession(t, srv)

	// AGENTS.md mentioning the key enters the system prompt redacted.
	if err := os.WriteFile(filepath.Join(dir, AgentsFile), []byte("notes: key SECRET123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Reset()
	noSecret(t, "SystemPrompt", s.SystemPrompt())
	if !strings.Contains(s.SystemPrompt(), "notes: key [redacted]") {
		t.Errorf("AGENTS.md not in the system prompt: %q", s.SystemPrompt())
	}
	s.SetMode(ModeAuto)

	var evs []Event
	evs = append(evs, submitAndWait(t, s, "go, the key is SECRET123")...)

	// /init with the existing AGENTS.md, then one more turn so compaction
	// has more than two turns.
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	waitTurn(t, s, 15*time.Second)
	evs = append(evs, s.PollEvents()...)
	evs = append(evs, submitAndWait(t, s, "three")...)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Compact(ctx); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	evs = append(evs, s.PollEvents()...)

	// The scenario really happened: the fake received the key and echoed it.
	reqs := srv.Requests()
	if len(reqs) < 6 || reqs[0].Header.Get("Authorization") != "Bearer "+secretValue {
		t.Fatalf("unexpected requests: %d", len(reqs))
	}
	var mainText strings.Builder
	sawBash, sawError, sawSubFail := false, false, false
	for _, ev := range evs {
		noSecret(t, "event text", ev.Text)
		noSecret(t, "event args", ev.Args)
		noSecret(t, "event tool", ev.Tool)
		if ev.Approval != nil {
			noSecret(t, "approval args", ev.Approval.Args)
		}
		switch {
		case ev.Kind == EvText && ev.AgentID == MainAgentID:
			mainText.WriteString(ev.Text)
		case ev.Kind == EvToolResult && ev.Tool == "bash":
			sawBash = strings.Contains(ev.Text, "[redacted]")
		case ev.Kind == EvToolResult && ev.Tool == "wait_agent" && ev.IsError:
			sawSubFail = strings.Contains(ev.Text, "401")
		case ev.Kind == EvError:
			sawError = sawError || strings.Contains(ev.Text, "401")
		}
	}
	if !strings.Contains(mainText.String(), "auth=Bearer [redacted]") {
		t.Errorf("echoed header not streamed (redacted): %q", mainText.String())
	}
	if !sawBash || !sawError || !sawSubFail {
		t.Errorf("scenario incomplete: bash=%v error=%v subFail=%v", sawBash, sawError, sawSubFail)
	}

	hist := s.History()
	sawSummary := false
	for i, m := range hist {
		noSecret(t, "history content", m.Content)
		for _, tc := range m.ToolCalls {
			noSecret(t, "history tool args", tc.Arguments)
		}
		if i == 1 && strings.Contains(m.Content, "summary: key was Bearer [redacted]") {
			sawSummary = true
		}
	}
	if !sawSummary {
		t.Errorf("compaction summary missing: %+v", hist)
	}

	// Everything sent back to the provider (later requests carry the
	// history) is free of the key; only the Authorization header holds it.
	for i, r := range reqs {
		noSecret(t, "request body", string(r.Body))
		for _, m := range r.Messages {
			noSecret(t, "request message", m.Content)
			for _, tc := range m.ToolCalls {
				noSecret(t, "request tool args", tc.Arguments)
			}
		}
		if i > 0 && r.Header.Get("Authorization") != "Bearer "+secretValue {
			t.Errorf("request %d lost its key", i)
		}
	}

	// Subagents: the compaction does not reset them, so both are listed.
	subs := s.Subagents()
	if len(subs) != 2 {
		t.Fatalf("subagents = %+v", subs)
	}
	for _, sub := range subs {
		noSecret(t, "subagent task", sub.Task)
		noSecret(t, "subagent final", sub.Final)
		noSecret(t, "subagent err", sub.Err)
		lines, ok := s.SubagentTranscript(sub.ID)
		if !ok || len(lines) == 0 {
			t.Errorf("no transcript for %s", sub.ID)
		}
		for _, l := range lines {
			noSecret(t, "transcript", l)
		}
	}
	if subs[0].Status != SubDone || !strings.Contains(subs[0].Final, "Bearer [redacted]") {
		t.Errorf("subagent a1 = %+v", subs[0])
	}
	if subs[1].Status != SubFailed || !strings.Contains(subs[1].Err, "401") {
		t.Errorf("subagent a2 = %+v", subs[1])
	}
}

// TestSecretSubagentPanic: a panic text recorded as a subagent's error is
// redacted.
func TestSecretSubagentPanic(t *testing.T) {
	s, _ := newTestSession(t, "")
	m := NewManager(0, s.redactPanics(func(context.Context, *Subagent, string, string) (string, error) {
		panic("boom SECRET123")
	}), nil)
	id, err := m.Spawn(context.Background(), "t", "fake/m1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, werr := m.Wait(ctx, id)
	info, _ := m.Get(id)
	if info.Status != SubFailed || !strings.Contains(info.Err, "boom [redacted]") {
		t.Errorf("info = %+v", info)
	}
	if werr == nil {
		t.Error("Wait: no error")
	} else {
		noSecret(t, "wait error", werr.Error())
	}
}

// TestSecretNewSessionError: an invalid base URL built from an {env:} value
// does not reveal the value in the NewSession error.
func TestSecretNewSessionError(t *testing.T) {
	base := t.TempDir()
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	if err := os.MkdirAll(filepath.Join(home, ".config", "yipyip"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgJSON := providertest.ConfigJSON("http://bad host{env:FAKE_KEY}/v1", "{env:FAKE_KEY}", 8000)
	if err := os.WriteFile(config.GlobalPath(home), []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(home, dir, func(k string) string {
		if k == "FAKE_KEY" {
			return secretValue
		}
		return ""
	})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	_, err = NewSession(Options{Config: cfg, StartDir: dir})
	if err == nil {
		t.Fatal("NewSession accepted an invalid base URL")
	}
	noSecret(t, "NewSession error", err.Error())
	if !strings.Contains(err.Error(), "invalid base URL") {
		t.Errorf("error = %q", err)
	}
}
