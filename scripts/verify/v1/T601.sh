#!/usr/bin/env bash
# Verification for T601 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: undo journal" <<'CMD'
go_tests ./internal/agent TestUndoRestoresModified TestUndoDeletesCreated TestUndoRemovesTurn TestUndoTwice TestUndoListsBash TestUndoSubagentWrites TestUndoNothing TestUndoBusy TestUndoRestoresMode
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T601.go
CMD
expect_ok "probe: two turns undone back to the original content and mode" <<'CMD'
probe_ok restore
CMD
expect_fail "negative: Undo on a fresh session returns ErrNothingToUndo" <<'CMD'
probe_rejects nothing
CMD
expect_fail "negative: Undo during a running turn returns ErrBusy" <<'CMD'
probe_rejects busy
CMD
finish
