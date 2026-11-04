#!/usr/bin/env bash
# Verification for T602 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "e2e tests: /undo" <<'CMD'
go_tests ./e2e TestUndoRestoresModified TestUndoDeletesCreated TestUndoRemovesTurn TestUndoTwice TestUndoListsBash
CMD
expect_ok "DoD D9: go test ./e2e -run Undo" <<'CMD'
go test -count=1 ./e2e -run Undo
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
