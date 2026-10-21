#!/usr/bin/env bash
# Verification for T303 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named jev client tests" <<'CMD'
go_tests ./internal/provider TestJevRequestShape TestJevAnswerTypes TestJev422 TestJevForeignHostRefused TestJevBadResponse TestJevInvalidQuestions TestJevContextTimeout
CMD
expect_ok "exported API present" <<'CMD'
has_symbols ./internal/provider JevClient NewJevClient JevClient.Ask JevQuestion JevAnswer JevUsage ErrJev JevChoice JevScore JevNoul
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package provider_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

func zzQs() []provider.JevQuestion {
	return []provider.JevQuestion{
		{Name: "pick", Type: provider.JevChoice, Instructions: "choose", Choices: map[string]string{"yes": "Y", "no": "N"}},
		{Name: "level", Type: provider.JevScore, Instructions: "rate", Levels: []string{"easy", "medium", "hard"}},
		{Name: "risk", Type: provider.JevNoul, Instructions: "risky?"},
	}
}

func zzAll() map[string]any {
	return map[string]any{"pick": providertest.JevChoiceAnswer("no", 0.85), "level": providertest.JevScoreAnswer(1.5, 0.6), "risk": providertest.JevNoulAnswer(0.7)}
}

func zzClient(t *testing.T, srv *providertest.JevServer, allow []string) *provider.JevClient {
	t.Helper()
	hc, err := provider.NewHTTPClient(allow)
	if err != nil {
		t.Fatal(err)
	}
	return provider.NewJevClient(hc, srv.URL+"/", "K-zz-key", "jev-latest")
}

func zzServe(resp providertest.JevResponse) *providertest.JevServer {
	return providertest.NewJev(func(int, providertest.JevRequest) providertest.JevResponse { return resp })
}

func TestZZPosShapeAndAnswers(t *testing.T) {
	srv := zzServe(providertest.JevResponse{Answers: zzAll(), Usage: &providertest.JevUsage{Input: 40, Output: 2}})
	defer srv.Close()
	ans, u, err := zzClient(t, srv, []string{srv.URL}).Ask(context.Background(), "STATE-zz", zzQs())
	if err != nil {
		t.Fatal(err)
	}
	if len(ans) != 3 || ans[0].Name != "pick" || ans[0].Type != provider.JevChoice || ans[0].Choice != "no" || ans[0].Confidence != 0.85 ||
		ans[1].Name != "level" || ans[1].Score != 1.5 || ans[1].Confidence != 0.6 || ans[2].Name != "risk" || ans[2].Noul != 0.7 {
		t.Fatalf("answers: %+v", ans)
	}
	if u.Input != 40 || u.Output != 2 {
		t.Fatalf("usage: %+v", u)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	r := reqs[0]
	if r.Method != "POST" || r.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer K-zz-key" || !strings.Contains(r.Header.Get("Content-Type"), "application/json") || r.Model != "jev-latest" || r.State != "STATE-zz" {
		t.Fatalf("request: %+v", r)
	}
	pick, _ := r.Questions["pick"].(map[string]any)
	crit, _ := pick["criteria"].(map[string]any)
	level, _ := r.Questions["level"].(map[string]any)
	lv, _ := level["criteria"].([]any)
	risk, _ := r.Questions["risk"].(map[string]any)
	if pick["type"] != "choice" || pick["instructions"] != "choose" || crit["yes"] != "Y" || crit["no"] != "N" || level["type"] != "score" || len(lv) != 3 || lv[0] != "easy" || lv[2] != "hard" || risk["type"] != "noul" {
		t.Fatalf("questions: %v", r.Questions)
	}
	if _, ok := risk["criteria"]; ok {
		t.Fatalf("noul has criteria: %v", risk)
	}
}

func TestZZPosNoUsage(t *testing.T) {
	srv := zzServe(providertest.JevResponse{Answers: zzAll()})
	defer srv.Close()
	_, u, err := zzClient(t, srv, []string{srv.URL}).Ask(context.Background(), "s", zzQs())
	if err != nil || u.Input < 1 || u.Output < 1 {
		t.Fatalf("estimate: %+v %v", u, err)
	}
}

