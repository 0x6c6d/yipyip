#!/usr/bin/env bash
# Verification for T201 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named web_search gate tests" <<'CMD'
go_tests ./internal/agent TestWebSearchIsGated TestWebSearchGateManualPrompt TestWebSearchGateDeny TestWebSearchGateAlways TestWebSearchGateAuto TestWebSearchGateSubagent
CMD
expect_ok "existing gate tests green" <<'CMD'
go_tests ./internal/agent TestGateManualPromptsGated TestGateReadsNeverPrompt TestGateDeny TestGateAlwaysPerTool TestGateAutoNeverPrompts TestGateReset TestGateCancel TestGateLabel
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package agent_test

import (
	"context"
	"strings"
	"testing"

	"yipyip/internal/agent"
)

func TestZZGatePos(t *testing.T) {
	var got []*agent.ApprovalRequest
	g := agent.NewGate(func(r *agent.ApprovalRequest) { got = append(got, r); r.Respond(agent.Allow) })
	ok, err := g.Check(context.Background(), agent.ModeManual, agent.MainAgentID, "web_search", `{"query":"q1"}`)
	if !ok || err != nil || len(got) != 1 || got[0].Tool != "web_search" || !strings.Contains(got[0].Args, "q1") {
		t.Fatalf("manual allow: ok=%v err=%v prompts=%d", ok, err, len(got))
	}
	n := 0
	ga := agent.NewGate(func(r *agent.ApprovalRequest) { n++; r.Respond(agent.Deny) })
	if ok, _ := ga.Check(context.Background(), agent.ModeAuto, agent.MainAgentID, "web_search", `{"query":"q"}`); !ok || n != 0 {
		t.Fatalf("auto: ok=%v prompts=%d", ok, n)
	}
	m := 0
	gl := agent.NewGate(func(r *agent.ApprovalRequest) { m++; r.Respond(agent.AllowAlways) })
	for i := 0; i < 3; i++ {
		if ok, _ := gl.Check(context.Background(), agent.ModeManual, "a1", "web_search", `{"query":"q"}`); !ok {
			t.Fatal("always: denied")
		}
	}
	if m != 1 || !agent.IsGated("web_search") {
		t.Fatalf("always prompts=%d", m)
	}
}

// TestZZGateEvil passes (exit 0) only when web_search passes in manual mode
// without an explicit Allow.
func TestZZGateEvil(t *testing.T) {
	ctx := context.Background()
	args := `{"query":"q"}`
	if ok, _ := agent.NewGate(nil).Check(ctx, agent.ModeManual, agent.MainAgentID, "web_search", args); ok {
		t.Log("nil notify allowed web_search")
		return
	}
	var nilGate *agent.Gate
	if ok, _ := nilGate.Check(ctx, agent.ModeManual, agent.MainAgentID, "web_search", args); ok {
		t.Log("nil gate allowed web_search")
		return
	}
	var label string
	deny := agent.NewGate(func(r *agent.ApprovalRequest) { label = r.AgentID; r.Respond(agent.Deny) })
	if ok, _ := deny.Check(ctx, agent.ModeManual, "a1", "web_search", args); ok || label != "a1" {
		t.Logf("deny allowed web_search or wrong label %q", label)
		return
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if ok, _ := agent.NewGate(func(r *agent.ApprovalRequest) { r.Respond(agent.Allow) }).Check(cctx, agent.ModeManual, agent.MainAgentID, "web_search", args); ok {
		t.Log("cancelled context allowed web_search")
		return
	}
	other := agent.NewGate(func(r *agent.ApprovalRequest) {
		if r.Tool == "bash" {
			r.Respond(agent.AllowAlways)
			return
		}
		r.Respond(agent.Deny)
	})
	other.Check(ctx, agent.ModeManual, agent.MainAgentID, "bash", `{"command":"ls"}`)
	if ok, _ := other.Check(ctx, agent.ModeManual, agent.MainAgentID, "web_search", args); ok {
		t.Log("always for bash let web_search through")
		return
	}
	weird := agent.NewGate(func(r *agent.ApprovalRequest) { r.Respond(agent.Decision(42)) })
	if ok, _ := weird.Check(ctx, agent.ModeManual, agent.MainAgentID, "web_search", args); ok {
		t.Log("unknown decision allowed web_search")
		return
	}
	t.Fatal("REJECTED: web_search never passes manual mode without an explicit Allow")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/agent/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: manual prompt with the query, auto runs, always remembered" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/agent TestZZGatePos
CMD
expect_fail "negative: web_search passes manual mode without an explicit Allow" <<'CMD'
out="$(go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZGateEvil$' ./internal/agent 2>&1)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q -e 'REJECTED:' && exit 1
printf '%s\n' "$out" | grep -q -e '^ok' && exit 0
exit 125
CMD
expect_ok "approval.go changed against ede3886 (this task owns it)" <<'CMD'
! git diff --quiet ede3886 -- internal/agent/approval.go && grep -q -e '"web_search"' internal/agent/approval.go
CMD
expect_fail "negative: approval.go diff deletes a line or adds a line not about web_search" <<'CMD'
d="$(git diff -U0 ede3886 -- internal/agent/approval.go)" || exit 125
printf '%s\n' "$d" | grep -e '^[-+]' | grep -v -e '^---' -e '^+++' | grep -v -E -e '^\+.*web_search' -e '^\+[[:space:]]*//' -e '^\+[[:space:]]*$' | no_match -e .
CMD
expect_fail "negative: another security-critical file differs from HEAD (diff)" <<'CMD'
out="$(git diff --name-only HEAD -- internal/provider/transport.go internal/tools/pathguard.go internal/tools/bash.go internal/config/redact.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: a file outside approval.go and agent tests changed (or a protected path)" <<'CMD'
out="$(git status --porcelain --untracked-files=all)" || exit 125
printf '%s\n' "$out" | sed -e 's/^...//' -e 's/^.* -> //' | grep -v -x -e '' -e 'versions/v6/Tasks.md' | grep -v -E -x -e 'internal/agent/approval\.go' -e 'internal/agent/[a-z0-9_]+_test\.go' | no_match -e .
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet ede3886 -- go.mod go.sum
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
