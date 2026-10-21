#!/usr/bin/env bash
# Verification for T407 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "TestJevE2E runs and passes" <<'CMD'
go_tests ./e2e TestJevE2E
CMD
expect_ok "whole e2e package still green" <<'CMD'
go test -count=1 ./e2e/...
CMD
expect_ok "phase 4 exit criterion: all TestJev of agent, tui, e2e with the plan's names" <<'CMD'
go_tests ./internal/agent TestJevQuestion TestJevRoute TestJevRisk TestJevRiskManualUnchanged TestJevRiskSafeUnchanged TestJevFallback && go_tests ./internal/tui TestJevToggle TestJevUnconfigured TestJevStats && go test -count=1 -run 'TestJev' ./internal/agent/ ./internal/tui/ ./e2e/
CMD
expect_ok "e2e helper maps the jev key" <<'CMD'
grep -q -e 'TYPESAFE_API_KEY' e2e/helpers_test.go
CMD
expect_ok "test checks the secret, the approval and the fallback" <<'CMD'
grep -q -e 'SECRET123' e2e/jev_test.go && grep -q -e 'JEVKEY-E2E' e2e/jev_test.go && grep -q -e 'denied by user' -e 'DeniedMessage' e2e/jev_test.go && grep -q -e 'fallbacks' e2e/jev_test.go
CMD
expect_fail "negative: e2e never names a real host" <<'CMD'
no_match -rn --include='*.go' -e 'api.typesafe.ai' -e 'openrouter.ai' -e 'https://' e2e/jev_test.go
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
