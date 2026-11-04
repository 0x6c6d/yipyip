#!/usr/bin/env bash
# Verification for T104 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: TUI skeleton" <<'CMD'
go_tests ./internal/tui TestSkeletonExit TestSkeletonStatusLine TestSkeletonEcho TestHelpTextListsCommands
CMD
expect_ok "tests: cmd/yip" <<'CMD'
go_tests ./cmd/yip TestConfigErrorExitCode TestVersionFlag TestHelpFlag
CMD
expect_ok "pinned UI dependencies in go.mod" <<'CMD'
grep -q 'github.com/charmbracelet/bubbletea v1.3.10' go.mod && grep -q 'github.com/charmbracelet/bubbles v0.21.1' go.mod && grep -q 'github.com/charmbracelet/lipgloss v1.1.0' go.mod
CMD
expect_ok "phase 1 exit criterion: make check and go run --version" <<'CMD'
make check && go run ./cmd/yip --version | grep -q '^yip '
CMD
expect_ok "--version still works next to an invalid yipyip.json" <<'CMD'
go build -o "$VTMP/yip" ./cmd/yip || exit 1
d="$(mktemp -d)"; echo '{bad' > "$d/yipyip.json"
(cd "$d" && HOME="$(mktemp -d)" "$VTMP/yip" --version) | grep -q '^yip '
CMD
expect_fail "negative: invalid yipyip.json makes yip exit 1 naming the file" <<'CMD'
go build -o "$VTMP/yip" ./cmd/yip || exit 125
d="$(mktemp -d)"; h="$(mktemp -d)"; echo '{bad' > "$d/yipyip.json"
err="$(cd "$d" && HOME="$h" "$VTMP/yip" 2>&1 >/dev/null </dev/null)"; rc=$?
echo "rc=$rc $err"
if [ "$rc" -eq 1 ] && printf '%s' "$err" | grep -q 'yipyip.json'; then exit 1; fi
exit 0
CMD
expect_fail "negative: no third-party module beyond the allowed ones" <<'CMD'
out="$(go list -m -f '{{if not .Indirect}}{{.Path}}{{end}}' all)" || exit 125
printf '%s\n' "$out" | grep -v -x -e yipyip -e github.com/charmbracelet/bubbletea -e github.com/charmbracelet/bubbles -e github.com/charmbracelet/lipgloss -e github.com/alecthomas/chroma/v2 | no_match .
CMD
finish
