#!/usr/bin/env bash
# Verification for T401 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named jev core tests" <<'CMD'
go_tests ./internal/agent TestJevSessionToggle TestJevAskRedacts TestJevAskOff TestJevAskTimeout TestJevStatsTokens TestJevSessionClient
CMD
expect_ok "exported API present" <<'CMD'
has_symbols ./internal/agent ErrJevNotConfigured ErrJevOff JevTimeout JevStateLimit JevStats Session.JevConfigured Session.SetJev Session.JevOn Session.JevStats Session.SetJevTimeout
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"yipyip/internal/config"
	"yipyip/internal/provider"
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

func zzJevCfg(j *providertest.JevServer) string {
	return `{"baseURL":"` + j.URL + `","apiKey":"{env:TYPESAFE_API_KEY}"}`
}

var zzQ = []provider.JevQuestion{{Name: "r", Type: provider.JevNoul, Instructions: "risky?"}}

func zzJevServe(resp providertest.JevResponse) *providertest.JevServer {
	return providertest.NewJev(func(int, providertest.JevRequest) providertest.JevResponse { return resp })
}

func TestZZPosToggle(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	j := zzJevServe(providertest.JevResponse{})
	defer j.Close()
	s := zzSess(t, srv, zzJevCfg(j))
	if !s.JevConfigured() || s.JevOn() {
		t.Fatal("want configured and off at start")
	}
	if err := s.SetJev(true); err != nil || !s.JevOn() {
		t.Fatalf("on: %v", err)
	}
	if err := s.SetJev(false); err != nil || s.JevOn() {
		t.Fatalf("off: %v", err)
	}
	for _, cfg := range []string{"", `{"baseURL":"` + j.URL + `"}`} {
		u := zzSess(t, srv, cfg)
		if err := u.SetJev(true); !errors.Is(err, ErrJevNotConfigured) || u.JevOn() || u.JevConfigured() {
			t.Fatalf("unconfigured %q: %v", cfg, err)
		}
	}
}

func TestZZPosAskRedacts(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	j := zzJevServe(providertest.JevResponse{Answers: map[string]any{"r": providertest.JevNoulAnswer(0.3)}, Usage: &providertest.JevUsage{Input: 33, Output: 1}})
	defer j.Close()
	s := zzSess(t, srv, zzJevCfg(j))
	if err := s.SetJev(true); err != nil {
		t.Fatal(err)
	}
	ans, err := s.askJev(context.Background(), "key SECRET123 and JEVKEY-zz "+strings.Repeat("x", 5000), zzQ)
	if err != nil || len(ans) != 1 || ans[0].Noul != 0.3 {
		t.Fatalf("ask: %+v %v", ans, err)
	}
	r := j.Requests()
	if len(r) != 1 {
		t.Fatalf("%d requests", len(r))
	}
	st := r[0].State
	if strings.Contains(st, "SECRET123") || strings.Contains(st, "JEVKEY-zz") || !strings.Contains(st, "[redacted]") || utf8.RuneCountInString(st) > JevStateLimit || JevStateLimit != 2000 {
		t.Fatalf("state (%d runes): %.80q", utf8.RuneCountInString(st), st)
	}
	if strings.Contains(string(r[0].Body), "SECRET123") {
		t.Fatal("secret in body")
	}
	if got := s.JevStats().InputTokens; got != 33 {
		t.Fatalf("InputTokens = %d", got)
	}
}

func TestZZPosAskOff(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	j := zzJevServe(providertest.JevResponse{Answers: map[string]any{"r": providertest.JevNoulAnswer(0.3)}})
	defer j.Close()
	s := zzSess(t, srv, zzJevCfg(j))
	if _, err := s.askJev(context.Background(), "s", zzQ); !errors.Is(err, ErrJevOff) || len(j.Requests()) != 0 {
		t.Fatalf("off: %v, %d requests", err, len(j.Requests()))
	}
}

func TestZZPosAskTimeout(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	j := zzJevServe(providertest.JevResponse{Delay: 5 * time.Second, Answers: map[string]any{"r": providertest.JevNoulAnswer(0.3)}})
	defer j.Close()
	s := zzSess(t, srv, zzJevCfg(j))
	if JevTimeout != 10*time.Second {
		t.Fatalf("JevTimeout = %v", JevTimeout)
	}
	s.SetJevTimeout(150 * time.Millisecond)
	if err := s.SetJev(true); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := s.askJev(context.Background(), "s", zzQ); err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("timeout: %v after %v", err, time.Since(start))
	}
}

func TestZZPosTokensOther(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "hi", Usage: &providertest.Usage{Prompt: 7, Completion: 3}})
	defer srv.Close()
	s := zzSess(t, srv, "")
	if err := s.Submit("hello"); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	st := s.JevStats()
	if st.OtherTokens != 10 || st.QuestionTokens != 0 || st.RoutingTokens != 0 {
		t.Fatalf("stats: %+v", st)
	}
}

// TestZZEvilLeak passes only when a secret reaches jev or jev is asked while off.
func TestZZEvilLeak(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	j := zzJevServe(providertest.JevResponse{Answers: map[string]any{"r": providertest.JevNoulAnswer(0.3)}})
	defer j.Close()
	s := zzSess(t, srv, zzJevCfg(j))
	_, _ = s.askJev(context.Background(), "SECRET123", zzQ)
	if len(j.Requests()) > 0 {
		return
	}
	_ = s.SetJev(true)
	_, _ = s.askJev(context.Background(), strings.Repeat("y", 1990)+"SECRET123", zzQ)
	for _, r := range j.Requests() {
		if strings.Contains(string(r.Body), "SECRET") {
			return
		}
	}
	t.Fatal("REJECTED: no request while off, secret never sent")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/agent/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: toggle, redaction and limit, off, timeout, token split" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/agent TestZZPosToggle TestZZPosAskRedacts TestZZPosAskOff TestZZPosAskTimeout TestZZPosTokensOther
CMD
expect_fail "negative: secret sent to jev or jev asked while off" <<'CMD'
go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvilLeak$' ./internal/agent
CMD
expect_fail "negative: jev.go writes no files" <<'CMD'
no_match -n -e 'os.WriteFile' -e 'os.Create' -e 'os.OpenFile' internal/agent/jev.go
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
