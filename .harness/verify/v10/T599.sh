#!/usr/bin/env bash
# Verification for T599 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"
expect_ok "crashlog tests" <<'CMD'
go test -count=1 ./internal/crashlog -run 'Crashlog'
CMD
expect_ok "e2e crash tests" <<'CMD'
go test -count=1 ./e2e -run 'TestCrashFatalExit2|TestCrashSecretNotLogged|TestCrashCapped'
CMD
expect_ok "recovered panics are logged" <<'CMD'
go test -count=1 ./internal/tools ./internal/agent ./internal/provider -run 'PanicRecovered'
CMD
expect_ok "checkpoints" <<'CMD'
go test -count=1 ./internal/agent -run 'TestCheckpointRounds|TestCheckpointPanicKeepsRounds|TestCheckpointNoDanglingCall'
CMD
expect_ok "autosave e2e" <<'CMD'
go test -count=1 ./e2e -run 'TestAutosaveAfterTurn|TestAutosaveSpacing|TestAutosaveErrorOnce|TestAutosaveInterruptedResume|TestAutosaveExitSingleFile'
CMD
expect_ok "containment" <<'CMD'
go test -count=1 ./internal/agent ./internal/tui -run 'TestSubagentPanicParentFinishes|TestRenderGuardBlock'
CMD
expect_ok "retention" <<'CMD'
go test -count=1 ./internal/session ./e2e -run 'TestPrune|TestRetentionStartup|TestRetentionOpenTabProtected'
CMD
expect_ok "crashlog imports only the standard library" <<'CMD'
test -z "$(go list -deps ./internal/crashlog | grep -e '^yipyip/' | grep -v -x -e yipyip/internal/crashlog)"
CMD
expect_ok "security files, go.mod and go.sum unchanged since 1ccbbc7" <<'CMD'
v10_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
