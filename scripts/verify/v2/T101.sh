#!/usr/bin/env bash
# Verification for T101 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "new two-row status tests" <<'CMD'
go_tests ./internal/tui TestStatusTwoRows TestStatusRow2Color TestStatusNarrowTwoRows TestStatusNoCostTwoRows
CMD
expect_ok "all Status tests (adapted v1 tests included)" <<'CMD'
go test -count=1 ./internal/tui -run 'Status|Skeleton'
CMD
expect_ok "StatusRows API exists" <<'CMD'
grep -q -e 'func (m Model) StatusRows() (row1, row2 string)' internal/tui/status.go
CMD
expect_fail "negative: old slash token format gone from non-test code" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'in %d / out' -e '/ out %d /' internal cmd
CMD
expect_fail "negative: no protected path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v2/spec.md versions/v2/plan.md versions/v2/tasks)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: agent, tools, provider, config unchanged since v1 (d3eeba4)" <<'CMD'
git rev-parse -q --verify 'd3eeba4^{commit}' >/dev/null || exit 125
out="$(git diff d3eeba4 -- internal/agent internal/tools internal/provider internal/config && git status --porcelain --untracked-files=all -- internal/agent internal/tools internal/provider internal/config)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
