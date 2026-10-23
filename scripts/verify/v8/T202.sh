#!/usr/bin/env bash
# Verification for T202 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_fail "negative: no jev, System One, typesafe or route: in docs/usage.md" <<'CMD'
no_match -n -i -e 'jev' -e 'system one' -e 'typesafe' -e 'route:' -e 'turn routing' docs/usage.md
CMD
expect_ok "all other section headings remain" <<'CMD'
for h in '## Install' '## Configuration' '## Modes' '## Slash commands' '## Input' '## Keys' '## Conversation' '## Subagents' '## Sessions' '## Tabs' '## Questions' '## Skills' '## Retry' '## Settings defaults' '## Web search' '## Security'; do
	grep -q -x -F -e "$h" docs/usage.md || { echo "missing heading: $h"; exit 1; }
done
CMD
expect_ok "slash table keeps /skills, /help and /exit rows" <<'CMD'
grep -q -e '^| `/skills`' docs/usage.md && grep -q -e '^| `/help`' docs/usage.md && grep -q -e '^| `/exit`' docs/usage.md
CMD
expect_ok "Web search network bullet: 'Provider calls never go there.'" <<'CMD'
tr '\n' ' ' < docs/usage.md | tr -s ' ' | grep -q -F -e 'Provider calls never go there.'
CMD
expect_ok "Security: provider hosts and the Brave host, no other web requests" <<'CMD'
flat="$(tr '\n' ' ' < docs/usage.md | tr -s ' ')"
printf '%s\n' "$flat" | grep -q -F -e 'Network traffic goes only to the provider hosts configured in your config files' &&
printf '%s\n' "$flat" | grep -q -F -e 'yip makes no other web requests.' &&
printf '%s\n' "$flat" | grep -q -F -e 'Skill text is redacted like all other text'
CMD
expect_ok "code fences are balanced" <<'CMD'
test $(( $(grep -c -e '^```' docs/usage.md) % 2 )) -eq 0
CMD
expect_ok "scope: only docs/usage.md changed" <<'CMD'
changed="$( { git diff --name-only HEAD; git ls-files -o --exclude-standard; } | sort -u | grep -v -e '^versions/v8/Tasks.md$')"
printf '%s\n' "$changed"
test -z "$(printf '%s\n' "$changed" | grep -v -e '^$' -e '^docs/usage.md$')"
CMD
finish
