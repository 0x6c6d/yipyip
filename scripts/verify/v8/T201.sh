#!/usr/bin/env bash
# Verification for T201 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_fail "negative: no jev, typesafe or System One in README.md" <<'CMD'
no_match -n -i -e 'jev' -e 'typesafe' -e 'system one' README.md
CMD
expect_ok "README sections 1-9 (before ## Usage) unchanged since a99a82f" <<'CMD'
diff <(git show a99a82f:README.md | sed '/^## Usage/,$d') <(sed '/^## Usage/,$d' README.md)
CMD
expect_ok "diff against a99a82f adds no non-empty line" <<'CMD'
added="$(git diff -U0 a99a82f -- README.md | grep -e '^+' | grep -v -e '^+++ ' -e '^+$')"
printf '%s\n' "$added"
test -z "$added"
CMD
expect_ok "Usage bullets and the docs/usage.md link remain" <<'CMD'
grep -q -e '^## Usage' README.md &&
grep -q -e '^- Config: ' README.md && grep -q -e '^- Modes: ' README.md &&
grep -q -e '^- Commands: ' README.md && grep -q -e '^- Keys: ' README.md &&
grep -q -F -e '[docs/usage.md](docs/usage.md)' README.md
CMD
expect_ok "no trailing blank line at the end of README.md" <<'CMD'
test -n "$(tail -n 1 README.md)"
CMD
expect_ok "scope: only README.md changed" <<'CMD'
changed="$( { git diff --name-only HEAD; git ls-files -o --exclude-standard; } | sort -u | grep -v -e '^versions/v8/Tasks.md$')"
printf '%s\n' "$changed"
test -z "$(printf '%s\n' "$changed" | grep -v -e '^$' -e '^README.md$')"
CMD
finish
