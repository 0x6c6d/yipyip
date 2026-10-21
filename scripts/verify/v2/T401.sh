#!/usr/bin/env bash
# Verification for T401 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "Suggest tests incl. negative hint and no-ghost tests" <<'CMD'
go_tests ./internal/tui TestSuggestCommand TestSuggestEffortHint TestSuggestEffortPrefix TestSuggestModelIDs TestSuggestTabAccepts TestSuggestHintNotAccepted TestSuggestNoGhost TestSuggestColor TestSuggestNeverSent
CMD
expect_ok "v1 completion still works" <<'CMD'
go_tests ./internal/tui TestCompletion
CMD
expect_ok "suggestion function exists" <<'CMD'
grep -q -e 'func (m Model) suggestion() (ghost string, accept string)' internal/tui/input.go
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
