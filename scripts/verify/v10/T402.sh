#!/usr/bin/env bash
# Verification for T402 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"
expect_ok "tests" <<'CMD'
go_tests ./internal/tui TestRenderGuardBlock TestRenderGuardLogged TestRenderGuardDoesNotLeakText TestRenderGuardMarkdownBlock
CMD
expect_ok "existing rendering and markdown tests pass" <<'CMD'
go test -count=1 ./internal/tui -run 'Markdown|Block|Render|View'
CMD
expect_ok "the guard text is exactly [render error]" <<'CMD'
grep -rn -e '\[render error\]' internal/tui --include=*.go | grep -v _test.go | grep -q -e .
CMD
expect_fail "negative: the render-error text never interpolates block content" <<'CMD'
no_match -n -e 'render error\][^"]*%' -e 'render error\]" *+' internal/tui/blocks.go
CMD
expect_ok "security files, go.mod and go.sum unchanged since 1ccbbc7" <<'CMD'
v10_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
