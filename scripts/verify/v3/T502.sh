#!/usr/bin/env bash
# Verification for T502 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "question overlay tests incl. negatives" <<'CMD'
go_tests ./internal/tui TestQuestionOverlaySingleChoiceRadio TestQuestionOverlayMultipleCheckbox TestQuestionEnterReturnsChosenLabel TestQuestionEnterMultipleJoinsComma TestQuestionFreeTextAnswer TestQuestionEscCancelled TestQuestionNothingSelectedEnterStays TestQuestionRedactsSecretInPrompt
CMD
expect_ok "whole tui package" <<'CMD'
go test -count=1 ./internal/tui
CMD
expect_ok "radio/checkbox glyphs and free-text row coded" <<'CMD'
grep -rnq '○\|●' internal/tui/question.go && grep -rnq 'type your own answer' internal/tui/question.go
CMD
expect_ok "Esc maps to the Cancelled constant (not a local string)" <<'CMD'
grep -rn 'agent.Cancelled' internal/tui/question.go | grep -q -e 'agent.Cancelled'
CMD
expect_fail "negative: overlay text passes the redactor" <<'CMD'
grep -rnq 'm.redact' internal/tui/question.go
CMD
expect_fail "negative: an empty result is never sent (Enter with nothing selected stays open — the negative test; also no \"+\"\"\")" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'Respond("")' internal/tui
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
