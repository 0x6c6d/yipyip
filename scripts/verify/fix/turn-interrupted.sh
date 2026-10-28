#!/usr/bin/env bash
# Verification for the runTurn interrupted fix (supervisor-owned). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "finished turn is not interrupted" <<'CMD'
go test -count=1 ./internal/agent -run 'TestTurnNotInterruptedWhenFinished' -v | grep -q -- '--- PASS: TestTurnNotInterruptedWhenFinished'
CMD
expect_ok "cancelled turn is interrupted" <<'CMD'
go test -count=1 ./internal/agent -run 'TestTurnInterruptedWhenCancelled' -v | grep -q -- '--- PASS: TestTurnInterruptedWhenCancelled'
CMD
expect_ok "only loop.go and agent tests changed" <<'CMD'
test -z "$(git status --porcelain | grep -vE ' (internal/agent/[a-z_]+\.go|scripts/verify/fix/.*)$')"
CMD
expect_ok "race detector" <<'CMD'
go test -race -count=1 ./internal/agent
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
