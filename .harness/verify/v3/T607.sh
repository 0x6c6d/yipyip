#!/usr/bin/env bash
# Verification for T607 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "e2e Retry tests incl. negatives" <<'CMD'
go_tests ./e2e TestRetryTwoFailuresThenStream TestRetryStatusVisibleWhileWaiting TestRetryNo400RetryEndToEnd TestRetryGivesUpAfterTenAttempts
CMD
expect_ok "whole e2e package" <<'CMD'
go test -count=1 ./e2e
CMD
expect_ok "the acceptance filter finds the tests (go test ./e2e -run Retry runs at least one)" <<'CMD'
go test -count=1 -v ./e2e -run Retry 2>&1 | grep -q -e '^--- PASS: TestRetry'
CMD
expect_ok "e2e retry tests never sleep" <<'CMD'
no_sleep() { ! grep -n 'time\.Sleep' e2e/retry_test.go; }
no_sleep
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
