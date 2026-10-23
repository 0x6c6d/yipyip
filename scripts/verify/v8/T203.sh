#!/usr/bin/env bash
# Verification for T203 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_fail "negative: no jev or System One in CLAUDE.md" <<'CMD'
no_match -n -i -e 'jev' -e 'system one' -e 'typesafe' CLAUDE.md
CMD
expect_ok "traffic sentence names providers and the Brave host only" <<'CMD'
grep -q -F -e 'Its network traffic is restricted to the configured provider hosts (plus, for `web_search` only, the Brave host while configured).' CLAUDE.md
CMD
expect_ok "architecture lines for provider, agent, tui and e2e remain" <<'CMD'
grep -q -e '^- `internal/provider`: ' CLAUDE.md && grep -q -e '^- `internal/agent`: ' CLAUDE.md &&
grep -q -e '^- `internal/tui`: ' CLAUDE.md && grep -q -e '^- `e2e`: ' CLAUDE.md &&
grep -e '^- `internal/agent`: ' CLAUDE.md | grep -q -e 'question.go' &&
grep -e '^- `internal/provider`: ' CLAUDE.md | grep -q -e 'brave.go'
CMD
expect_ok "Delegation workflow section equals 5a884db" <<'CMD'
diff <(git show 5a884db:CLAUDE.md | sed -n '/^## Delegation workflow/,$p') <(sed -n '/^## Delegation workflow/,$p' CLAUDE.md)
CMD
expect_ok "scope: only CLAUDE.md changed" <<'CMD'
changed="$( { git diff --name-only HEAD; git ls-files -o --exclude-standard; } | sort -u | grep -v -e '^versions/v8/Tasks.md$')"
printf '%s\n' "$changed"
test -z "$(printf '%s\n' "$changed" | grep -v -e '^$' -e '^CLAUDE.md$')"
CMD
finish
