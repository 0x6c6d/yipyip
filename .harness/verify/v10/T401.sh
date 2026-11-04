#!/usr/bin/env bash
# Verification for T401 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"
expect_ok "tests" <<'CMD'
go_tests ./internal/agent TestSubagentPanicParentFinishes TestSubagentPanicOtherSubagentUnaffected TestSubagentPanicNextSubmitWorks
CMD
expect_ok "race detector" <<'CMD'
go test -race -count=1 ./internal/agent
CMD
expect_ok "existing subagent tests pass" <<'CMD'
go test -count=1 ./internal/agent -run 'Subagent|Spawn'
CMD
expect_ok "security files, go.mod and go.sum unchanged since 1ccbbc7" <<'CMD'
v10_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
