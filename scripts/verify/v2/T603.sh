#!/usr/bin/env bash
# Verification for T603 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "Copy tests" <<'CMD'
go_tests ./internal/tui TestCopySelection TestCopyTrailingSpacesTrimmed TestCopyHighlightReverse TestCopyNotice TestCopyFinishedMarkerPlain
CMD
expect_ok "negative: click without drag copies nothing" <<'CMD'
go_tests ./internal/tui TestCopyClickNoDrag
CMD
expect_ok "negative: SECRET123 never reaches the clipboard" <<'CMD'
go_tests ./internal/tui TestCopySecretNeverCopied
CMD
expect_ok "all Copy tests and all Secret tests repo-wide" <<'CMD'
go test -count=1 ./internal/tui -run Copy && go test -count=1 ./... -run Secret
CMD
expect_fail "negative: copy path never reads raw block text" <<'CMD'
test -f internal/tui/mouse.go || exit 125
no_match -n -e '\.blocks\[' -e 'b\.text' internal/tui/mouse.go
CMD
expect_ok "os/exec is imported only by internal/tools/bash.go" <<'CMD'
got="$(grep -rl --include='*.go' --exclude='*_test.go' -e '"os/exec"' cmd internal e2e | sort)"
[ "$got" = "internal/tools/bash.go" ] || { echo "os/exec importers: $got"; exit 1; }
CMD
expect_fail "negative: no file except internal/tui/clipboard.go imports the clipboard packages" <<'CMD'
test -f internal/tui/clipboard.go || exit 125
no_match -rln --include='*.go' --exclude='clipboard.go' --exclude='clipboard_test.go' -e 'atotto/clipboard' -e 'go-osc52' cmd internal e2e
CMD
expect_fail "negative: no protected path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v2/spec.md versions/v2/plan.md versions/v2/tasks)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: agent, tools, provider, config unchanged since v1 (d3eeba4)" <<'CMD'
git rev-parse -q --verify 'd3eeba4^{commit}' >/dev/null || exit 125
out="$(git diff d3eeba4 -- internal/agent internal/tools internal/provider internal/config && git status --porcelain --untracked-files=all -- internal/agent internal/tools internal/provider internal/config)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
