#!/usr/bin/env bash
# Verification for T503 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: commands, completion, Shift+Tab" <<'CMD'
go_tests ./internal/tui TestHelpCommand TestNewClearsSession TestExitQuit TestEffortCommand TestModeCommand TestShiftTabTogglesMode TestUnknownCommand TestCompletion
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