func zzFails(t *testing.T, resp providertest.JevResponse, sub string) {
	t.Helper()
	srv := zzServe(resp)
	defer srv.Close()
	ans, _, err := zzClient(t, srv, []string{srv.URL}).Ask(context.Background(), "s", zzQs())
	if err == nil || !errors.Is(err, provider.ErrJev) || ans != nil {
		t.Fatalf("want ErrJev, got %v %+v", err, ans)
	}
	if sub != "" && !strings.Contains(err.Error(), sub) {
		t.Fatalf("error %q lacks %q", err, sub)
	}
	if strings.Contains(err.Error(), "K-zz-key") {
		t.Fatalf("key in error %q", err)
	}
}

func TestZZPosFailures(t *testing.T) {
	zzFails(t, providertest.JevResponse{Status: 422}, "422")
	zzFails(t, providertest.JevResponse{Status: 500}, "")
	zzFails(t, providertest.JevResponse{Body: "not json"}, "")
	part := zzAll()
	delete(part, "risk")
	zzFails(t, providertest.JevResponse{Answers: part}, "")
	wrong := zzAll()
	wrong["pick"] = providertest.JevNoulAnswer(0.1)
	zzFails(t, providertest.JevResponse{Answers: wrong}, "")
}

func TestZZPosTimeout(t *testing.T) {
	srv := zzServe(providertest.JevResponse{Delay: 5 * time.Second, Answers: zzAll()})
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := zzClient(t, srv, []string{srv.URL}).Ask(ctx, "s", zzQs())
	if err == nil || !errors.Is(err, provider.ErrJev) || time.Since(start) > 2*time.Second {
		t.Fatalf("timeout: %v after %v", err, time.Since(start))
	}
}

func TestZZPosInvalid(t *testing.T) {
	srv := zzServe(providertest.JevResponse{Answers: zzAll()})
	defer srv.Close()
	c := zzClient(t, srv, []string{srv.URL})
	many := make([]string, 11)
	for i := range many {
		many[i] = "l"
	}
	for _, qs := range [][]provider.JevQuestion{
		nil,
		{{Name: "x", Type: "box"}},
		{{Name: "x", Type: provider.JevChoice}},
		{{Name: "x", Type: provider.JevScore, Levels: many}},
		{{Name: "", Type: provider.JevNoul}},
		{{Name: "d", Type: provider.JevNoul}, {Name: "d", Type: provider.JevNoul}},
	} {
		if _, _, err := c.Ask(context.Background(), "s", qs); err == nil || !errors.Is(err, provider.ErrJev) {
			t.Fatalf("accepted %+v: %v", qs, err)
		}
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("%d requests for invalid questions", n)
	}
}

func TestZZPosForeign(t *testing.T) {
	srv := zzServe(providertest.JevResponse{Answers: zzAll()})
	defer srv.Close()
	_, _, err := zzClient(t, srv, []string{"http://127.0.0.1:1"}).Ask(context.Background(), "s", zzQs())
	if err == nil || !errors.Is(err, provider.ErrHostNotAllowed) || !errors.Is(err, provider.ErrJev) || len(srv.Requests()) != 0 {
		t.Fatalf("foreign: %v, %d requests", err, len(srv.Requests()))
	}
}

// TestZZEvilForeign passes only when a host outside the allowlist was contacted
// or the key leaked into an error.
func TestZZEvilForeign(t *testing.T) {
	srv := zzServe(providertest.JevResponse{Status: 401, Body: "bad key"})
	defer srv.Close()
	if _, _, err := zzClient(t, srv, nil).Ask(context.Background(), "s", zzQs()); err == nil || len(srv.Requests()) > 0 {
		return
	}
	if _, _, err := zzClient(t, srv, []string{srv.URL}).Ask(context.Background(), "s", zzQs()); err == nil || strings.Contains(err.Error(), "K-zz-key") {
		return
	}
	t.Fatal("REJECTED: foreign host refused and key kept out of errors")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/provider/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: shape, answers, usage estimate, failures, timeout, invalid, foreign host" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/provider TestZZPosShapeAndAnswers TestZZPosNoUsage TestZZPosFailures TestZZPosTimeout TestZZPosInvalid TestZZPosForeign
CMD
expect_fail "negative: foreign host contacted or key leaked" <<'CMD'
go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvilForeign$' ./internal/provider
CMD
expect_fail "negative: jev.go builds no client or transport of its own" <<'CMD'
no_match -n -e 'http.Client{' -e 'http.Transport{' -e 'DefaultClient' -e 'DefaultTransport' -e 'http.Get(' -e 'http.Post(' -e 'NewTransport(' internal/provider/jev.go
CMD
expect_fail "negative: no real host in tests" <<'CMD'
no_match -n -e 'typesafe.ai' internal/provider/jev_test.go
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
