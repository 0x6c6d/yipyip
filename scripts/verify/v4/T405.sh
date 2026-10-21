#!/usr/bin/env bash
# Verification for T405 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named jev risk tests (and the gate tests)" <<'CMD'
go_tests ./internal/agent TestJevRisk TestJevRiskSafeUnchanged TestJevRiskManualUnchanged TestJevRiskFallback TestJevRiskState TestRiskScreenManual TestRiskScreenNoAlways
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

func zzSrv(name, args string) *providertest.Server {
	return providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "user" {
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "b1", Name: name, Arguments: args}}}
		}
		return providertest.Response{Text: "done"}
	})
}

func zzJev(resp providertest.JevResponse) *providertest.JevServer {
	return providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
		r := resp
		if a, ok := resp.Answers["risk"]; ok {
			r.Answers = map[string]any{}
			for name := range req.Questions {
				r.Answers[name] = a
			}
		}
		return r
	})
}

func zzNoul(p float64) providertest.JevResponse {
	return providertest.JevResponse{Answers: map[string]any{"risk": providertest.JevNoulAnswer(p)}}
}

type zzOut struct {
	approvals int
	result    string
	jevReqs   []providertest.JevRequest
	s         *Session
}

const zzBash = `{"command":"echo hi SECRET123"}`

func zzRun(t *testing.T, tool, args string, resp providertest.JevResponse, mode Mode, on bool, dec Decision) zzOut {
	t.Helper()
	srv := zzSrv(tool, args)
	t.Cleanup(srv.Close)
	j := zzJev(resp)
	t.Cleanup(j.Close)
	s := zzSess(t, srv, `{"baseURL":"`+j.URL+`","apiKey":"{env:TYPESAFE_API_KEY}"}`)
	s.SetJevTimeout(150 * time.Millisecond)
	s.SetMode(mode)
	if on {
		if err := s.SetJev(true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Submit("go"); err != nil {
		t.Fatal(err)
	}
	o := zzOut{s: s}
	deadline := time.Now().Add(15 * time.Second)
	for done := false; !done; {
		for _, ev := range s.PollEvents() {
			switch ev.Kind {
			case EvApproval:
				o.approvals++
				ev.Approval.Respond(dec)
			case EvToolResult:
				if ev.Tool == tool {
					o.result = ev.Text
				}
			case EvTurnDone:
				done = true
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("turn did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	o.jevReqs = j.Requests()
	return o
}

func TestZZPosRisky(t *testing.T) {
	o := zzRun(t, "bash", zzBash, zzNoul(0.9), ModeAuto, true, Deny)
	if o.approvals != 1 || o.result != DeniedMessage || len(o.jevReqs) != 1 {
		t.Fatalf("risky+Deny: %+v", o)
	}
	st := o.jevReqs[0].State
	if !strings.Contains(st, "bash") || strings.Contains(string(o.jevReqs[0].Body), "SECRET123") || strings.Contains(string(o.jevReqs[0].Body), "JEVKEY-zz") {
		t.Fatalf("state: %q", st)
	}
	o = zzRun(t, "bash", zzBash, zzNoul(0.5), ModeAuto, true, Allow)
	if o.approvals != 1 || !strings.Contains(o.result, "hi") {
		t.Fatalf("risky+Allow: %+v", o)
	}
}

func TestZZPosSafe(t *testing.T) {
	o := zzRun(t, "bash", zzBash, zzNoul(0.1), ModeAuto, true, Deny)
	if o.approvals != 0 || !strings.Contains(o.result, "hi") || len(o.jevReqs) != 1 {
		t.Fatalf("safe: %+v", o)
	}
}

func TestZZPosManual(t *testing.T) {
	for _, p := range []float64{0.9, 0.1} {
		o := zzRun(t, "bash", zzBash, zzNoul(p), ModeManual, true, Deny)
		if o.approvals != 1 || o.result != DeniedMessage || len(o.jevReqs) != 0 {
			t.Fatalf("manual p=%v: %+v", p, o)
		}
	}
}

func TestZZPosOffAndScope(t *testing.T) {
	o := zzRun(t, "bash", zzBash, zzNoul(0.9), ModeAuto, false, Deny)
	if o.approvals != 0 || len(o.jevReqs) != 0 {
		t.Fatalf("jev off: %+v", o)
	}
	o = zzRun(t, "read_file", `{"path":"notes.txt"}`, zzNoul(0.9), ModeAuto, true, Deny)
	if o.approvals != 0 || len(o.jevReqs) != 0 {
		t.Fatalf("read_file: %+v", o)
	}
}

func TestZZPosFallback(t *testing.T) {
	for _, resp := range []providertest.JevResponse{{Status: 500}, {Delay: 5 * time.Second}} {
		o := zzRun(t, "bash", zzBash, resp, ModeAuto, true, Deny)
		if o.approvals != 0 || !strings.Contains(o.result, "hi") || o.s.JevStats().Fallbacks != 1 {
			t.Fatalf("fallback %+v: %+v stats %+v", resp, o, o.s.JevStats())
		}
	}
}

// TestZZEvilManual passes only when manual mode skipped a prompt or asked jev,
// or a secret reached jev.
func TestZZEvilManual(t *testing.T) {
	o := zzRun(t, "bash", zzBash, zzNoul(0.0), ModeManual, true, Deny)
	if o.approvals == 0 || len(o.jevReqs) > 0 {
		return
	}
	o = zzRun(t, "write_file", `{"path":"x.txt","content":"SECRET123"}`, zzNoul(0.9), ModeAuto, true, Deny)
	for _, r := range o.jevReqs {
		if strings.Contains(string(r.Body), "SECRET123") {
			return
		}
	}
	if o.approvals == 0 {
		return
	}
	t.Fatal("REJECTED: manual unchanged, secret kept, risky auto call prompted")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/agent/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: risky prompts, safe, manual, off, scope, fallback" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/agent TestZZPosRisky TestZZPosSafe TestZZPosManual TestZZPosOffAndScope TestZZPosFallback
CMD
expect_fail "negative: manual mode skips a prompt or asks jev, or a secret reaches jev" <<'CMD'
go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvilManual$' ./internal/agent
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
