#!/usr/bin/env bash
# Verification for T501 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "question gate tests incl. negatives" <<'CMD'
go_tests ./internal/agent TestQuestionToolOfferedAndUngated TestQuestionSingleChoiceResult TestQuestionMultipleResultJoined TestQuestionEscCancelled TestQuestionNeverFinishesTurnWhileWaiting TestQuestionOptionsCappedAtSix TestQuestionRespondOnceOnly
CMD
expect_ok "whole internal/agent package" <<'CMD'
go test -count=1 ./internal/agent
CMD
expect_ok "whole internal/tools package unchanged and green" <<'CMD'
go test -count=1 ./internal/tools
CMD
expect_ok "cancelled constant exported" <<'CMD'
has_symbols ./internal/agent QuestionRequest Cancelled
CMD
expect_fail "negative: internal/tools is byte-identical to the v2 baseline" <<'CMD'
out="$(git diff --stat 9519824 -- internal/tools && git status --porcelain --untracked-files=all -- internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: question is not a gated tool" <<'CMD'
grep -rn 'question' internal/agent/approval.go | no_match -e 'true'
CMD
expect_fail "negative: the tool never shells out or dials" <<'CMD'
no_match -rn --include='*.go' -e 'exec.Command' -e 'http.Get' -e 'http.Post' internal/agent/question.go
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
