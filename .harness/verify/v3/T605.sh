#!/usr/bin/env bash
# Verification for T605 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "agent retry event tests" <<'CMD'
go_tests ./internal/agent TestRetryEventForwarded TestRetryEventNotInHistory
CMD
expect_ok "tui retry status tests incl. negatives" <<'CMD'
go_tests ./internal/tui TestRetryStatusShown TestRetryStatusClearedOnDelta TestRetryStatusPerTab
CMD
expect_ok "agent, provider and tui packages green" <<'CMD'
go test -count=1 ./internal/agent ./internal/provider ./internal/tui
CMD
expect_ok "EvRetry event kind exists" <<'CMD'
grep -q 'EvRetry' internal/agent/events.go
CMD
expect_ok "display string coded in the tui" <<'CMD'
grep -rq 'retrying (' internal/agent internal/tui --include='*.go' --exclude='*_test.go'
CMD
expect_fail "negative: the provider reason text never goes into the display string" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'Retry\.Reason' internal/agent internal/tui
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
