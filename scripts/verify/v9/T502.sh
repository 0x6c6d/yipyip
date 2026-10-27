#!/usr/bin/env bash
# Verification for T502 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "docs: /clear row" <<'CMD'
grep -F -e '| `/clear` | clear the current tab in place' docs/usage.md
CMD
expect_ok "docs: Ctrl+W row" <<'CMD'
grep -F -e '| Ctrl+W | close the current tab (saved first); the last tab is replaced by an empty one |' docs/usage.md
CMD
expect_ok "docs: Sessions say /clear and the last-tab close save nothing" <<'CMD'
grep -F -e 'Closing the last tab and `/clear` save nothing.' docs/usage.md
CMD
expect_ok "docs: Tabs describe the last-tab close" <<'CMD'
grep -F -e 'discards it without saving and leaves one new empty tab' docs/usage.md
CMD
expect_ok "docs: Markdown rendering section with its key words" <<'CMD'
sec="$(sed -n '/^### Markdown rendering/,/^## /p' docs/usage.md)"
test -n "$sec" || exit 1
for w in table heading italic 'inline code' blockquote '•' 'text (url)' fence stream redacted 'tool results' images; do
	printf '%s\n' "$sec" | grep -q -i -F -e "$w" || { echo "section lacks: $w"; exit 1; }
done
CMD
expect_ok "README Usage line lists /new and /clear separately" <<'CMD'
sed -n '/^## Usage/,$p' README.md | grep -F -e '`/new`, `/clear`'
CMD
expect_ok "README sections 1-9 unchanged since a4e4cbb" <<'CMD'
diff <(git show a4e4cbb:README.md | sed -n '1,/^## Usage/p') <(sed -n '1,/^## Usage/p' README.md)
CMD
expect_fail "negative: docs still call /clear an alias of /new" <<'CMD'
no_match -n -F -e 'alias of `/new`' docs/usage.md
CMD
expect_fail "negative: docs still say the last tab close is /exit" <<'CMD'
no_match -n -F -e 'is the same as `/exit`' docs/usage.md
CMD
expect_fail "negative: README Usage still shows the (/clear) alias" <<'CMD'
sed -n '/^## Usage/,$p' README.md | no_match -n -F -e '(`/clear`)'
CMD
expect_ok "no code path changed" <<'CMD'
test -z "$(git status --porcelain -- cmd internal e2e go.mod go.sum Makefile CLAUDE.md)"
CMD
finish
