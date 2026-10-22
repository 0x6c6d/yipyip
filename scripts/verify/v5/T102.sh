#!/usr/bin/env bash
# Verification for T102 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named routing tests (and the kept v4 route tests)" <<'CMD'
go_tests ./internal/agent TestRouteTiers TestRouteMediumMissing TestRouteFallbackError TestRouteFallbackTimeout TestRouteFallbackLowConfidence TestJevRoute TestJevRouteFallback TestJevRouteOff TestJevRouteNoBlock
CMD
expect_ok "exported Tier API" <<'CMD'
has_symbols ./internal/agent Tier TierNone TierEasy TierMedium TierHard Session.TurnTier Session.JevRoute Session.TurnModel
CMD
expect_ok "phase 1 exit criterion" <<'CMD'
go test -count=1 ./internal/config/ ./internal/agent/ -run 'JevRoute|Route'
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

const zzModels = `"provider":{"fake":{"models":{"m3":{"name":"m3"},"m4":{"name":"m4"}}}}`

const zzRoute3 = `{"easy":"fake/m2","medium":"fake/m3","hard":"fake/m4"}`

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
	if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(`{`+zzModels+`,"jev":`+jevJSON+`}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("n\n"), 0o644); err != nil {
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

type zzRig struct {
	srv *providertest.Server
	j   *providertest.JevServer
	s   *Session
}

// zzNewRig: provider turns = read_file call, then text; jev answers resp
// (its "difficulty" answer under every asked question name). route "" = none.
func zzNewRig(t *testing.T, resp providertest.JevResponse, route string, on bool) *zzRig {
	t.Helper()
	r := &zzRig{
		srv: providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
			if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "user" {
				return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "r1", Name: "read_file", Arguments: `{"path":"notes.txt"}`}}}
			}
			return providertest.Response{Text: "done"}
		}),
		j: providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
			out := resp
			if a, ok := resp.Answers["difficulty"]; ok {
				out.Answers = map[string]any{}
				for name := range req.Questions {
					out.Answers[name] = a
				}
			}
			return out
		}),
	}
	t.Cleanup(r.srv.Close)
	t.Cleanup(r.j.Close)
	jev := `{"baseURL":"` + r.j.URL + `","apiKey":"{env:TYPESAFE_API_KEY}"`
	if route != "" {
		jev += `,"route":` + route
	}
	r.s = zzSess(t, r.srv, jev+`}`)
	r.s.SetJevTimeout(150 * time.Millisecond)
	if on {
		if err := r.s.SetJev(true); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

// turn runs one turn; it returns the models of its requests and its duration.
func (r *zzRig) turn(t *testing.T, text string) ([]string, time.Duration) {
	t.Helper()
	before := len(r.srv.Requests())
	start := time.Now()
	if err := r.s.Submit(text); err != nil {
		t.Fatal(err)
	}
	deadline := start.Add(10 * time.Second)
	for done := false; !done; {
		for _, ev := range r.s.PollEvents() {
			switch ev.Kind {
			case EvApproval:
				ev.Approval.Respond(Deny)
			case EvTurnDone:
				done = true
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("turn did not finish")
		}
		time.Sleep(3 * time.Millisecond)
	}
	var models []string
	for _, q := range r.srv.Requests()[before:] {
		models = append(models, q.Model)
	}
	return models, time.Since(start)
}

func zzAll(models []string, want string) bool {
	if len(models) < 2 {
		return false
	}
	for _, m := range models {
		if m != want {
			return false
		}
	}
	return true
}

func zzScore(score, conf float64) providertest.JevResponse {
	return providertest.JevResponse{Answers: map[string]any{"difficulty": providertest.JevScoreAnswer(score, conf)}}
}

func TestZZPosTiers(t *testing.T) {
	for _, c := range []struct {
		score float64
		model string
		tier  Tier
	}{
		{0, "m2", TierEasy}, {0.49, "m2", TierEasy}, {-4, "m2", TierEasy},
		{0.5, "m3", TierMedium}, {1, "m3", TierMedium}, {1.49, "m3", TierMedium},
		{1.5, "m4", TierHard}, {2, "m4", TierHard}, {9, "m4", TierHard},
	} {
		r := zzNewRig(t, zzScore(c.score, 0.9), zzRoute3, true)
		models, _ := r.turn(t, "fix it")
		if !zzAll(models, c.model) || r.s.TurnModel() != "fake/"+c.model || r.s.TurnTier() != c.tier || r.s.ModelID() != "fake/m1" {
			t.Fatalf("score %v: models %q, turn model %q, tier %v, session %q", c.score, models, r.s.TurnModel(), r.s.TurnTier(), r.s.ModelID())
		}
		if st := r.s.JevStats(); st.Fallbacks != 0 {
			t.Fatalf("score %v: fallbacks %d", c.score, st.Fallbacks)
		}
		reqs := r.j.Requests()
		if len(reqs) != 1 || reqs[0].State != "request: fix it" || len(reqs[0].Questions) != 1 || reqs[0].Questions["difficulty"] == nil {
			t.Fatalf("score %v: jev requests %+v", c.score, reqs)
		}
		if qm, _ := reqs[0].Questions["difficulty"].(map[string]any); qm["type"] != "score" {
			t.Fatalf("difficulty question %v", reqs[0].Questions["difficulty"])
		}
	}
	if e, m, h := TierEasy.String(), TierMedium.String(), TierHard.String(); e != "easy" || m != "medium" || h != "hard" || TierNone.String() != "not routed" {
		t.Fatalf("tier names %q %q %q %q", e, m, h, TierNone.String())
	}
}

func TestZZPosMissing(t *testing.T) {
	r := zzNewRig(t, zzScore(1, 0.9), `{"easy":"fake/m2","hard":"fake/m4"}`, true)
	if models, _ := r.turn(t, "x"); !zzAll(models, "m4") || r.s.TurnTier() != TierHard || r.s.TurnModel() != "fake/m4" {
		t.Fatalf("no medium: %q %v", models, r.s.TurnTier())
	}
	if e, m, h := r.s.JevRoute(); e != "fake/m2" || m != "fake/m4" || h != "fake/m4" {
		t.Fatalf("JevRoute two keys: %q %q %q", e, m, h)
	}
	r = zzNewRig(t, zzScore(0, 0.9), `{"medium":"fake/m3","hard":"fake/m4"}`, true)
	if models, _ := r.turn(t, "x"); !zzAll(models, "m4") || r.s.TurnTier() != TierHard {
		t.Fatalf("no easy: %q %v", models, r.s.TurnTier())
	}
	if e, m, h := r.s.JevRoute(); e != "fake/m4" || m != "fake/m3" || h != "fake/m4" {
		t.Fatalf("JevRoute no easy: %q %q %q", e, m, h)
	}
	r = zzNewRig(t, zzScore(2, 0.9), zzRoute3, true)
	if e, m, h := r.s.JevRoute(); e != "fake/m2" || m != "fake/m3" || h != "fake/m4" {
		t.Fatalf("JevRoute three keys: %q %q %q", e, m, h)
	}
	r = zzNewRig(t, zzScore(2, 0.9), "", true)
	if e, m, h := r.s.JevRoute(); e != "" || m != "" || h != "" {
		t.Fatalf("JevRoute without route: %q %q %q", e, m, h)
	}
	if models, _ := r.turn(t, "x"); !zzAll(models, "m1") || len(r.j.Requests()) != 0 || r.s.TurnTier() != TierNone {
		t.Fatalf("no route: %q, %d jev requests", models, len(r.j.Requests()))
	}
}

func TestZZPosFallbacks(t *testing.T) {
	for name, resp := range map[string]providertest.JevResponse{
		"500":     {Status: 500},
		"lowconf": zzScore(2, 0.3),
		"timeout": {Delay: 4 * time.Second},
	} {
		r := zzNewRig(t, resp, zzRoute3, true)
		models, took := r.turn(t, "fix it")
		if !zzAll(models, "m1") || r.s.TurnModel() != "" || r.s.TurnTier() != TierNone || r.s.ModelID() != "fake/m1" {
			t.Fatalf("%s: models %q, turn model %q, tier %v", name, models, r.s.TurnModel(), r.s.TurnTier())
		}
		if st := r.s.JevStats(); st.Fallbacks != 1 {
			t.Fatalf("%s: fallbacks %d", name, st.Fallbacks)
		}
		if took > 2500*time.Millisecond {
			t.Fatalf("%s: turn took %v", name, took)
		}
	}
}

// TestZZEvil passes only when a failed or unconfident jev answer runs a routed
// model, routing changes the session model, jev off still sends, or a secret
// reaches jev.
func TestZZEvil(t *testing.T) {
	for _, resp := range []providertest.JevResponse{{Status: 500}, zzScore(0, 0.69), zzScore(2, 0.1), {Status: 422}} {
		r := zzNewRig(t, resp, zzRoute3, true)
		models, _ := r.turn(t, "fix it")
		for _, m := range models {
			if m != "m1" {
				t.Logf("routed on a failed answer %+v: %q", resp, models)
				return
			}
		}
	}
	r := zzNewRig(t, zzScore(0, 0.99), zzRoute3, true)
	r.turn(t, "small SECRET123 fix")
	if r.s.ModelID() != "fake/m1" {
		t.Log("session model changed by routing")
		return
	}
	for _, q := range r.j.Requests() {
		if strings.Contains(string(q.Body), "SECRET123") || strings.Contains(string(q.Body), "JEVKEY-zz") {
			t.Log("secret sent to jev")
			return
		}
	}
	off := zzNewRig(t, zzScore(0, 0.99), zzRoute3, false)
	if models, _ := off.turn(t, "x"); len(off.j.Requests()) != 0 || !zzAll(models, "m1") {
		t.Log("jev off still routed")
		return
	}
	t.Fatal("REJECTED: fallbacks keep the session model, jev off sends nothing, no secret sent")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/agent/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: tiers by rounded score, missing tiers, JevRoute, fallbacks" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/agent TestZZPosTiers TestZZPosMissing TestZZPosFallbacks
CMD
expect_fail "negative: failed answer routed, session model changed, jev off sends, or secret sent" <<'CMD'
out="$(go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvil$' ./internal/agent 2>&1)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q -e 'REJECTED:' && exit 1
printf '%s\n' "$out" | grep -q -e '^ok' && exit 0
exit 125
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 0c3a7c5 -- go.mod go.sum
CMD
expect_fail "negative: no protected or security-critical path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md CLAUDE.md prompts .claude scripts/verify versions/v5/spec.md versions/v5/plan.md versions/v5/tasks internal/provider internal/tools internal/agent/approval.go internal/agent/question.go internal/config/redact.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
