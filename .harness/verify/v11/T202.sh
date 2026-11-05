#!/usr/bin/env bash
# Verification for T202 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "model call limit tests" <<'CMD'
go_tests ./internal/agent/ TestModelCallLimitMain TestModelCallLimitSubagents TestModelCallLimitWithin TestModelCallsUnlimited
CMD
expect_ok "race detector on the limit tests" <<'CMD'
go test -count=1 -race -run 'ModelCall' ./internal/agent/
CMD
expect_ok "exported symbols" <<'CMD'
has_symbols ./internal/agent ErrModelCallLimit Session.ModelCalls && go doc ./internal/agent Options | grep -q -e 'MaxModelCalls'
CMD
expect_ok "whole agent package" <<'CMD'
go test -count=1 ./internal/agent/
CMD
expect_ok "build supervisor probe" <<'CMD'
build_probe .harness/verify/v11/probe_T202.go
CMD
expect_ok "probe: a turn within the budget ends normally" <<'CMD'
build_probe .harness/verify/v11/probe_T202.go && probe_ok within
CMD
expect_fail "negative: probe: the third request is refused with a limit of 2" <<'CMD'
build_probe .harness/verify/v11/probe_T202.go && probe_rejects limit
CMD
finish
