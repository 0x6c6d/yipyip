#!/usr/bin/env bash
# Verification for T203 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named e2e web_search Secret tests" <<'CMD'
go_tests ./e2e TestSecretWebSearchE2E TestSecretBraveKeyNotInBashEnv
CMD
expect_ok "spec acceptance: go test ./internal/agent ./e2e -run Secret" <<'CMD'
go test -count=1 ./internal/agent ./e2e -run Secret
CMD
expect_ok "whole e2e package" <<'CMD'
go test -count=1 ./e2e
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package e2e

import (
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

// TestZZE2EWebSearchEvil passes (exit 0) only when a secret reaches the
// search service, the view or the model, or the search did not happen.
func TestZZE2EWebSearchEvil(t *testing.T) {
	b := providertest.NewBrave(func(int, providertest.BraveRequest) providertest.BraveResponse {
		return providertest.BraveResponse{Results: []providertest.BraveResult{{Title: "t SECRET123", URL: "https://x.example/BRAVEKEY-LIT", Description: "s SECRET123 BRAVEKEY-LIT"}}}
	})
	defer b.Close()
	srv := providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		last := req.Messages[len(req.Messages)-1]
		if last.Role == "user" {
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "c1", Name: "web_search", Arguments: `{"query":"zz SECRET123 zz"}`}}}
		}
		return providertest.Response{Text: "finished"}
	})
	e := newEnvServer(t, srv, envOpts{contextLimit: 8000, projectJSON: `{"search":{"baseURL":"` + b.URL + `","apiKey":"BRAVEKEY-LIT"}}`})
	e.submit(t, "search it")
	reqs := b.Requests()
	if len(reqs) != 1 {
		t.Logf("search not performed: %d requests", len(reqs))
		return
	}
	if q := reqs[0].Query.Get("q"); strings.Contains(q, "SECRET123") || !strings.Contains(q, "[redacted]") {
		t.Logf("query reached Brave unredacted: %q", q)
		return
	}
	v := e.Model.View()
	if strings.Contains(v, "SECRET123") || strings.Contains(v, "BRAVEKEY-LIT") || !strings.Contains(v, "finished") {
		t.Logf("view leaks or turn missing:\n%s", v)
		return
	}
	for _, r := range e.Server.Requests() {
		if strings.Contains(string(r.Body), "SECRET123") || strings.Contains(string(r.Body), "BRAVEKEY-LIT") {
			t.Log("secret or Brave key in a provider request")
			return
		}
	}
	t.Fatal("REJECTED: query, results and key redacted end to end")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/e2e/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_fail "negative: a secret reaches Brave, the view or the model end to end" <<'CMD'
out="$(go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZE2EWebSearchEvil$' ./e2e 2>&1)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q -e 'REJECTED:' && exit 1
printf '%s\n' "$out" | grep -q -e '^ok' && exit 0
exit 125
CMD
expect_fail "negative: an e2e test names the real Brave host" <<'CMD'
no_match -rn -e 'api\.search\.brave\.com' e2e
CMD
expect_fail "negative: a security-critical file differs from HEAD (diff)" <<'CMD'
out="$(git diff --name-only HEAD -- internal/provider/transport.go internal/tools/pathguard.go internal/agent/approval.go internal/tools/bash.go internal/config/redact.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: a file outside e2e tests changed (or a protected path)" <<'CMD'
out="$(git status --porcelain --untracked-files=all)" || exit 125
printf '%s\n' "$out" | sed -e 's/^...//' -e 's/^.* -> //' | grep -v -x -e '' -e 'versions/v6/Tasks.md' | grep -v -E -x -e 'e2e/[a-z0-9_]+_test\.go' | no_match -e .
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet ede3886 -- go.mod go.sum
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
