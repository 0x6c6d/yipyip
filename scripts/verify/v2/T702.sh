#!/usr/bin/env bash
# Verification for T702 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "docs/usage.md covers v2" <<'CMD'
f=docs/usage.md
grep -qi -e 'auto.*default' $f || { echo "auto default missing"; exit 1; }
for k in 'Ctrl+Home' 'Worked for' 'Interrupted after' 'Thinking' 'characters copied' 'Shift' 'Pasted text' 'Subagent' 'shift+tab to toggle' 'in N · out N'; do
  grep -qF -e "$k" $f || { echo "missing: $k"; exit 1; }
done
grep -qi -e 'queue' $f
CMD
expect_ok "CLAUDE.md lists the v2 tui files" <<'CMD'
for f in turn.go subagents.go input.go mouse.go clipboard.go; do grep -qF -e "$f" CLAUDE.md || { echo "missing $f"; exit 1; }; done
CMD
expect_fail "negative: v1 status format gone from docs/usage.md" <<'CMD'
no_match -n -e 'in N / out N' docs/usage.md
CMD
expect_fail "negative: README.md unchanged" <<'CMD'
out="$(git diff d3eeba4 -- README.md)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: CLAUDE.md Delegation section unchanged" <<'CMD'
a="$(git show HEAD:CLAUDE.md | sed -n '/^## Delegation workflow/,$p')" || exit 125
b="$(sed -n '/^## Delegation workflow/,$p' CLAUDE.md)"
[ -n "$a" ] || exit 125
[ "$a" = "$b" ] && exit 1
diff <(printf '%s\n' "$a") <(printf '%s\n' "$b"); exit 0
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
