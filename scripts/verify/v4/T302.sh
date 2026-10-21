#!/usr/bin/env bash
# Verification for T302 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named fake test and existing providertest tests" <<'CMD'
go_tests ./internal/provider/providertest TestJevFakeEndpoint TestFakeTextStream TestConfigJSON
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package providertest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func zzPost(t *testing.T, ctx context.Context, url, body string) (int, map[string]any, string, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer K-zz")
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return 0, nil, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var js map[string]any
	_ = json.Unmarshal(b, &js)
	return resp.StatusCode, js, string(b), nil
}

func TestZZPosJevFake(t *testing.T) {
	s := NewJev(func(n int, req JevRequest) JevResponse {
		switch n {
		case 0:
			return JevResponse{Answers: map[string]any{"q": JevChoiceAnswer("b", 0.9), "s": JevScoreAnswer(2, 0.8), "r": JevNoulAnswer(0.6)}, Usage: &JevUsage{Input: 12, Output: 3}}
		case 1:
			return JevResponse{Status: 422}
		case 2:
			return JevResponse{Status: 500, Body: "boom-zz"}
		default:
			return JevResponse{Delay: 5 * time.Second, Answers: map[string]any{"r": JevNoulAnswer(0.2)}}
		}
	})
	defer s.Close()
	if strings.HasSuffix(s.URL, "/v1") || !strings.HasPrefix(s.URL, "http://127.0.0.1:") {
		t.Fatalf("URL %q", s.URL)
	}
	body := `{"model":"jev-latest","state":"S1","questions":{"q":{"type":"choice","instructions":"pick","criteria":{"a":"A","b":"B"}}}}`
	ctx := context.Background()
	code, js, _, err := zzPost(t, ctx, s.URL+"/v1/systemone", body)
	if err != nil || code != 200 || js["model"] != "jev-latest" {
		t.Fatalf("success: %d %v %v", code, js, err)
	}
	ans := js["answers"].(map[string]any)
	q := ans["q"].(map[string]any)
	sc := ans["s"].(map[string]any)
	r := ans["r"].(map[string]any)
	if q["type"] != "choice" || q["choice"] != "b" || q["confidence"] != 0.9 || sc["type"] != "score" || sc["score"] != 2.0 || sc["confidence"] != 0.8 || r["type"] != "noul" || r["noul"] != 0.6 {
		t.Fatalf("answers: %v", ans)
	}
	u := js["usage"].(map[string]any)
	if u["input_tokens"] != 12.0 || u["output_tokens"] != 3.0 {
		t.Fatalf("usage: %v", u)
	}
	code, js, _, _ = zzPost(t, ctx, s.URL+"/v1/systemone", body)
	if code != 422 || js["detail"] == nil {
		t.Fatalf("422: %d %v", code, js)
	}
	code, _, raw, _ := zzPost(t, ctx, s.URL+"/v1/systemone", body)
	if code != 500 || raw != "boom-zz" {
		t.Fatalf("verbatim: %d %q", code, raw)
	}
	code, _, _, _ = zzPost(t, ctx, s.URL+"/v1/other", body)
	if code != 404 {
		t.Fatalf("other path: %d", code)
	}
	cctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, _, _, err := zzPost(t, cctx, s.URL+"/v1/systemone", body); err == nil {
		t.Fatal("delayed reply was not cut")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("delay not cut: %v", d)
	}
	reqs := s.Requests()
	if len(reqs) != 4 {
		t.Fatalf("recorded %d requests, want 4 (404 not recorded)", len(reqs))
	}
	r0 := reqs[0]
	if r0.Method != "POST" || r0.Path != "/v1/systemone" || r0.Header.Get("Authorization") != "Bearer K-zz" || r0.Model != "jev-latest" || r0.State != "S1" || r0.Questions["q"] == nil || string(r0.Body) != body || r0.JSON["state"] != "S1" {
		t.Fatalf("recorded: %+v", r0)
	}
}

func TestZZPosNoUsage(t *testing.T) {
	s := NewJev(func(int, JevRequest) JevResponse { return JevResponse{Answers: map[string]any{}} })
	defer s.Close()
	_, js, _, err := zzPost(t, context.Background(), s.URL+"/v1/systemone", `{"model":"m","state":"","questions":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := js["usage"]; ok {
		t.Fatalf("usage present without Usage: %v", js)
	}
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/provider/providertest/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: success, 422, verbatim, 404, delay, no usage" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/provider/providertest TestZZPosJevFake TestZZPosNoUsage
CMD
expect_fail "negative: providertest imports no yipyip package" <<'CMD'
no_match -rn --include='*.go' -e '"yipyip/' internal/provider/providertest
CMD
expect_fail "negative: the fake names no real host" <<'CMD'
no_match -n -e 'typesafe.ai' -e 'https://' internal/provider/providertest/jev.go
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
