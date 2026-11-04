#!/usr/bin/env bash
# Verification for T503 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "CLAUDE.md tui bullet names markdown.go and renderMarkdown" <<'CMD'
grep -n -e '^- `internal/tui`' CLAUDE.md | grep -F -e '`markdown.go`' | grep -F -e 'renderMarkdown'
CMD
expect_ok "CLAUDE.md tui bullet names resetTab" <<'CMD'
grep -n -e '^- `internal/tui`' CLAUDE.md | grep -F -e '`resetTab`' | grep -F -e '/clear'
CMD
expect_ok "CLAUDE.md e2e bullet lists Markdown" <<'CMD'
grep -n -e '^- `e2e`' CLAUDE.md | grep -F -e '`WebFetch`, `Markdown`'
CMD
expect_ok "Delegation workflow section unchanged since a4e4cbb" <<'CMD'
diff <(git show a4e4cbb:CLAUDE.md | sed -n '/^## Delegation workflow/,$p') <(sed -n '/^## Delegation workflow/,$p' CLAUDE.md)
CMD
expect_ok "Project and Commands sections unchanged since a4e4cbb" <<'CMD'
diff <(git show a4e4cbb:CLAUDE.md | sed -n '1,/^## Architecture/p') <(sed -n '1,/^## Architecture/p' CLAUDE.md)
CMD
expect_fail "negative: CLAUDE.md still describes highlight.go as the only renderer" <<'CMD'
no_match -n -F -e '`blocks.go` and `highlight.go` conversation rendering; `turn.go`' CLAUDE.md
CMD
expect_ok "no code path changed" <<'CMD'
test -z "$(git status --porcelain -- cmd internal e2e go.mod go.sum Makefile README.md docs)"
CMD
finish
