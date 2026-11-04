#!/usr/bin/env bash
# Verification for T201 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "R6: requirement line names no iteration limit (R64)" <<'CMD'
grep -n -F -e '- R6. Agent loop: after a user message the main agent calls the model, runs requested tools, feeds results back, and repeats until a reply without tool calls (no iteration limit, R64). `Esc` cancels the running turn' README.md
CMD
expect_ok "R6: acceptance line uses the 120-round turn" <<'CMD'
grep -n -F -e '- [ ] Agent loop test with a scripted fake provider: tool call → result → final answer; a scripted 120-round turn runs to its final answer; cancelling the context stops the turn within 2 s;' README.md
CMD
expect_ok "README differs from 4f99ac0 in exactly the two R6 lines" <<'CMD'
d="$(git diff -U0 4f99ac0 -- README.md | grep -e '^[-+]' | grep -v -e '^--- ' -e '^+++ ')"
printf '%s\n' "$d"
test "$(printf '%s\n' "$d" | grep -c -e '^-')" -eq 2 &&
test "$(printf '%s\n' "$d" | grep -c -e '^+')" -eq 2 &&
printf '%s\n' "$d" | grep -q -F -e '+- R6. Agent loop:' &&
printf '%s\n' "$d" | grep -q -F -e '+- [ ] Agent loop test with a scripted fake provider:'
CMD
expect_fail "negative: old R6 phrases are gone from README.md" <<'CMD'
no_match -n -e 'iteration limit stops at 50' -e 'or 50 iterations' README.md
CMD
finish
