#!/usr/bin/env bash
# Verification for T103 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "cmd/yip sets auto mode" <<'CMD'
grep -n -e 'SetMode(agent.ModeAuto)' cmd/yip/main.go
CMD
expect_ok "cmd/yip newModel test" <<'CMD'
go_tests ./cmd/yip TestNewModelStartsAuto
CMD
expect_ok "tui auto-default tests incl. negative manual prompt" <<'CMD'
go_tests ./internal/tui TestModeStartAutoLikeCmd TestModeAutoBashUnprompted TestModeManualPromptsAgain
CMD
expect_ok "all Mode and Approval tests" <<'CMD'
go test -count=1 ./internal/tui -run 'Mode|Approval'
CMD
expect_fail "negative: agent constructor still defaults to manual (no ModeAuto default added in internal/agent)" <<'CMD'
out="$(git diff d3eeba4 -- internal/agent)" || exit 125
printf '%s' "$out" | no_match -e 'ModeAuto'
CMD
expect_fail "negative: newModel is not bypassed (no second tui.New in cmd/yip)" <<'CMD'
n="$(grep -c -e 'tui.New(' cmd/yip/main.go)"
[ "$n" -gt 1 ] && { echo "tui.New called $n times"; exit 0; }
exit 1
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
