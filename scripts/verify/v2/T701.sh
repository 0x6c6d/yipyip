#!/usr/bin/env bash
# Verification for T701 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "new help tests" <<'CMD'
go_tests ./internal/tui TestHelpTextListsNewKeys && go_tests ./cmd/yip TestHelpFlagNewKeys
CMD
expect_ok "existing help tests" <<'CMD'
go_tests ./internal/tui TestHelpTextListsCommands TestHelpTextNoCost TestHelpCommand && go_tests ./cmd/yip TestHelpFlag
CMD
expect_ok "yip --help lists the new keys" <<'CMD'
out="$(go run ./cmd/yip --help)" || exit 1
for k in 'Ctrl+Home' 'Ctrl+End' 'Up' 'Tab'; do printf '%s\n' "$out" | grep -qF -e "$k" || { echo "missing $k"; exit 1; }; done
printf '%s\n' "$out" | grep -qi -e 'queue'
CMD
expect_fail "negative: help text has no cost, price or dollar sign" <<'CMD'
out="$(go run ./cmd/yip --help)" || exit 125
printf '%s\n' "$out" | no_match -i -e 'cost' -e 'price' -e '\$'
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
