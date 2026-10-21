#!/usr/bin/env bash
# Verification for T602 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "clipboard modules are direct requirements" <<'CMD'
grep -Eq -e '^\s+github.com/atotto/clipboard v0.1.4$' go.mod && grep -Eq -e '^\s+github.com/aymanbagabas/go-osc52/v2 v2.0.1$' go.mod
CMD
expect_ok "offline build, go.mod tidy-consistent" <<'CMD'
GOPROXY=off GOFLAGS=-mod=readonly go build ./... && GOPROXY=off go mod verify
CMD
expect_ok "clipboard tests" <<'CMD'
go_tests ./internal/tui TestCopyOSC52Sequence TestCopyOSC52Unicode TestCopyClipboardOption
CMD
expect_ok "Options.Clipboard and writer exist" <<'CMD'
grep -Eq -e '^\s+Clipboard\s+func\(string\) error' internal/tui/model.go && grep -q -e 'func writeClipboard(w io.Writer, useTool bool, text string) error' internal/tui/clipboard.go
CMD
expect_fail "negative: clipboard.go builds no command itself" <<'CMD'
no_match -n -e 'exec\.' -e 'sh -c' -e '"os/exec"' internal/tui/clipboard.go
CMD
expect_fail "negative: no test calls the real clipboard writer" <<'CMD'
no_match -rn --include='*_test.go' -e 'defaultClipboard(' -e 'writeClipboard([^,]*, *true' -e 'clipboard.WriteAll' internal cmd e2e
CMD
expect_ok "CLAUDE.md mentions clipboard.go" <<'CMD'
grep -q -e 'clipboard.go' CLAUDE.md
CMD
expect_fail "negative: CLAUDE.md Delegation section unchanged" <<'CMD'
a="$(git show HEAD:CLAUDE.md | sed -n '/^## Delegation workflow/,$p')" || exit 125
b="$(sed -n '/^## Delegation workflow/,$p' CLAUDE.md)"
[ -n "$a" ] || exit 125
[ "$a" = "$b" ] && exit 1
diff <(printf '%s\n' "$a") <(printf '%s\n' "$b"); exit 0
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
