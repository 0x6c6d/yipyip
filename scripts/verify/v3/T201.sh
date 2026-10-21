#!/usr/bin/env bash
# Verification for T201 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "cmd/yip resume tests incl. negatives" <<'CMD'
go_tests ./cmd/yip TestResumeFlagParsesId TestResumeUnknownIdExit1 TestResumeWithIdLoadsData TestResumeNoIdNonTTYStartsFresh TestResumeNoIdEmptyDirStartsFresh TestResumeHelpMentionsFlag
CMD
expect_ok "whole cmd/yip package (v1/v2 behaviour unchanged)" <<'CMD'
go test -count=1 ./cmd/yip
CMD
expect_ok "flag and alias wired in main.go" <<'CMD'
grep -q -- '--resume' cmd/yip/main.go && grep -q -- '"-c"' cmd/yip/main.go
CMD
expect_ok "hand-over fields exist on tui Options" <<'CMD'
has_symbols ./internal/tui Options
CMD
expect_fail "negative: unknown-id message never leaks the sessions path" <<'CMD'
no_match -rn --include='*_test.go' -e 'unknown session .*\.yipyip' cmd/yip
CMD
expect_fail "negative: no picker or restore rendering implemented in this task (T202's job)" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'resumePicker\|overlayPicker(.*)resume\|resumeKey' cmd/yip
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
