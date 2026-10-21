#!/usr/bin/env bash
# Verification for T204 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "e2e Session tests incl. negatives" <<'CMD'
go_tests ./e2e TestSessionRoundTripRequests TestSessionFileLayout TestSessionNoSecretLeak TestSessionPerDirectory
CMD
expect_ok "whole e2e package" <<'CMD'
go test -count=1 ./e2e
CMD
expect_ok "e2e file exists with the request-diff assertion" <<'CMD'
grep -rnq 'messages' e2e/session_test.go
CMD
expect_fail "negative: no api key or Authorization header material in any written session file (regex over the test's temp-dir placeholder is covered by the unit test; here guard the source)" <<'CMD'
no_match -rni --include='*.go' --exclude='*_test.go' -e 'authorization' -e 'bearer ' e2e internal/session
CMD
expect_fail "negative: only _test.go files name a real provider host" <<'CMD'
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
