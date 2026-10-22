#!/usr/bin/env bash
# Verification for T202 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "approval.go and approval_test.go equal their pre-v4 version (ba7d939^)" <<'CMD'
git rev-parse -q --verify 'ba7d939^{commit}' >/dev/null || exit 125
git diff --quiet ba7d939^ -- internal/agent/approval.go internal/agent/approval_test.go
CMD
expect_ok "gate tests (pre-v4 set)" <<'CMD'
go_tests ./internal/agent TestGateManualPromptsGated TestGateReadsNeverPrompt TestGateDeny TestGateAlwaysPerTool TestGateAutoNeverPrompts TestGateReset TestGateCancel TestGateLabel
CMD
expect_ok "named tests and adapted TestJevE2E" <<'CMD'
go_tests ./internal/agent TestJevNoRiskQuestion && go_tests ./e2e TestJevNoRiskScreen TestJevE2E
CMD
expect_ok "v4 risk tests deleted" <<'CMD'
test ! -e internal/agent/jev_risk_test.go
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

func zzSess(t *testing.T, srv *providertest.Server, jevJSON string) *Session {
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
	if err := os.WriteFile(config.GlobalPath(home), []byte(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(`{"jev":`+jevJSON+`}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(home, dir, func(k string) string {
		switch k {
		case "FAKE_KEY":
			return "SECRET123"
		case "TYPESAFE_API_KEY":
			return "JEVKEY-zz"
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

// zzRun runs one turn calling bash, write_file and edit_file in mode; every
// approval prompt is answered with d. It returns the number of prompts, the
// start dir and the jev requests.
func zzRun(t *testing.T, mode Mode, d Decision) (int, string, []providertest.JevRequest) {
	t.Helper()
	srv := providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		steps := 0
		for _, m := range req.Messages {
			switch m.Role {
			case "user":
				steps = 0
			case "tool":
				steps++
			}
		}
		switch steps {
		case 0:
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "b1", Name: "bash", Arguments: `{"command":"echo zz > bash.txt"}`}}}
		case 1:
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "w1", Name: "write_file", Arguments: `{"path":"w.txt","content":"hello\n"}`}}}
		case 2:
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "e1", Name: "edit_file", Arguments: `{"path":"w.txt","old_string":"hello","new_string":"bye"}`}}}
		}
		return providertest.Response{Text: "done"}
	})
	t.Cleanup(srv.Close)
	j := providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
		ans := map[string]any{}
		for name, q := range req.Questions {
			if qm, _ := q.(map[string]any); qm["type"] == "score" {
				ans[name] = providertest.JevScoreAnswer(0, 0.9)
			} else {
				ans[name] = providertest.JevNoulAnswer(0.99)
			}
		}
		return providertest.JevResponse{Answers: ans}
	})
	t.Cleanup(j.Close)
	s := zzSess(t, srv, `{"baseURL":"`+j.URL+`","apiKey":"{env:TYPESAFE_API_KEY}","route":{"easy":"fake/m2","hard":"fake/m1"}}`)
	s.SetJevTimeout(500 * time.Millisecond)
	if err := s.SetJev(true); err != nil {
		t.Fatal(err)
	}
	s.SetMode(mode)
	if err := s.Submit("do the three things"); err != nil {
		t.Fatal(err)
	}
	prompts := 0
	deadline := time.Now().Add(10 * time.Second)
	for done := false; !done; {
		for _, ev := range s.PollEvents() {
			switch ev.Kind {
			case EvApproval:
				prompts++
				ev.Approval.Respond(d)
			case EvQuestion:
				ev.Question.Cancel()
			case EvTurnDone:
				done = true
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("turn did not finish")
		}
		time.Sleep(3 * time.Millisecond)
	}
	return prompts, s.StartDir(), j.Requests()
}

func zzNoRisk(t *testing.T, reqs []providertest.JevRequest) {
	t.Helper()
	for _, r := range reqs {
		for name, q := range r.Questions {
			if qm, _ := q.(map[string]any); qm["type"] == "noul" || name == "risk" {
				t.Fatalf("risk question sent to jev: %q %v", name, qm)
			}
		}
	}
}

func TestZZPosAuto(t *testing.T) {
	prompts, dir, reqs := zzRun(t, ModeAuto, Deny)
	if prompts != 0 {
		t.Fatalf("auto mode prompted %d times", prompts)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "bash.txt")); err != nil || string(b) != "zz\n" {
		t.Fatalf("bash did not run: %q %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "w.txt")); err != nil || string(b) != "bye\n" {
		t.Fatalf("write/edit did not run: %q %v", b, err)
	}
	if len(reqs) != 1 {
		t.Fatalf("jev requests: %d, want 1 (routing only)", len(reqs))
	}
	zzNoRisk(t, reqs)
}

func TestZZPosManual(t *testing.T) {
	prompts, dir, reqs := zzRun(t, ModeManual, Allow)
	if prompts != 3 {
		t.Fatalf("manual mode prompts = %d, want 3", prompts)
	}
	if _, err := os.Stat(filepath.Join(dir, "bash.txt")); err != nil {
		t.Fatalf("allowed bash did not run: %v", err)
	}
	zzNoRisk(t, reqs)
}

// TestZZEvil passes only when a denied manual-mode call ran, manual mode ran
// a gated call without a prompt, or auto mode prompted.
func TestZZEvil(t *testing.T) {
	prompts, dir, _ := zzRun(t, ModeManual, Deny)
	if prompts == 0 {
		t.Log("manual mode ran gated calls without a prompt")
		return
	}
	for _, f := range []string{"bash.txt", "w.txt"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Logf("denied call ran: %s exists", f)
			return
		}
	}
	if prompts, _, _ := zzRun(t, ModeAuto, Deny); prompts != 0 {
		t.Log("auto mode prompted")
		return
	}
	t.Fatal("REJECTED: manual prompts and denials hold, auto mode never prompts")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/agent/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: auto runs unprompted without a risk question; manual prompts" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/agent TestZZPosAuto TestZZPosManual
CMD
expect_fail "negative: manual call unprompted, denied call ran, or auto prompted" <<'CMD'
out="$(go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvil$' ./internal/agent 2>&1)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q -e 'REJECTED:' && exit 1
printf '%s\n' "$out" | grep -q -e '^ok' && exit 0
exit 125
CMD
expect_fail "negative: risk-screen code left in internal" <<'CMD'
no_match -rn -e 'SetRiskScreen' -e 'jevRisk' -e 'riskQuestion' -e 'riskScreened' -e 'storeAlways' internal
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 0c3a7c5 -- go.mod go.sum
CMD
expect_fail "negative: no protected or other security-critical path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md CLAUDE.md prompts .claude scripts/verify versions/v5/spec.md versions/v5/plan.md versions/v5/tasks internal/provider internal/tools internal/config internal/tui internal/agent/question.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
