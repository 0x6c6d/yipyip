#!/usr/bin/env bash
# Verification for T302 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"
expect_ok "tests" <<'CMD'
go_tests ./internal/tui TestAutosaveAfterTurnEnd TestAutosaveSpacing TestAutosaveNotOnUIGoroutine TestAutosaveErrorOnce TestAutosaveInterruptedMidTurn TestAutosaveEmptyConversationSkipped TestAutosaveExitSingleFile TestAutosavePrunesProtectingTabs
CMD
expect_ok "race detector on autosave tests" <<'CMD'
go test -race -count=1 ./internal/tui -run 'Autosave'
CMD
expect_ok "existing save, exit and tab tests pass" <<'CMD'
go test -count=1 ./internal/tui -run 'Save|Exit|Tab|Resume|Session'
CMD
expect_ok "seams exist" <<'CMD'
grep -q -e 'Autosave ' internal/tui/model.go && grep -q -e 'PruneSessions' internal/tui/model.go
CMD
expect_fail "negative: the store is never written directly from Update" <<'CMD'
no_match -n -e 'store.Save(' internal/tui/model.go
CMD
expect_ok "security files, go.mod and go.sum unchanged since 1ccbbc7" <<'CMD'
v10_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
