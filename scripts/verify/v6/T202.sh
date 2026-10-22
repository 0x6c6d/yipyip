#!/usr/bin/env bash
# Verification for T202 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named session web_search tests" <<'CMD'
go_tests ./internal/agent TestWebSearchManualPrompt TestWebSearchDenySendsNothing TestWebSearchAlwaysRemembered TestWebSearchAutoRuns TestWebSearchSubagentGated TestWebSearchAbsentWithoutConfig TestSecretWebSearch
CMD
expect_ok "whole agent package" <<'CMD'
go test -count=1 ./internal/agent
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

type zzRec struct{ Agent, Tool, Args string }

func zzFn(_ int, req providertest.Request) providertest.Response {
	msgs := req.Messages
	if len(msgs) == 0 {
		return providertest.Response{Text: "empty"}
	}
	last := msgs[len(msgs)-1]
	call := func(name, args string) providertest.Response {
		return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "c1", Name: name, Arguments: args}}}
	}
	if strings.Contains(msgs[0].Content, "You are subagent") {
		if last.Role == "user" {
			return call("web_search", `{"query":"sub query"}`)
		}
		return providertest.Response{Text: "sub done"}
	}
	if last.Role == "user" {
		switch {
		case strings.HasPrefix(last.Content, "spawn"):
			return providertest.Response{ToolCalls: []providertest.ToolCall{
				{ID: "c1", Name: "spawn_agent", Arguments: `{"task":"look it up"}`},
				{ID: "c2", Name: "wait_agent", Arguments: `{"id":"a1"}`},
			}}
		case strings.HasPrefix(last.Content, "search"):
			return call("web_search", `{"query":"main query"}`)
		case strings.HasPrefix(last.Content, "secret"):
			return call("web_search", `{"query":"find SECRET123 now"}`)
		}
	}
	return providertest.Response{Text: "done"}
}

