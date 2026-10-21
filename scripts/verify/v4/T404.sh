#!/usr/bin/env bash
# Verification for T404 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named risk-screen tests" <<'CMD'
go_tests ./internal/agent TestRiskScreenPrompts TestRiskScreenSafe TestRiskScreenManual TestRiskScreenScope TestRiskScreenNoAlways TestRiskScreenPanic TestRiskScreenCancel
CMD
expect_ok "existing approval tests still pass" <<'CMD'
go test -count=1 -run 'Approval|Gate' ./internal/agent
CMD
expect_ok "exported API present" <<'CMD'
has_symbols ./internal/agent Gate.SetRiskScreen
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package agent

import (
	"context"
	"testing"
)

type zzRig struct {
	g       *Gate
	prompts int
	dec     Decision
	calls   int
	risky   bool
	lastArg string
}

func zzNew() *zzRig {
	r := &zzRig{dec: Allow}
	r.g = NewGate(func(req *ApprovalRequest) {
		r.prompts++
		r.lastArg = req.Tool + " " + req.Args
		req.Respond(r.dec)
	})
	r.g.SetRiskScreen(func(_ context.Context, tool, args string) bool {
		r.calls++
		return r.risky
	})
	return r
}

func (r *zzRig) check(t *testing.T, mode Mode, tool string) bool {
	t.Helper()
	ok, err := r.g.Check(context.Background(), mode, MainAgentID, tool, `{"command":"rm -rf x"}`)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestZZPosAuto(t *testing.T) {
	r := zzNew()
	r.risky = true
	for _, tool := range []string{"bash", "write_file", "edit_file"} {
		r.dec = Allow
		if !r.check(t, ModeAuto, tool) {
			t.Fatalf("%s: risky+Allow refused", tool)
		}
		r.dec = Deny
		if r.check(t, ModeAuto, tool) {
			t.Fatalf("%s: risky+Deny approved", tool)
		}
	}
	if r.prompts != 6 || r.calls != 6 || r.lastArg != `edit_file {"command":"rm -rf x"}` {
		t.Fatalf("prompts %d calls %d last %q", r.prompts, r.calls, r.lastArg)
	}
	r.risky = false
	if !r.check(t, ModeAuto, "bash") || r.prompts != 6 {
		t.Fatalf("safe call prompted (%d)", r.prompts)
	}
	calls := r.calls
	r.risky = true
	if !r.check(t, ModeAuto, "spawn_agent") || !r.check(t, ModeAuto, "read_file") || r.calls != calls || r.prompts != 6 {
		t.Fatalf("hook used out of scope: calls %d prompts %d", r.calls, r.prompts)
	}
	r.g.SetRiskScreen(nil)
	if !r.check(t, ModeAuto, "bash") || r.prompts != 6 {
		t.Fatal("nil hook changed auto mode")
	}
}

func TestZZPosNilNotify(t *testing.T) {
	g := NewGate(nil)
	g.SetRiskScreen(func(context.Context, string, string) bool { return true })
	if ok, _ := g.Check(context.Background(), ModeAuto, MainAgentID, "bash", "{}"); ok {
		t.Fatal("risky call approved without a prompt channel")
	}
}

func TestZZPosManual(t *testing.T) {
	r := zzNew()
	for _, risky := range []bool{true, false} {
		r.risky = risky
		r.dec = Allow
		if !r.check(t, ModeManual, "bash") {
			t.Fatal("manual Allow refused")
		}
		r.dec = Deny
		if r.check(t, ModeManual, "bash") {
			t.Fatal("manual Deny approved")
		}
	}
	if r.calls != 0 || r.prompts != 4 {
		t.Fatalf("manual: hook calls %d prompts %d", r.calls, r.prompts)
	}
}

func TestZZPosNoAlways(t *testing.T) {
	r := zzNew()
	r.risky = true
	r.dec = AllowAlways
	if !r.check(t, ModeAuto, "bash") {
		t.Fatal("AllowAlways refused")
	}
	r.dec = Deny
	if r.check(t, ModeAuto, "bash") || r.prompts != 2 {
		t.Fatalf("always stored by risk prompt (prompts %d)", r.prompts)
	}
	if r.check(t, ModeManual, "bash") || r.prompts != 3 {
		t.Fatalf("manual after risk always: prompts %d", r.prompts)
	}
}

func TestZZPosPanic(t *testing.T) {
	r := zzNew()
	r.g.SetRiskScreen(func(context.Context, string, string) bool { panic("boom") })
	if !r.check(t, ModeAuto, "bash") || r.prompts != 0 {
		t.Fatal("panic changed auto mode")
	}
}

func TestZZPosCancel(t *testing.T) {
	g := NewGate(func(*ApprovalRequest) {}) // never answers
	g.SetRiskScreen(func(context.Context, string, string) bool { return true })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok, err := g.Check(ctx, ModeAuto, MainAgentID, "bash", "{}"); ok || err == nil {
		t.Fatalf("cancelled risk prompt: %v %v", ok, err)
	}
}

// TestZZEvilBypass passes only when the hook runs in manual mode, approves a
// manual Deny, or stores "always" from a risk prompt.
func TestZZEvilBypass(t *testing.T) {
	r := zzNew()
	r.dec = Deny
	if r.check(t, ModeManual, "bash") || r.calls > 0 {
		return
	}
	r.risky = true
	r.dec = AllowAlways
	r.check(t, ModeAuto, "write_file")
	r.dec = Deny
	if r.check(t, ModeManual, "write_file") {
		return
	}
	t.Fatal("REJECTED: hook only adds prompts in auto mode")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/agent/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: auto prompts, safe, scope, nil hook, nil notify, manual, no always, panic, cancel" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/agent TestZZPosAuto TestZZPosNilNotify TestZZPosManual TestZZPosNoAlways TestZZPosPanic TestZZPosCancel
CMD
expect_fail "negative: hook used in manual mode, manual Deny approved or risk always stored" <<'CMD'
go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvilBypass$' ./internal/agent
CMD
expect_fail "negative: approval.go gains no jev or network dependency" <<'CMD'
no_match -n -e 'provider' -e 'jev' -e 'Jev' -e '"net/' internal/agent/approval.go
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 4fabda9 -- go.mod go.sum
CMD
expect_fail "negative: no other protected or security-critical path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v4/spec.md versions/v4/plan.md versions/v4/tasks internal/provider internal/tools internal/config internal/tui)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
