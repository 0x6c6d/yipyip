#!/usr/bin/env bash
# Verification for T402 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named jev question tests (and existing question tests)" <<'CMD'
go_tests ./internal/agent TestJevQuestion TestJevQuestionState TestJevFallback TestJevAskRedacts
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
	deadline := time.Now().Add(15 * time.Second)
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
		time.Sleep(5 * time.Millisecond)
	}
}

func zzQSrv(args string) *providertest.Server {
	return providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "user" {
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "q1", Name: "question", Arguments: args}}}
		}
		return providertest.Response{Text: "done"}
	})
}

const zzArgs2 = `{"question":"Which color SECRET123?","options":["alpha","beta"]}`

// zzCase runs one turn: question args, jev reply, jev on/off; it returns the
// outcome, the jev requests and the session.
func zzCase(t *testing.T, args string, resp providertest.JevResponse, on bool) (zzOut, []providertest.JevRequest, *Session) {
	t.Helper()
	srv := zzQSrv(args)
	t.Cleanup(srv.Close)
	j := providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
		r := resp
		if a, ok := resp.Answers["answer"]; ok {
			r.Answers = map[string]any{}
			for name := range req.Questions {
				r.Answers[name] = a
			}
		}
		return r
	})
	t.Cleanup(j.Close)
	s := zzSess(t, srv, `{"baseURL":"`+j.URL+`","apiKey":"{env:TYPESAFE_API_KEY}"}`)
	s.SetJevTimeout(150 * time.Millisecond)
	if on {
		if err := s.SetJev(true); err != nil {
			t.Fatal(err)
		}
	}
	o := zzTurn(t, s, "please pick MSG-zz SECRET123")
	return o, j.Requests(), s
}

func zzChoice(c string, conf float64) providertest.JevResponse {
	return providertest.JevResponse{Answers: map[string]any{"answer": providertest.JevChoiceAnswer(c, conf)}}
}

// zzChoiceAny answers whatever question name was asked (see zzCase).
func zzChoiceAny(c string, conf float64) providertest.JevResponse {
	return zzChoice(c, conf)
}

func TestZZPosConfident(t *testing.T) {
	srv := zzQSrv(zzArgs2)
	defer srv.Close()
	j := providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
		ans := map[string]any{}
		for name := range req.Questions {
			ans[name] = providertest.JevChoiceAnswer("beta", 0.9)
		}
		return providertest.JevResponse{Answers: ans}
	})
	defer j.Close()
	s := zzSess(t, srv, `{"baseURL":"`+j.URL+`","apiKey":"{env:TYPESAFE_API_KEY}"}`)
	if err := s.SetJev(true); err != nil {
		t.Fatal(err)
	}
	o := zzTurn(t, s, "please pick MSG-zz SECRET123")
	if o.asked || o.result != "beta" || !strings.Contains(o.info, "beta") {
		t.Fatalf("confident: %+v", o)
	}
	if st := s.JevStats(); st.Answered != 1 || st.Fallbacks != 0 {
		t.Fatalf("stats: %+v", st)
	}
	r := j.Requests()
	if len(r) != 1 || len(r[0].Questions) != 1 {
		t.Fatalf("jev requests: %+v", r)
	}
	for _, q := range r[0].Questions {
		qm, _ := q.(map[string]any)
		crit, _ := qm["criteria"].(map[string]any)
		if qm["type"] != "choice" || len(crit) != 2 || crit["alpha"] == nil || crit["beta"] == nil {
			t.Fatalf("question: %v", qm)
		}
	}
	st := r[0].State
	for _, want := range []string{"Which color", "alpha", "beta", "MSG-zz"} {
		if !strings.Contains(st, want) {
			t.Fatalf("state lacks %q: %q", want, st)
		}
	}
	if strings.Contains(string(r[0].Body), "SECRET123") || strings.Contains(string(r[0].Body), "JEVKEY-zz") {
		t.Fatal("secret in jev request")
	}
}

func TestZZPosPrompts(t *testing.T) {
	// asked, jev consulted
	for _, resp := range []providertest.JevResponse{zzChoiceAny("beta", 0.3), zzChoiceAny("gamma", 0.99), {Status: 500}, {Status: 422}, {Delay: 5 * time.Second}} {
		o, reqs, s := zzCase(t, zzArgs2, resp, true)
		if !o.asked || o.result != "USER-ANS" || len(reqs) != 1 {
			t.Fatalf("fallback %+v: %+v, %d jev requests", resp, o, len(reqs))
		}
		if st := s.JevStats(); st.Fallbacks != 1 || st.Answered != 0 {
			t.Fatalf("stats after fallback: %+v", st)
		}
	}
	// asked, jev never consulted
	for _, c := range []struct {
		args string
		on   bool
	}{
		{`{"question":"Q?","options":["a","b"],"multiple":true}`, true},
		{`{"question":"Q?","options":["only"]}`, true},
		{`{"question":"Free?"}`, true},
		{zzArgs2, false},
	} {
		o, reqs, _ := zzCase(t, c.args, zzChoiceAny("a", 0.99), c.on)
		if !o.asked || len(reqs) != 0 {
			t.Fatalf("%s on=%v: %+v, %d jev requests", c.args, c.on, o, len(reqs))
		}
	}
}

// TestZZEvilAnswers passes only when jev answered a multi-select or a
// low-confidence question.
func TestZZEvilAnswers(t *testing.T) {
	all := zzChoiceAny
	if o, _, _ := zzCase(t, `{"question":"Q?","options":["a","b"],"multiple":true}`, all("a", 0.99), true); !o.asked {
		return
	}
	if o, _, _ := zzCase(t, `{"question":"Q?","options":["a","b"]}`, all("a", 0.2), true); !o.asked {
		return
	}
	t.Fatal("REJECTED: multi-select and low confidence went to the user")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/agent/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: confident answer, fallbacks, cases jev never sees" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/agent TestZZPosConfident TestZZPosPrompts
CMD
expect_fail "negative: jev answers a multi-select or low-confidence question" <<'CMD'
go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvilAnswers$' ./internal/agent
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
