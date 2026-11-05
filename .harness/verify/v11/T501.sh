#!/usr/bin/env bash
# Verification for T501 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "paste shown tests" <<'CMD'
go_tests ./internal/tui/ TestPasteShown TestPasteShownQueued
CMD
expect_ok "whole tui package" <<'CMD'
go test -count=1 ./internal/tui/
CMD
expect_fail "negative: no change outside internal/tui" <<'CMD'
git status --porcelain --untracked-files=all | grep -v -e ' internal/tui/' -e ' \.harness/versions/v11/Tasks.md$'
CMD
finish
