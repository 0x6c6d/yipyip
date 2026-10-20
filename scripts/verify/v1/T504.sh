#!/usr/bin/env bash
# Verification for T504 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: /model" <<'CMD'
go_tests ./internal/tui TestModelPicker TestModelPickerEsc TestModelDirect TestModelUnknown
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
