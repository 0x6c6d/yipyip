#!/usr/bin/env bash
# Verification for T503 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "e2e Question tests incl. negatives" <<'CMD'
go_tests ./e2e TestQuestionSingleChoiceEndsInFollowupRequest TestQuestionMultipleJoinedInRequest TestQuestionCancelledResultInRequest TestQuestionOverlayVisibleAndTurnRunning
CMD
expect_ok "whole e2e package" <<'CMD'
go test -count=1 ./e2e
CMD
expect_fail "negative: no approval prompt ever for a question call in the e2e source or output" <<'CMD'
out="$(go test -count=1 -run 'TestQuestionOverlayVisibleAndTurnRunning' ./e2e 2>&1)" || exit 125
printf '%s' "$out" | no_match -e 'denied by user'
CMD
expect_fail "negative: the question tool is never registered in internal/tools" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e '"question"' internal/tools
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
