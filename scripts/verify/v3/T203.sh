#!/usr/bin/env bash
# Verification for T203 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "/sessions tests incl. negatives" <<'CMD'
go_tests ./internal/tui TestSessionsListEmpty TestSessionsListNewestFirstAndColumns TestSessionsListOverflow TestSessionsLoadRestores TestSessionsLoadUnknownId TestSessionsRmConfirmY TestSessionsRmConfirmN TestSessionsRmNeverCurrentSession
CMD
expect_ok "whole tui package" <<'CMD'
go test -count=1 ./internal/tui
CMD
expect_ok "strings and formats from the brief coded" <<'CMD'
grep -rnq 'no saved sessions' internal/tui/sessions.go && grep -rnq '+.*more\|more$' internal/tui/sessions.go
CMD
expect_ok "loading a session routes through Store.Load" <<'CMD'
grep -rn 'session.Store\|\.Store\b\|Store()' internal/tui/sessions.go | grep -q -e 'Store'
CMD
expect_fail "negative: no direct session-dir file access in tui sources" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'os.ReadDir' -e 'os.ReadFile' -e 'os.Remove' internal/tui/sessions.go internal/tui/resume.go
CMD
expect_fail "negative: no secrets printed into the sessions list" <<'CMD'
out="$(go test -count=1 -run 'TestSessionsListNewestFirstAndColumns' ./internal/tui 2>&1)" || exit 125
printf '%s' "$out" | no_match -e 'SECRET123'
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
