#!/usr/bin/env bash
# Verification for T202 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"
expect_ok "tests" <<'CMD'
go_tests ./internal/agent TestCheckpointRounds TestCheckpointPanicKeepsRounds TestCheckpointNoDanglingCall TestCheckpointNotForSubagent TestCheckpointEvent
CMD
expect_ok "race detector" <<'CMD'
go test -race -count=1 ./internal/agent
CMD
expect_ok "EvCheckpoint exists and no existing event kind line was removed" <<'CMD'
grep -q -e EvCheckpoint internal/agent/events.go && ! git diff HEAD -U0 -- internal/agent/events.go | grep -e '^-[^-]'
CMD
expect_fail "negative: a checkpoint call placed before the tool results of a round (assistant message appended, no tool loop between)" <<'CMD'
no_match -n -z -P 'out = append\(out, provider\.Message\{Role: "assistant"[^\n]*\n\s*s\.checkpoint' internal/agent/loop.go
CMD
expect_ok "existing undo and compact tests pass" <<'CMD'
go test -count=1 ./internal/agent -run 'Undo|Compact'
CMD
expect_ok "security files, go.mod and go.sum unchanged since 1ccbbc7" <<'CMD'
v10_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
