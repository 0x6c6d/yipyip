#!/usr/bin/env bash
# Verification for T404 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: subagent manager" <<'CMD'
go_tests ./internal/agent TestSpawnReturnsImmediately TestSpawnLimit TestWaitReturnsFinal TestCancelSubagent TestParentCancel TestWaitUnknown TestSubagentTokensAndTranscript TestSubagentFailed
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T404.go
CMD
expect_ok "probe: spawn, wait, transcript, tokens, failed" <<'CMD'
probe_ok spawn
CMD
expect_ok "probe: cancel, parent cancel, CancelAll" <<'CMD'
probe_ok cancel
CMD
expect_fail "negative: 5th concurrent spawn with limit 4 refused" <<'CMD'
probe_rejects limit
CMD
expect_fail "negative: Wait and Cancel on an unknown id fail immediately" <<'CMD'
probe_rejects unknown
CMD
finish
