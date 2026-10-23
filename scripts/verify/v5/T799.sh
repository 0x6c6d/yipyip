#!/usr/bin/env bash
# Verification for T799 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "R53: config and routing tests by name" <<'CMD'
go_tests ./internal/config TestJevRouteThreeTiers TestJevRouteTwoKeys TestJevRouteMediumUnknown && go_tests ./internal/agent TestRouteTiers TestRouteMediumMissing
CMD
expect_ok "R53: TestJevRouteMediumUnknown names jev.route.medium" <<'CMD'
grep -q -F -e 'jev.route.medium' internal/config/jev_test.go
CMD
expect_ok "R54, R6 (v7: R64): iteration limit tests by name" <<'CMD'
go_tests ./internal/agent TestSubagentIterationLimit TestMainNoIterationLimit
CMD
expect_ok "R64: main agent unlimited, subagent limit 500 (constants)" <<'CMD'
grep -n 'const MaxIterations = 0' internal/agent/loop.go && grep -n 'const MaxSubagentIterations = 500' internal/agent/loop.go
CMD
expect_ok "R55: /jev route tests by name; README Usage has medium" <<'CMD'
go_tests ./internal/tui TestJevCmdRoute TestJevCmdNoRoute && test "$(sed -n '/^## Usage/,$p' README.md | grep -c '"medium"')" -ge 1
CMD
expect_ok "R41: click-to-cursor tests by name" <<'CMD'
go_tests ./internal/tui TestInputClickMultiLine TestInputClickWrapped TestInputClickScrolled TestInputClickWideRunes TestInputClickOutside
CMD
expect_ok "R49, R51: e2e tests by name (and TestJevE2E)" <<'CMD'
go_tests ./e2e TestJevQuestionGoesToUser TestJevNoRiskScreen TestJevE2E
CMD
expect_ok "R51: approval.go unchanged since the v7 start (4f99ac0)" <<'CMD'
git rev-parse -q --verify '4f99ac0^{commit}' >/dev/null || exit 125
git diff --quiet 4f99ac0 -- internal/agent/approval.go && test -z "$(git status --porcelain -- internal/agent/approval.go)"
CMD
expect_ok "R52: fallback tests by name" <<'CMD'
go_tests ./internal/agent TestRouteFallbackError TestRouteFallbackTimeout TestRouteFallbackLowConfidence
CMD
expect_ok "R56, R48: stats tests by name" <<'CMD'
go_tests ./internal/tui TestJevStatsLines TestJevStatsNotConfigured && go_tests ./internal/agent TestJevStatsCounters
CMD
expect_ok "phase exit criteria 1-5" <<'CMD'
go test -count=1 ./internal/config/ ./internal/agent/ -run 'JevRoute|Route' && go build ./... && go test -count=1 ./internal/agent/ ./e2e/ && go test -count=1 ./internal/agent/ ./internal/tui/ -run 'Jev' && go test -count=1 ./internal/agent/ -run 'IterationLimit' && go test -count=1 ./internal/tui/ -run 'InputClick'
CMD
expect_ok "secret suite repo-wide" <<'CMD'
go test -count=1 ./... -run Secret
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 0c3a7c5 -- go.mod go.sum
CMD
expect_fail "negative: R49 and phase 2 exit: JevChoice or answer/risk code in internal/agent" <<'CMD'
no_match -rn -e 'JevChoice' -e 'SetRiskScreen' -e 'jevRisk' -e 'jevAnswer' -e 'jevQuestionState' -e 'turnHistory' internal/agent
CMD
expect_fail "negative: R51 risk screen in internal" <<'CMD'
no_match -rn -E -e 'SetRiskScreen|jevRisk' internal
CMD
expect_fail "negative: R48 QuestionTokens in internal" <<'CMD'
no_match -rn -e 'QuestionTokens' internal
CMD
expect_fail "negative: protected-path diff against 4f99ac0 is not empty" <<'CMD'
git rev-parse -q --verify '4f99ac0^{commit}' >/dev/null || exit 125
paths="prompts .claude scripts/verify/_lib.sh versions/v5/spec.md versions/v5/plan.md internal/provider/transport.go internal/tools internal/config/redact.go"
out="$(git diff --stat 4f99ac0 -- $paths && git status --porcelain --untracked-files=all -- $paths)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: README sections 1-9 changed (v7: only the two R6 lines may differ from 4f99ac0)" <<'CMD'
! diff <(git show 4f99ac0:README.md | sed '/^## Usage/,$d' | grep -v -e '^- R6\. ' -e '^- \[ \] Agent loop test with a scripted fake provider') <(sed '/^## Usage/,$d' README.md | grep -v -e '^- R6\. ' -e '^- \[ \] Agent loop test with a scripted fake provider')
CMD
expect_fail "negative: main 50 limit text in internal/agent" <<'CMD'
no_match -n -F -e 'iteration limit (50)' internal/agent/*.go
CMD
expect_fail "negative: Delegation workflow section of CLAUDE.md changed" <<'CMD'
! diff <(git show 0c3a7c5:CLAUDE.md | sed -n '/^## Delegation workflow/,$p') <(sed -n '/^## Delegation workflow/,$p' CLAUDE.md)
CMD
expect_fail "negative: default HTTP client in non-test code" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'http\.DefaultClient' -e 'http\.DefaultTransport' -e 'http\.Get(' -e 'http\.Post(' -e 'http\.Head(' cmd internal
CMD
expect_fail "negative: os/exec outside bash.go and clipboard.go" <<'CMD'
grep -rln --include='*.go' --exclude='*_test.go' -e '"os/exec"' cmd internal | grep -v -x -e 'internal/tools/bash.go' -e 'internal/tui/clipboard.go' | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
