#!/usr/bin/env bash
# Verification for T403 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: agent loop" <<'CMD'
go_tests ./internal/agent TestLoopToolCallThenAnswer TestIterationLimit TestCancelStopsTurn TestSubmitNoModel TestSubmitBusy TestTokenAccounting TestTokenEstimate TestProviderErrorShown TestEffortSent TestManualDenyRunsNothing
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T403.go
CMD
expect_ok "probe: tool call -> result -> answer, tokens, history" <<'CMD'
probe_ok answer
CMD
expect_ok "probe: HTTP error shown, session keeps working" <<'CMD'
probe_ok error
CMD
expect_ok "probe: reasoning_effort only for reasoning models; token estimate" <<'CMD'
probe_ok effort
CMD
expect_fail "negative: Submit without a model fails" <<'CMD'
probe_rejects nomodel
CMD
expect_fail "negative: Submit while busy fails" <<'CMD'
probe_rejects busy
CMD
expect_fail "negative: Cancel ends a turn running sleep 30 within 2 s" <<'CMD'
probe_rejects cancel
CMD
expect_fail "negative: endless tool calls stop after exactly 50 requests" <<'CMD'
probe_rejects limit
CMD
finish
