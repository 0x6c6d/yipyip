#!/usr/bin/env bash
# Verification for T204 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "Finished tests incl. negative slash test" <<'CMD'
go_tests ./internal/tui TestFinishedWorkedFor TestFinishedUnderOneSecond TestFinishedInterrupted TestFinishedOnePerTurn TestFinishedNotForSlash TestFinishedColors TestFinishedClearedByNew
CMD
expect_ok "all Thinking and Finished tests" <<'CMD'
go test -count=1 ./internal/tui -run 'Thinking|Finished'
CMD
expect_ok "marker texts present" <<'CMD'
grep -rq --include='*.go' --exclude='*_test.go' -e 'Worked for' internal/tui && grep -rq --include='*.go' --exclude='*_test.go' -e 'Interrupted after' internal/tui
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
