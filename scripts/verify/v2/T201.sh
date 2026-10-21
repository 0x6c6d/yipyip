#!/usr/bin/env bash
# Verification for T201 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "new tool-block render tests" <<'CMD'
go_tests ./internal/tui TestRenderToolCallLine TestRenderToolSummaries TestRenderToolCollapsed TestRenderToolDotColors TestRenderToolNoBorder
CMD
expect_ok "adapted v1 tool-block tests" <<'CMD'
go_tests ./internal/tui TestToolBlockRendered TestToolResultCollapsed TestToolResultExpand TestToolResultsMatchByOrder
CMD
expect_fail "negative: bordered tool block and old collapse text removed" <<'CMD'
no_match -n -e 'toolBorderStyle' -e 'more lines, ctrl+o' -e 'RoundedBorder' internal/tui/blocks.go
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
