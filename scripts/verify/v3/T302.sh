#!/usr/bin/env bash
# Verification for T302 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "retry tests incl. the negative ones (400, post-first-byte, foreign host)" <<'CMD'
go_tests ./internal/provider TestRetryNineFailuresThenSuccess TestRetry429And5xxAreRetryable TestNoRetryOn400 TestNoRetryAfterFirstByte TestRetryCancellation TestRetryNeverOtherHost TestRetryEventFields
CMD
expect_ok "whole provider package" <<'CMD'
go test -count=1 ./internal/provider
CMD
expect_ok "e2e package still green" <<'CMD'
go test -count=1 ./e2e
CMD
expect_ok "wait sequence and attempt cap encoded in client.go" <<'CMD'
f=internal/provider/client.go
grep -qF -e '1 \* time.Second' $f && grep -qF -e '2 \* time.Second' $f && grep -qF -e '4 \* time.Second' $f && grep -qF -e '8 \* time.Second' $f && grep -qF -e '16 \* time.Second' $f && grep -qF -e '32 \* time.Second' $f && grep -qF -e '60 \* time.Second' $f
CMD
expect_ok "waits go through an injectable wait fn (no hardcoded sleep loop)" <<'CMD'
f=internal/provider/client.go
grep -rn --include='*.go' -e 'retryWait\|wait =\|Wait(' $f | grep -q -e 'retryWait\|Wait('
CMD
expect_ok "first-byte boundary handled (pre-first-byte only retry)" <<'CMD'
grep -rnqi -e 'firstbyte' -e 'first byte' -e 'first sse byte' internal/provider/client.go
CMD
expect_fail "negative: transport.go untouched since 9519824" <<'CMD'
git rev-parse -q --verify '9519824^{commit}' >/dev/null || exit 125
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: tests never sleep (waits are stubbed)" <<'CMD'
grep -rn --include='*_test.go' -e 'time\.Sleep' internal/provider | no_match -e .
CMD
expect_fail "negative: retry code lives in client.go only (no retry in transport.go or sse.go)" <<'CMD'
no_match -rni --include='*.go' -e 'retry' internal/provider/transport.go internal/provider/sse.go
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
