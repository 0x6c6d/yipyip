#!/usr/bin/env bash
# Verification for T507 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: subagent panel and /agents" <<'CMD'
go_tests ./internal/tui TestAgentsList TestAgentsTranscript TestAgentsKill TestAgentsUnknown TestAgentsNone TestSubagentPanel
CMD
expect_ok "phase 5 exit criterion: go test ./internal/tui" <<'CMD'
go test -count=1 ./internal/tui
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
