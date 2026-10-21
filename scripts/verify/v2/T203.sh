#!/usr/bin/env bash
# Verification for T203 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "Thinking tests incl. negative idle-tick test" <<'CMD'
go_tests ./internal/tui TestThinkingLine TestThinkingTokens TestThinkingEarlierTurnNotCounted TestThinkingWorkingVerb TestThinkingColors TestThinkingGoneAfterTurn TestThinkingNoTickWhileIdle TestThinkingFormat
CMD
expect_ok "adapted streaming test" <<'CMD'
go_tests ./internal/tui TestSendMessageStreams TestEscCancels TestDoubleCtrlCQuits
CMD
expect_ok "Options.Now injectable" <<'CMD'
grep -Eq -e '^\s+Now\s+func\(\) time\.Time' internal/tui/model.go && test -f internal/tui/turn.go && test -f internal/tui/clock_test.go
CMD
expect_fail "negative: no unconditional ticker (time.NewTicker / tea.Every) in internal/tui" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'time.NewTicker' -e 'tea.Every' -e 'time.Sleep' internal/tui
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
