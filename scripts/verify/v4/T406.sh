#!/usr/bin/env bash
# Verification for T406 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named tui jev tests" <<'CMD'
go_tests ./internal/tui TestJevToggle TestJevUnconfigured TestJevStats TestJevHelp
CMD
expect_ok "existing help and status tests still pass" <<'CMD'
go test -count=1 -run 'Help|Status' ./internal/tui
CMD
expect_ok "stats lines and messages present in the tui code" <<'CMD'
for s in 'jev is not configured' 'jev answered: ' 'jev fallbacks: ' 'jev input tokens: ' 'main tokens question: ' 'main tokens routing: ' 'main tokens other: ' '/jev'; do
	grep -rq --include='*.go' --exclude='*_test.go' -F -e "$s" internal/tui || { echo "missing: $s"; exit 1; }
done
CMD
expect_fail "negative: tui never talks to jev or the gate directly" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'JevClient' -e 'askJev' -e 'SetRiskScreen' -e 'NewJevClient' internal/tui
CMD
expect_fail "negative: jev state is not persisted in settings" <<'CMD'
no_match -n -i -e 'jev' internal/config/settings.go internal/tui/settings.go
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 4fabda9 -- go.mod go.sum
CMD
expect_fail "negative: no protected or security-critical path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v4/spec.md versions/v4/plan.md versions/v4/tasks internal/provider internal/tools internal/agent/approval.go internal/config/redact.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
