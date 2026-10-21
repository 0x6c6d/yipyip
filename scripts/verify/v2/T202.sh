#!/usr/bin/env bash
# Verification for T202 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "new message-block render tests" <<'CMD'
go_tests ./internal/tui TestRenderUserBlock TestRenderAssistantBlock TestRenderErrorBlock TestRenderBlankLineBetweenBlocks TestRenderNoBordersOutsideInput
CMD
expect_ok "adapted fence and streaming tests" <<'CMD'
go_tests ./internal/tui TestFenceHighlightedInView TestSendMessageStreams
CMD
expect_ok "all Render tests" <<'CMD'
go test -count=1 ./internal/tui -run Render
CMD
expect_ok "renderBlock exists" <<'CMD'
grep -q -e 'func (m \*Model) renderBlock(' internal/tui/blocks.go
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
