#!/usr/bin/env bash
# Verification for T103 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "ExitSave tests incl. negatives" <<'CMD'
go_tests ./internal/tui TestExitSaveWritesFile TestExitSaveEmptyWritesNothing TestDoubleCtrlCSavesAllTabs TestSaveFailureShowsLineStillQuits TestExitKeepsOtherOutput
CMD
expect_ok "cmd/yip tests unchanged and green" <<'CMD'
go test -count=1 ./cmd/yip
CMD
expect_ok "whole tui package" <<'CMD'
go test -count=1 ./internal/tui
CMD
expect_ok "e2e still green (v1 exit codes preserved)" <<'CMD'
go test -count=1 ./e2e
CMD
expect_ok "secret appears in the written session file (stored un-redacted); the test asserts it and the Redactor path is covered separately" <<'CMD'
go test -count=1 ./internal/tui -run 'TestExitSaveWritesFile|TestDoubleCtrlCSavesAllTabs'
CMD
expect_fail "negative: exported/redacted view never contains the secret" <<'CMD'
no_match -rn --include='*_test.go' -e 'View().*SECRET123' internal/tui
CMD
expect_fail "negative: no tui-side path handcraft; save.go routes through Store (no direct os.WriteFile of session JSON in tui)" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'os.WriteFile' -e 'json.Marshal' -e '.yipyip/sessions' internal/tui/save.go
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
