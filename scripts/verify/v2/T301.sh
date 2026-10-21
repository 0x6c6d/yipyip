#!/usr/bin/env bash
# Verification for T301 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "subagent block tests incl. negative no-separate-blocks test" <<'CMD'
go_tests ./internal/tui TestSubagentBlockSingle TestSubagentBlockRunning TestSubagentBlockEnded TestSubagentNoSeparateBlocks TestSubagentRealSpawn
CMD
expect_ok "v1 /agents tests unchanged" <<'CMD'
go_tests ./internal/tui TestAgentsList TestAgentsTranscript TestAgentsKill TestAgentsUnknown TestAgentsNone
CMD
expect_ok "subState fields as specified" <<'CMD'
grep -q -e 'type subState struct' internal/tui/subagents.go && grep -q -e 'subInfo' internal/tui/agents.go
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
