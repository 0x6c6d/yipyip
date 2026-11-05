#!/usr/bin/env bash
# Verification for T603 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "CLAUDE.md mentions the v11 pieces" <<'CMD'
for s in 'yip -p' 'headless.go' 'headless_events.go' 'headless_args.go' 'SystemAppend' 'MaxModelCalls' 'ErrModelCallLimit' 'FromAgent' 'ApplyTo' 'Headless'; do
  grep -q -F -e "$s" CLAUDE.md || { echo "missing: $s"; exit 1; }
done
CMD
expect_ok "Delegation workflow section unchanged" <<'CMD'
a="$(git show HEAD:CLAUDE.md | sed -n '/^## Delegation workflow/,$p')"
b="$(sed -n '/^## Delegation workflow/,$p' CLAUDE.md)"
[ -n "$a" ] && [ "$a" = "$b" ]
CMD
expect_fail "negative: no change outside CLAUDE.md" <<'CMD'
git status --porcelain --untracked-files=all | grep -v -e ' CLAUDE.md$' -e ' \.harness/versions/v11/Tasks.md$'
CMD
finish
