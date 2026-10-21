#!/usr/bin/env bash
# Verification for T403 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named jev routing tests" <<'CMD'
go_tests ./internal/agent TestJevRoute TestJevRouteFallback TestJevRouteOff TestJevRouteNoBlock TestJevSessionToggle
CMD
expect_ok "exported API present" <<'CMD'
has_symbols ./internal/agent Session.TurnModel
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
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if jevJSON != "" {
		if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(`{"jev":`+jevJSON+`}`), 0o644); err != nil {
			t.Fatal(err)
		}
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

// zzSrv: a user message gets a read_file call, a tool result gets "done".
func zzSrv() *providertest.Server {
	return providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "user" {
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "r1", Name: "read_file", Arguments: `{"path":"notes.txt"}`}}}
		}
		return providertest.Response{Text: "done"}
	})
}

func zzJev(resp providertest.JevResponse) *providertest.JevServer {
	return providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
		r := resp
		if a, ok := resp.Answers["difficulty"]; ok {
			r.Answers = map[string]any{}
			for name := range req.Questions {
				r.Answers[name] = a
			}
		}
		return r
	})
}

func zzScore(sc, conf float64) providertest.JevResponse {
	return providertest.JevResponse{Answers: map[string]any{"difficulty": providertest.JevScoreAnswer(sc, conf)}}
}

const zzRoute = `,"route":{"easy":"fake/m2","hard":"fake/m1"}`

type zzRig struct {
	srv *providertest.Server
	j   *providertest.JevServer
	s   *Session
}

func zzSetup(t *testing.T, resp providertest.JevResponse, extra string, on bool) *zzRig {
	t.Helper()
	r := &zzRig{srv: zzSrv(), j: zzJev(resp)}
	t.Cleanup(r.srv.Close)
	t.Cleanup(r.j.Close)
	r.s = zzSess(t, r.srv, `{"baseURL":"`+r.j.URL+`","apiKey":"{env:TYPESAFE_API_KEY}"`+extra+`}`)
	r.s.SetJevTimeout(150 * time.Millisecond)
	if on {
		if err := r.s.SetJev(true); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

// turn runs one turn (approvals denied) and returns the models of its requests.
func (r *zzRig) turn(t *testing.T, text string) []string {
	t.Helper()
	before := len(r.srv.Requests())
	if err := r.s.Submit(text); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for done := false; !done; {
		for _, ev := range r.s.PollEvents() {
			if ev.Kind == EvApproval {
				ev.Approval.Respond(Deny)
			}
			if ev.Kind == EvTurnDone {
				done = true
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("turn did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	var models []string
	for _, q := range r.srv.Requests()[before:] {
		models = append(models, q.Model)
	}
	return models
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

func TestZZPosEasyHard(t *testing.T) {
	for _, sc := range []float64{0, 0.99} {
		r := zzSetup(t, zzScore(sc, 0.9), zzRoute, true)
		if got := r.turn(t, "small fix please"); !zzAll(got, "m2") || r.s.TurnModel() != "fake/m2" || r.s.ModelID() != "fake/m1" {
			t.Fatalf("score %v: models %q, turn model %q, session %q", sc, got, r.s.TurnModel(), r.s.ModelID())
		}
		if len(r.j.Requests()) != 1 || r.j.Requests()[0].State == "" {
			t.Fatalf("score %v: jev requests %+v", sc, r.j.Requests())
		}
	}
	for _, sc := range []float64{1, 2} {
		r := zzSetup(t, zzScore(sc, 0.9), zzRoute, true)
		if err := r.s.SetModel("fake/m2"); err != nil {
			t.Fatal(err)
		}
		if got := r.turn(t, "big refactor"); !zzAll(got, "m1") || r.s.TurnModel() != "fake/m1" || r.s.ModelID() != "fake/m2" {
			t.Fatalf("score %v: models %q, turn model %q", sc, got, r.s.TurnModel())
		}
		r.turn(t, "again")
		if len(r.j.Requests()) != 2 {
			t.Fatalf("second turn not routed: %d jev requests", len(r.j.Requests()))
		}
	}
}

func TestZZPosFallback(t *testing.T) {
	for _, resp := range []providertest.JevResponse{{Status: 500}, {Delay: 5 * time.Second}, zzScore(0, 0.2)} {
		r := zzSetup(t, resp, zzRoute, true)
		if got := r.turn(t, "x"); !zzAll(got, "m1") || r.s.TurnModel() != "" || r.s.JevStats().Fallbacks != 1 {
			t.Fatalf("fallback %+v: models %q, turn model %q, stats %+v", resp, got, r.s.TurnModel(), r.s.JevStats())
		}
	}
}

func TestZZPosOff(t *testing.T) {
	for _, c := range []struct {
		extra string
		on    bool
	}{{zzRoute, false}, {"", true}} {
		r := zzSetup(t, zzScore(0, 0.9), c.extra, c.on)
		if got := r.turn(t, "x"); !zzAll(got, "m1") || len(r.j.Requests()) != 0 {
			t.Fatalf("off case %+v: models %q, %d jev requests", c, got, len(r.j.Requests()))
		}
	}
}

func TestZZPosNoBlock(t *testing.T) {
	r := zzSetup(t, providertest.JevResponse{Delay: 5 * time.Second}, zzRoute, true)
	start := time.Now()
	if err := r.s.Submit("x"); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("Submit blocked %v", d)
	}
	r.s.Wait()
}

// TestZZEvilSwitch passes only when a fallback or an off case switched models
// or routing changed the session's model.
func TestZZEvilSwitch(t *testing.T) {
	r := zzSetup(t, zzScore(0, 0.1), zzRoute, true)
	if got := r.turn(t, "x"); len(got) > 0 && got[0] == "m2" {
		return
	}
	r = zzSetup(t, zzScore(0, 0.9), zzRoute, false)
	if got := r.turn(t, "x"); (len(got) > 0 && got[0] == "m2") || len(r.j.Requests()) > 0 {
		return
	}
	r = zzSetup(t, zzScore(0, 0.9), zzRoute, true)
	r.turn(t, "x")
	if r.s.ModelID() != "fake/m1" {
		return
	}
	t.Fatal("REJECTED: fallback and off keep the model, session model untouched")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/agent/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: easy/hard for the whole turn, fallbacks, off cases, non-blocking submit" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/agent TestZZPosEasyHard TestZZPosFallback TestZZPosOff TestZZPosNoBlock
CMD
expect_fail "negative: a fallback or an off case switches the model" <<'CMD'
go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvilSwitch$' ./internal/agent
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 4fabda9 -- go.mod go.sum
CMD
expect_fail "negative: no protected or security-critical path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v4/spec.md versions/v4/plan.md versions/v4/tasks internal/provider/transport.go internal/tools internal/agent/approval.go internal/config/redact.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
