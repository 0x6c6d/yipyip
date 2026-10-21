#!/usr/bin/env bash
# Verification for T301 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "fake provider scripted failure tests" <<'CMD'
go_tests ./internal/provider TestFakeConnectionResetScripted TestFake429Scripted TestFake503Scripted
CMD
expect_ok "per-attempt recording test" <<'CMD'
go_tests ./internal/provider TestFakeRecordsEachAttempt
CMD
expect_ok "whole provider package (no regression from the fake changes)" <<'CMD'
go test -count=1 ./internal/provider
CMD
expect_fail "negative: the new scripting knobs are confined to the fake provider (test-only names absent from client.go and transport.go)" <<'CMD'
no_match -rni --include='*.go' --exclude='*_test.go' -e 'scriptre' -e 'scriptfail' -e 'failfirst' -e 'resets(' internal/provider/client.go internal/provider/transport.go
CMD
expect_fail "negative: a real provider host name appears only in _test.go files" <<'CMD'
grep -rn --include='*.go' -e 'openrouter\.ai' . | grep -v '^.*_test\.go:' | no_match -e .
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
