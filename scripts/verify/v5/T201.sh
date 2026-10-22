#!/usr/bin/env bash
# Verification for T201 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named tests and kept question tests (agent)" <<'CMD'
go_tests ./internal/agent TestJevQuestionAsksUser && go test -count=1 -run 'Question' ./internal/agent
CMD
expect_ok "named e2e test and adapted TestJevE2E" <<'CMD'
go_tests ./e2e TestJevQuestionGoesToUser TestJevE2E
CMD
expect_ok "v4 jev question tests deleted" <<'CMD'
test ! -e internal/agent/jev_question_test.go
CMD
expect_ok "generic client and fake keep JevChoice" <<'CMD'
has_symbols ./internal/provider JevChoice && has_symbols ./internal/provider/providertest JevChoiceAnswer
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package agent

import (
	"os"
	"path/filepath"
	"strings"
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

type zzOut struct {
	asked  bool
	result string
	info   string
}

func zzTurn(t *testing.T, s *Session, user string) zzOut {
	t.Helper()
	if err := s.Submit(user); err != nil {
		t.Fatal(err)
	}
	var o zzOut
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, ev := range s.PollEvents() {
			switch ev.Kind {
			case EvQuestion:
				o.asked = true
				ev.Question.Respond("USER-ANS")
			case EvApproval:
				ev.Approval.Respond(Deny)
			case EvInfo:
				o.info += ev.Text + "\n"
			case EvToolResult:
				if ev.Tool == "question" {
					o.result = ev.Text
				}
			case EvTurnDone:
				return o
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("turn did not finish: %+v", o)
		}
		time.Sleep(3 * time.Millisecond)
	}
}

// zzCase: the model asks a 2-option question; jev answers every question
// confidently (choice "beta" 0.99, score 0 0.9, noul 0.01).
func zzCase(t *testing.T, route string, on bool) (zzOut, []providertest.JevRequest) {
	t.Helper()
	srv := providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "user" {
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "q1", Name: "question", Arguments: `{"question":"Which color?","options":["alpha","beta"]}`}}}
		}
		return providertest.Response{Text: "done"}
	})
	t.Cleanup(srv.Close)
	j := providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
		ans := map[string]any{}
		for name, q := range req.Questions {
			qm, _ := q.(map[string]any)
			switch qm["type"] {
			case "score":
				ans[name] = providertest.JevScoreAnswer(0, 0.9)
			case "noul":
				ans[name] = providertest.JevNoulAnswer(0.01)
			default:
				ans[name] = map[string]any{"type": "choice", "choice": "beta", "confidence": 0.99}
			}
		}
		return providertest.JevResponse{Answers: ans}
	})
	t.Cleanup(j.Close)
	jev := `{"baseURL":"` + j.URL + `","apiKey":"{env:TYPESAFE_API_KEY}"`
	if route != "" {
		jev += `,"route":` + route
	}
	s := zzSess(t, srv, jev+`}`)
	s.SetJevTimeout(500 * time.Millisecond)
	if on {
		if err := s.SetJev(true); err != nil {
			t.Fatal(err)
		}
	}
	return zzTurn(t, s, "pick a color"), j.Requests()
}

const zzRoute = `{"easy":"fake/m2","hard":"fake/m1"}`

func TestZZPosAsked(t *testing.T) {
	for _, c := range []struct {
		route string
		on    bool
		jevN  int
	}{{"", true, 0}, {zzRoute, true, 1}, {zzRoute, false, 0}} {
		o, reqs := zzCase(t, c.route, c.on)
		if !o.asked || o.result != "USER-ANS" || strings.Contains(o.info, "jev answered") {
			t.Fatalf("route %q on %v: %+v", c.route, c.on, o)
		}
		if len(reqs) != c.jevN {
			t.Fatalf("route %q on %v: %d jev requests, want %d", c.route, c.on, len(reqs), c.jevN)
		}
		for _, r := range reqs {
			for name, q := range r.Questions {
				qm, _ := q.(map[string]any)
				if name != "difficulty" || qm["type"] != "score" || strings.Contains(r.State, "Which color") {
					t.Fatalf("jev got %q %v state %q", name, qm, r.State)
				}
			}
		}
	}
}

// TestZZEvil passes only when jev's choice became the answer or jev was asked
// a choice question.
func TestZZEvil(t *testing.T) {
	for _, route := range []string{"", zzRoute} {
		o, reqs := zzCase(t, route, true)
		if !o.asked || o.result == "beta" {
			t.Logf("jev answered: %+v", o)
			return
		}
		for _, r := range reqs {
			for name, q := range r.Questions {
				if qm, _ := q.(map[string]any); qm["type"] == "choice" || name == "answer" {
					t.Logf("choice question sent to jev: %q", name)
					return
				}
			}
		}
	}
	t.Fatal("REJECTED: every question went to the user and jev saw no choice question")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/agent/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: question always asked; jev only sees the routing score question" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/agent TestZZPosAsked
CMD
expect_fail "negative: jev's choice answers a question or jev gets a choice question" <<'CMD'
out="$(go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvil$' ./internal/agent 2>&1)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q -e 'REJECTED:' && exit 1
printf '%s\n' "$out" | grep -q -e '^ok' && exit 0
exit 125
CMD
expect_fail "negative: question-answering code left in internal/agent" <<'CMD'
no_match -rn -e 'JevChoice' -e 'jevAnswer' -e 'jevQuestionState' -e 'turnHistory' -e 'withTurnHistory' -e 'jev answered' internal/agent
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 0c3a7c5 -- go.mod go.sum
CMD
expect_fail "negative: no protected or security-critical path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md CLAUDE.md prompts .claude scripts/verify versions/v5/spec.md versions/v5/plan.md versions/v5/tasks internal/provider internal/tools internal/config internal/agent/approval.go internal/agent/approval_test.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
