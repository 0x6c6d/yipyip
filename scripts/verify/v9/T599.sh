#!/usr/bin/env bash
# Verification for T599 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"
expect_ok "R73: go test ./internal/tui -run Clear" <<'CMD'
go test ./internal/tui -run Clear -count=1 -v
CMD
expect_ok "R73: TestClear* tests ran" <<'CMD'
go_tests ./internal/tui TestClearKeepsTab TestClearWhileBusy TestClearKeepsHistory TestClearNoSave TestClearNewStillAddsTab
CMD
expect_ok "R74: go test ./internal/tui -run CloseLastTab" <<'CMD'
go test ./internal/tui -run CloseLastTab -count=1 -v
CMD
expect_ok "R74: TestCloseLastTab* tests ran" <<'CMD'
go_tests ./internal/tui TestCloseLastTabCtrlW TestCloseLastTabClick TestCloseLastTabTwoTabs
CMD
expect_ok "R75, R76: go test ./internal/tui -run Markdown" <<'CMD'
go test ./internal/tui -run Markdown -count=1 -v
CMD
expect_ok "R75, R76: key Markdown tests ran" <<'CMD'
go_tests ./internal/tui TestMarkdownTableBasic TestMarkdownTableWrap TestMarkdownFenceUntouched TestMarkdownIncomplete TestMarkdownSecretReassembly FuzzMarkdown
CMD
expect_ok "R75, R77: go test ./e2e -run Markdown" <<'CMD'
go test ./e2e -run Markdown -count=1
CMD
expect_ok "R75, R77: e2e Markdown tests ran" <<'CMD'
go_tests ./e2e TestMarkdownE2E TestMarkdownE2ECopy
CMD
expect_ok "R73: /help lists /clear without alias" <<'CMD'
grep -n '/clear' internal/tui/help.go | grep -v alias
CMD
expect_ok "§5: go.mod, go.sum and the security files unchanged since a4e4cbb" <<'CMD'
v9_guard
CMD
expect_ok "secrets: go test ./... -run Secret" <<'CMD'
go test ./... -run Secret -count=1
CMD
expect_ok "README sections 1-9 unchanged since a4e4cbb" <<'CMD'
diff <(git show a4e4cbb:README.md | sed -n '1,/^## Usage/p') <(sed -n '1,/^## Usage/p' README.md)
CMD
expect_ok "CLAUDE.md Delegation workflow section unchanged since a4e4cbb" <<'CMD'
diff <(git show a4e4cbb:CLAUDE.md | sed -n '/^## Delegation workflow/,$p') <(sed -n '/^## Delegation workflow/,$p' CLAUDE.md)
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_fail "negative: help.go still calls /clear an alias of /new" <<'CMD'
no_match -n -e 'alias of /new' internal/tui/help.go
CMD
expect_fail "negative: docs/usage.md still calls /clear an alias" <<'CMD'
no_match -n -F -e 'alias of `/new`' docs/usage.md
CMD
expect_fail "negative: a zz_verify probe file was left in the tree" <<'CMD'
find . -path ./.git -prune -o -name 'zz_verify_*' -print | grep .
CMD
expect_ok "working tree clean apart from versions/v9/Tasks.md" <<'CMD'
test -z "$(git status --porcelain | grep -v -e ' versions/v9/Tasks.md$')"
CMD
finish
