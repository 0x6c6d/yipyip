#!/usr/bin/env bash
# Verification for T501 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "TestInputClick runs and passes" <<'CMD'
go_tests ./internal/tui TestInputClick
CMD
expect_ok "conversation copy-on-select, scroll and input tests unchanged and green" <<'CMD'
go_tests ./internal/tui TestCopySelection TestCopyClickNoDrag TestCopySecretNeverCopied TestCopyHighlightReverse && go test -count=1 -run 'Copy|Scroll|Suggest|History|Paste' ./internal/tui
CMD
expect_ok "input hit-testing lives in inputmouse.go" <<'CMD'
test -f internal/tui/inputmouse.go && grep -q -e 'func (m Model) inputCellAt' -e 'func (m \*Model) inputCellAt' internal/tui/inputmouse.go
CMD
expect_fail "negative: no new exec or clipboard writer in the tui" <<'CMD'
no_match -rln --include='*.go' -e '"os/exec"' internal/tui | grep -v -e 'internal/tui/clipboard.go' | no_match -e .
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 4fabda9 -- go.mod go.sum
CMD
expect_fail "negative: no protected path or other package touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v4/spec.md versions/v4/plan.md versions/v4/tasks internal/provider internal/tools internal/agent internal/config internal/session cmd)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
