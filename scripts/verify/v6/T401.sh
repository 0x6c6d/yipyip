#!/usr/bin/env bash
# Verification for T401 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "spec acceptance: usage.md documents the search block and BRAVE_API_KEY" <<'CMD'
grep -q -F -e '"search"' docs/usage.md && grep -q -e 'BRAVE_API_KEY' docs/usage.md && grep -q -F -e '{env:BRAVE_API_KEY}' docs/usage.md && grep -q -F -e 'https://api.search.brave.com' docs/usage.md
CMD
expect_ok "usage.md has a Web search section about web_search, approvals and untrusted results" <<'CMD'
grep -q -e '^## Web search' docs/usage.md && grep -q -e 'web_search' docs/usage.md && grep -q -i -e 'untrusted' docs/usage.md && sed -n '/^## Web search/,/^## /p' docs/usage.md | grep -q -i -e 'approv'
CMD
expect_ok "usage.md documents the read rule with a warning and confined writes" <<'CMD'
grep -q -e 'read_file' docs/usage.md && grep -q -F -e '.ssh' docs/usage.md && sed -n '/^## Security/,$p' docs/usage.md | grep -q -e 'write_file' && sed -n '/^## Security/,$p' docs/usage.md | grep -q -i -e 'anywhere'
CMD
expect_ok "web_search listed in the Modes section" <<'CMD'
sed -n '/^## Modes/,/^## /p' docs/usage.md | grep -q -e 'web_search'
CMD
expect_fail "negative: old claims (no web tools, only inside that directory, file tools confined) still present" <<'CMD'
tr '\n' ' ' < docs/usage.md | tr -s ' ' > "$VTMP/flat.txt" || exit 125
no_match -o -e 'block is configured\. yip has no web tools' -e 'only inside that directory' -e 'are confined to the start directory; paths that escape it are refused' "$VTMP/flat.txt"
CMD
expect_fail "negative: a real-looking key value in usage.md" <<'CMD'
no_match -n -E -e 'BSA[A-Za-z0-9_-]{20,}' -e '"apiKey": *"[A-Za-z0-9]{24,}"' docs/usage.md
CMD
expect_fail "negative: a file other than docs/usage.md changed (or a protected path)" <<'CMD'
out="$(git status --porcelain --untracked-files=all)" || exit 125
printf '%s\n' "$out" | sed -e 's/^...//' -e 's/^.* -> //' | grep -v -x -e '' -e 'versions/v6/Tasks.md' -e 'docs/usage.md' | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
