#!/usr/bin/env bash
# Verification for T401 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named tests: main unlimited, subagent 500" <<'CMD'
go_tests ./internal/agent TestMainNoIterationLimit TestSubagentIterationLimit
CMD
expect_ok "phase 4 exit criterion" <<'CMD'
go test -count=1 ./internal/agent/ -run 'IterationLimit'
CMD
expect_ok "both limit constants exported" <<'CMD'
has_symbols ./internal/agent MaxIterations MaxSubagentIterations
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

func zzSess(t *testing.T, srv *providertest.Server) *Session {
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
	if err := os.WriteFile(config.GlobalPath(home), []byte(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 10000000)), 0o644); err != nil {
		t.Fatal(err)
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
	s, err := NewSession(Options{Config: cfg, StartDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Cancel(); s.Wait() })
	return s
}

func zzTurn(t *testing.T, s *Session, text string, d time.Duration) []Event {
	t.Helper()
	if err := s.Submit(text); err != nil {
		t.Fatal(err)
	}
	var evs []Event
	deadline := time.Now().Add(d)
	for {
		for _, ev := range s.PollEvents() {
			evs = append(evs, ev)
			switch ev.Kind {
			case EvApproval:
				ev.Approval.Respond(Allow)
			case EvTurnDone:
				return evs
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("turn did not finish")
		}
		time.Sleep(3 * time.Millisecond)
	}
}

func zzUser(req providertest.Request) (string, int) {
	user, steps := "", 0
	for _, m := range req.Messages {
		switch m.Role {
		case "user":
			user, steps = m.Content, 0
		case "tool":
			steps++
		}
	}
	return user, steps
}

// zzRig: subagent requests (task SUBTASK-zz) and main requests of "loop"
// turns always call glob; a "spawn" turn spawns, waits, then finishes.
func zzRig(t *testing.T) (*Session, *atomic.Int64, *atomic.Int64, *atomic.Int64) {
	var sub, main, waitRes atomic.Int64
	srv := providertest.NewFunc(func(n int, req providertest.Request) providertest.Response {
		glob := providertest.Response{ToolCalls: []providertest.ToolCall{{ID: fmt.Sprintf("g%d", n), Name: "glob", Arguments: `{"pattern":"*.none"}`}}}
		user, steps := zzUser(req)
		if user == "SUBTASK-zz" {
			sub.Add(1)
			return glob
		}
		main.Add(1)
		if user == "loop" {
			return glob
		}
		switch steps {
		case 0:
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "s1", Name: "spawn_agent", Arguments: `{"task":"SUBTASK-zz"}`}}}
		case 1:
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "w1", Name: "wait_agent", Arguments: `{"id":"a1"}`}}}
		}
		last := req.Messages[len(req.Messages)-1]
		if strings.Contains(last.Content, "iteration limit (500) reached") {
			waitRes.Store(1)
		}
		return providertest.Response{Text: "main done"}
	})
	t.Cleanup(srv.Close)
	s := zzSess(t, srv)
	s.SetMode(ModeAuto)
	return s, &sub, &main, &waitRes
}

func TestZZPosSubagent500(t *testing.T) {
	if MaxIterations != 0 || MaxSubagentIterations != 500 {
		t.Fatalf("limits %d %d", MaxIterations, MaxSubagentIterations)
	}
	s, sub, main, waitRes := zzRig(t)
	evs := zzTurn(t, s, "spawn", 60*time.Second)
	if sub.Load() != 500 {
		t.Fatalf("subagent requests = %d, want 500", sub.Load())
	}
	if main.Load() != 3 || waitRes.Load() != 1 {
		t.Fatalf("main requests = %d, wait result seen = %d", main.Load(), waitRes.Load())
	}
	subs := s.Subagents()
	if len(subs) != 1 || !strings.Contains(subs[0].Err, "iteration limit (500) reached") {
		t.Fatalf("subagents %+v", subs)
	}
	for _, ev := range evs {
		if ev.Kind == EvError && ev.AgentID == MainAgentID {
			t.Fatalf("main turn failed: %s", ev.Text)
		}
	}
}

func TestZZPosMainUnlimited(t *testing.T) {
	var reqs atomic.Int64
	srv := providertest.NewFunc(func(n int, _ providertest.Request) providertest.Response {
		reqs.Add(1)
		if n >= 120 {
			return providertest.Response{Text: "probe done"}
		}
		return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: fmt.Sprintf("u%d", n), Name: "glob", Arguments: `{"pattern":"*.none"}`}}}
	})
	t.Cleanup(srv.Close)
	s := zzSess(t, srv)
	s.SetMode(ModeAuto)
	evs := zzTurn(t, s, "unlimited", 30*time.Second)
	if reqs.Load() != 121 {
		t.Fatalf("main requests = %d, want 121", reqs.Load())
	}
	for _, ev := range evs {
		if ev.Kind == EvError && ev.AgentID == MainAgentID {
			t.Fatalf("main turn failed: %s", ev.Text)
		}
	}
	h := s.History()
	if last := h[len(h)-1]; last.Role != "assistant" || last.Content != "probe done" {
		t.Fatalf("last history entry %+v", last)
	}
	if MaxIterations != 0 {
		t.Errorf("MaxIterations = %d, want 0", MaxIterations)
	}
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/agent/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: subagent stops at exactly 500 calls with its error text, main agent runs 121 requests unlimited" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/agent TestZZPosSubagent500 TestZZPosMainUnlimited
CMD
expect_fail "negative: no main 50 limit in internal/agent" <<'CMD'
no_match -n -F -e 'iteration limit (50)' internal/agent/*.go
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 0c3a7c5 -- go.mod go.sum
CMD
expect_fail "negative: no protected or security-critical path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md CLAUDE.md prompts .claude scripts/verify versions/v5/spec.md versions/v5/plan.md versions/v5/tasks internal/provider internal/tools internal/config internal/tui internal/agent/approval.go internal/agent/question.go internal/agent/jev.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
