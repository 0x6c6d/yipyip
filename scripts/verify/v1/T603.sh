#!/usr/bin/env bash
# Verification for T603 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: compaction" <<'CMD'
go_tests ./internal/agent TestCompactKeepsLastTwoTurns TestCompactTokensNotReduced TestCompactNothing TestAutoCompactAt80 TestAutoCompactBelowThreshold TestAutoCompactDisabled
CMD
expect_ok "exported API" <<'CMD'
has_symbols ./internal/agent Session.Compact ErrNothingToCompact
CMD
expect_ok "undo tests still pass" <<'CMD'
go_tests ./internal/agent TestUndoTwice TestUndoRemovesTurn
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
