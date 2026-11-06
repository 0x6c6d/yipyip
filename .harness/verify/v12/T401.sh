#!/usr/bin/env bash
# Verification for T401 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "exact sentence in prompt.go" <<'CMD'
grep -qF 'Processes still running when yip exits (including background jobs and servers) are terminated, so do not expect them to survive the session.' internal/agent/prompt.go
CMD
expect_ok "TestPromptExitCleanup passes" <<'CMD'
go_tests ./internal/agent TestPromptExitCleanup
CMD
expect_ok "all Prompt tests pass" <<'CMD'
go test -count=1 -run Prompt ./internal/agent
CMD
expect_fail "negative: sentence appears only once in prompt.go" <<'CMD'
test "$(grep -cF 'are terminated, so do not expect them to survive the session.' internal/agent/prompt.go)" -gt 1
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
