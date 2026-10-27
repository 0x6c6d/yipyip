#!/usr/bin/env bash
# Verification for T501 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"
expect_ok "T501 tests" <<'CMD'
go_tests ./e2e TestMarkdownE2E TestMarkdownE2ECopy
CMD
expect_ok "acceptance: go test ./e2e -run Markdown" <<'CMD'
go test ./e2e -run Markdown -count=1
CMD
expect_ok "envOpts has a clipboard field wired into tui.Options" <<'CMD'
grep -q -e 'clipboard *func(string) error' e2e/helpers_test.go && grep -q -e 'Clipboard: *o\.clipboard' e2e/helpers_test.go
CMD
expect_ok "the e2e test uses the secret and the borders" <<'CMD'
grep -q -e 'SECRET123' e2e/markdown_test.go && grep -q -e '┌' e2e/markdown_test.go && grep -q -e 'MouseActionRelease' e2e/markdown_test.go
CMD
expect_fail "negative: the e2e Markdown tests skip" <<'CMD'
no_match -n -e 't\.Skip' e2e/markdown_test.go
CMD
expect_fail "negative: SECRET123 appears in the verbose e2e output" <<'CMD'
out="$(go test ./e2e -run 'Markdown' -count=1 -v 2>&1)" || { echo "$out" | tail -20; exit 125; }
printf '%s\n' "$out" | no_match -n -e 'SECRET123'
CMD
expect_ok "only the e2e test files changed in e2e" <<'CMD'
test -z "$(git status --porcelain -- e2e | grep -v -e ' e2e/markdown_test.go$' -e ' e2e/helpers_test.go$')"
CMD
expect_ok "security files and go.mod unchanged" <<'CMD'
v9_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
