#!/usr/bin/env bash
# Verification for T405 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: subagent tools" <<'CMD'
go_tests ./internal/agent TestSpawnToolReturnsImmediately TestSpawnLimitError TestSubagentNoSpawnTool TestWaitAgentReturnsFinal TestCancelAgentTool TestSubagentTokensAdded TestSubagentApprovalLabelled TestEscCancelsSubagents
CMD
expect_ok "phase 4 exit criterion: go test ./internal/agent" <<'CMD'
go test -count=1 ./internal/agent
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T405.go
CMD
expect_ok "probe: spawn -> wait -> final text, tokens added" <<'CMD'
probe_ok flow
CMD
expect_ok "probe: subagent approvals labelled with its id" <<'CMD'
probe_ok label
CMD
expect_fail "negative: subagent requests offer no spawn_agent" <<'CMD'
probe_rejects nospawn
CMD
expect_fail "negative: spawn beyond max_subagents is an error result" <<'CMD'
probe_rejects limit
CMD
expect_fail "negative: Esc during wait_agent cancels the turn and its subagent" <<'CMD'
probe_rejects esc
CMD
finish
