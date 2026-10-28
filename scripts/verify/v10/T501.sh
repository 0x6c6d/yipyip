#!/usr/bin/env bash
# Verification for T501 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"
expect_ok "crash tests" <<'CMD'
go_tests ./e2e TestCrashFatalExit2 TestCrashSecretNotLogged TestCrashCapped TestCrashNoPanicNoLog
CMD
expect_ok "autosave tests" <<'CMD'
go_tests ./e2e TestAutosaveAfterTurn TestAutosaveSpacing TestAutosaveErrorOnce TestAutosaveInterruptedResume TestAutosaveExitSingleFile
CMD
expect_ok "retention tests" <<'CMD'
go_tests ./e2e TestRetentionStartup TestRetentionOpenTabProtected
CMD
expect_ok "race detector on autosave e2e" <<'CMD'
go test -race -count=1 ./e2e -run 'Autosave'
CMD
expect_ok "only e2e files changed" <<'CMD'
test -z "$(git status --porcelain | grep -v -e ' e2e/' -e ' versions/' -e ' scripts/verify/')"
CMD
expect_ok "crash_test.go really asserts on the secret" <<'CMD'
grep -q -e SECRET123 e2e/crash_test.go
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