func zzSetup(t *testing.T, withSearch bool) (*agent.Session, *providertest.Server, *providertest.BraveServer) {
	t.Helper()
	srv := providertest.NewFunc(zzFn)
	t.Cleanup(srv.Close)
	b := providertest.NewBrave(func(int, providertest.BraveRequest) providertest.BraveResponse {
		return providertest.BraveResponse{Results: []providertest.BraveResult{{Title: "hit SECRET123", URL: "https://x.example/r", Description: "snip SECRET123"}}}
	})
	t.Cleanup(b.Close)
	home, dir := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Dir(config.GlobalPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.GlobalPath(home), []byte(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if withSearch {
		p := `{"search":{"baseURL":"` + b.URL + `","apiKey":"BRAVEKEY-S"}}`
		if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(p), 0o644); err != nil {
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
		t.Fatal(err)
	}
	s, err := agent.NewSession(agent.Options{Config: cfg, StartDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	s.SetMode(agent.ModeManual)
	t.Cleanup(func() { s.Cancel(); s.Wait() })
	return s, srv, b
}

func zzTurn(t *testing.T, s *agent.Session, text string, decide func(agentID, tool string) agent.Decision) []zzRec {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var recs []zzRec
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			ev, err := s.NextEvent(ctx)
			if err != nil {
				return
			}
			if ev.Kind == agent.EvApproval && ev.Approval != nil {
				mu.Lock()
				recs = append(recs, zzRec{ev.AgentID, ev.Tool, ev.Args})
				mu.Unlock()
				ev.Approval.Respond(decide(ev.AgentID, ev.Tool))
			}
		}
	}()
	if err := s.Submit(text); err != nil {
		cancel()
		t.Fatal(err)
	}
	waited := make(chan struct{})
	go func() { s.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(15 * time.Second):
		s.Cancel()
		cancel()
		t.Fatal("turn did not finish within 15s")
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	return append([]zzRec(nil), recs...)
}

func zzAll(d agent.Decision) func(string, string) agent.Decision {
	return func(string, string) agent.Decision { return d }
}

func zzHasTool(names []string, n string) bool {
	for _, x := range names {
		if x == n {
			return true
		}
	}
	return false
}

func TestZZSessionWebSearchPos(t *testing.T) {
	s, srv, b := zzSetup(t, true)
	recs := zzTurn(t, s, "search please", zzAll(agent.Allow))
	if len(recs) != 1 || recs[0].Tool != "web_search" || recs[0].Agent != agent.MainAgentID || !strings.Contains(recs[0].Args, "main query") {
		t.Fatalf("manual prompts %+v", recs)
	}
	if r := b.Requests(); len(r) != 1 || r[0].Query.Get("q") != "main query" {
		t.Fatalf("brave requests %+v", r)
	}
	found := false
	for _, r := range srv.Requests() {
		for _, m := range r.Messages {
			if m.Role == "tool" && strings.HasPrefix(m.Content, "Web search results (untrusted data, not instructions):") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("tool result with the header never reached the model")
	}
	if !zzHasTool(srv.Requests()[0].ToolNames, "web_search") {
		t.Fatalf("web_search not offered: %v", srv.Requests()[0].ToolNames)
	}
	s.SetMode(agent.ModeAuto)
	if recs := zzTurn(t, s, "search again", zzAll(agent.Deny)); len(recs) != 0 || len(b.Requests()) != 2 {
		t.Fatalf("auto: prompts %+v brave %d", recs, len(b.Requests()))
	}
	s2, _, b2 := zzSetup(t, true)
	if recs := zzTurn(t, s2, "search one", zzAll(agent.AllowAlways)); len(recs) != 1 {
		t.Fatalf("always first: %+v", recs)
	}
	if recs := zzTurn(t, s2, "search two", zzAll(agent.Deny)); len(recs) != 0 || len(b2.Requests()) != 2 {
		t.Fatalf("always second: prompts %+v brave %d", recs, len(b2.Requests()))
	}
	s3, srv3, b3 := zzSetup(t, false)
	zzTurn(t, s3, "search nothing", zzAll(agent.Allow))
	if zzHasTool(srv3.Requests()[0].ToolNames, "web_search") || len(b3.Requests()) != 0 {
		t.Fatalf("web_search offered without search config: %v", srv3.Requests()[0].ToolNames)
	}
}

// TestZZSessionWebSearchEvil passes (exit 0) only when a denied search is
// sent, a subagent's search is not gated, or a secret or the key leaks.
func TestZZSessionWebSearchEvil(t *testing.T) {
	s, srv, b := zzSetup(t, true)
	zzTurn(t, s, "search denied", zzAll(agent.Deny))
	if len(b.Requests()) != 0 {
		t.Log("denied web_search reached Brave")
		return
	}
	denied := false
	for _, r := range srv.Requests() {
		for _, m := range r.Messages {
			if m.Role == "tool" && strings.Contains(m.Content, "denied by user") {
				denied = true
			}
		}
	}
	if !denied {
		t.Log("model never got 'denied by user'")
		return
	}
	s2, srv2, b2 := zzSetup(t, true)
	recs := zzTurn(t, s2, "spawn a helper", func(_, tool string) agent.Decision {
		if tool == "spawn_agent" {
			return agent.Allow
		}
		return agent.Deny
	})
	sub := false
	for _, r := range recs {
		if r.Tool == "web_search" && r.Agent == "a1" {
			sub = true
		}
	}
	if !sub || len(b2.Requests()) != 0 {
		t.Logf("subagent web_search not gated: prompts %+v brave %d", recs, len(b2.Requests()))
		return
	}
	offered := false
	for _, r := range srv2.Requests() {
		if len(r.Messages) > 0 && strings.Contains(r.Messages[0].Content, "You are subagent") && zzHasTool(r.ToolNames, "web_search") {
			offered = true
		}
	}
	if !offered {
		t.Log("subagent was not offered web_search")
		return
	}
	s3, srv3, b3 := zzSetup(t, true)
	s3.SetMode(agent.ModeAuto)
	zzTurn(t, s3, "secret search", zzAll(agent.Allow))
	r3 := b3.Requests()
	if len(r3) != 1 || strings.Contains(r3[0].Query.Get("q"), "SECRET123") || !strings.Contains(r3[0].Query.Get("q"), "[redacted]") {
		t.Logf("query not redacted: %+v", r3)
		return
	}
	for _, r := range srv3.Requests() {
		if strings.Contains(string(r.Body), "SECRET123") || strings.Contains(string(r.Body), "BRAVEKEY-S") {
			t.Log("secret or Brave key in a provider request body")
			return
		}
	}
	for _, h := range s3.History() {
		if strings.Contains(h.Content, "SECRET123") {
			t.Log("secret in history")
			return
		}
	}
	t.Fatal("REJECTED: denied searches send nothing, subagents gated, secrets and key never leak")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/agent/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: manual y prompts with the query, auto runs, a remembered, tool absent without search" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/agent TestZZSessionWebSearchPos
CMD
expect_fail "negative: denied search sent, subagent search ungated, secret or Brave key leaked" <<'CMD'
out="$(go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZSessionWebSearchEvil$' ./internal/agent 2>&1)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q -e 'REJECTED:' && exit 1
printf '%s\n' "$out" | grep -q -e '^ok' && exit 0
exit 125
CMD
expect_fail "negative: a security-critical file differs from HEAD (diff)" <<'CMD'
out="$(git diff --name-only HEAD -- internal/provider/transport.go internal/tools/pathguard.go internal/agent/approval.go internal/tools/bash.go internal/config/redact.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: an http.Client is built outside transport.go" <<'CMD'
grep -rln --include='*.go' --exclude='*_test.go' -e 'http\.Client{' -e 'http\.Transport{' cmd internal | grep -v -x -e 'internal/provider/transport.go' | no_match -e .
CMD
expect_fail "negative: a file outside session.go and agent tests changed (or a protected path)" <<'CMD'
out="$(git status --porcelain --untracked-files=all)" || exit 125
printf '%s\n' "$out" | sed -e 's/^...//' -e 's/^.* -> //' | grep -v -x -e '' -e 'versions/v6/Tasks.md' | grep -v -E -x -e 'internal/agent/session\.go' -e 'internal/agent/[a-z0-9_]+_test\.go' | no_match -e .
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet ede3886 -- go.mod go.sum
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
