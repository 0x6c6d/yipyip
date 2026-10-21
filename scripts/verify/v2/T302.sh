#!/usr/bin/env bash
# Verification for T302 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "panel format tests" <<'CMD'
go_tests ./internal/tui TestSubagentPanelFormat TestSubagentPanelOnlyRunning TestSubagentPanel
CMD
expect_ok "all Subagent and Agents tests" <<'CMD'
go test -count=1 ./internal/tui -run 'Subagent|Agents'
CMD
expect_fail "negative: v1 panel format gone" <<'CMD'
no_match -n -e 'running %s %d tokens' internal/tui/agents.go
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
