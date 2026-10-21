#!/usr/bin/env bash
# Verification for T102 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "mode label tests" <<'CMD'
go_tests ./internal/tui TestModeLabelAutoColored TestModeLabelManualPlain TestModeLabelCommandUpdates TestModeLabelKeptByNew TestModeLabelNarrow TestModeHintNeverMentionsSlashMode
CMD
expect_ok "all Mode and Status tests" <<'CMD'
go test -count=1 ./internal/tui -run 'Mode|Status'
CMD
expect_ok "mode colours defined in non-test tui code" <<'CMD'
grep -rqi --include='*.go' --exclude='*_test.go' -e '#E8A33D' internal/tui && grep -rq --include='*.go' --exclude='*_test.go' -e '"250"' internal/tui
CMD
expect_fail "negative: the hint never mentions /mode" <<'CMD'
no_match -n -e 'toggle.*/mode' -e '/mode.*toggle)' internal/tui/status.go
CMD
expect_fail "negative: agent constructor default untouched" <<'CMD'
out="$(git diff d3eeba4 -- internal/agent/session.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: no protected path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v2/spec.md versions/v2/plan.md versions/v2/tasks)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: agent, tools, provider, config unchanged since v1 (d3eeba4)" <<'CMD'
git rev-parse -q --verify 'd3eeba4^{commit}' >/dev/null || exit 125
out="$(git diff d3eeba4 -- internal/agent internal/tools internal/provider internal/config && git status --porcelain --untracked-files=all -- internal/agent internal/tools internal/provider internal/config)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
