#!/usr/bin/env bash
# Verification for T602 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "traffic sentence adds the host of an approved web_fetch call" <<'CMD'
grep -q -F -e 'Its network traffic is restricted to the configured provider hosts (plus, for `web_search` only, the Brave host while configured, and for an approved `web_fetch` call, the host of its URL).' CLAUDE.md
CMD
expect_ok "R72: internal/provider line names NewFetchHTTPClient and web_fetch" <<'CMD'
grep -e '^- `internal/provider`: ' CLAUDE.md | grep -q -e 'NewFetchHTTPClient' &&
grep -e '^- `internal/provider`: ' CLAUDE.md | grep -q -e 'web_fetch'
CMD
expect_ok "R72: internal/tools line names webfetch.go, htmltext.go and web_fetch" <<'CMD'
l="$(grep -e '^- `internal/tools`: ' CLAUDE.md)"
printf '%s\n' "$l" | grep -q -e 'webfetch.go' && printf '%s\n' "$l" | grep -q -e 'htmltext.go' && printf '%s\n' "$l" | grep -q -e 'web_fetch'
CMD
expect_ok "e2e line names WebFetch" <<'CMD'
grep -e '^- `e2e`: ' CLAUDE.md | grep -q -e 'WebFetch'
CMD
expect_ok "Phase 6 exit: web_fetch in docs, >= 2 in CLAUDE.md, no jev in README, docs, CLAUDE.md" <<'CMD'
grep -q web_fetch docs/usage.md && test "$(grep -c web_fetch CLAUDE.md)" -ge 2 && ! grep -n -i jev README.md docs/usage.md CLAUDE.md
CMD
expect_fail "negative: no 'only web-like tool name' claim and no jev text in CLAUDE.md" <<'CMD'
no_match -n -i -e 'the only web-like tool name' -e 'jev' -e 'system one' CLAUDE.md
CMD
expect_ok "Delegation workflow section equals 5a884db" <<'CMD'
diff <(git show 5a884db:CLAUDE.md | sed -n '/^## Delegation workflow/,$p') <(sed -n '/^## Delegation workflow/,$p' CLAUDE.md)
CMD
expect_ok "security-critical paragraph unchanged" <<'CMD'
grep -q -F -e 'Security-critical logic lives only in `internal/provider/transport.go`, `internal/tools/pathguard.go`, `internal/tools/bash.go`, `internal/agent/approval.go` and `internal/config/redact.go`' CLAUDE.md
CMD
expect_ok "scope: only CLAUDE.md changed" <<'CMD'
changed="$( { git diff --name-only HEAD; git ls-files -o --exclude-standard; } | sort -u | grep -v -e '^versions/v8/Tasks.md$')"
printf '%s\n' "$changed"
test -z "$(printf '%s\n' "$changed" | grep -v -e '^$' -e '^CLAUDE.md$')"
CMD
finish
