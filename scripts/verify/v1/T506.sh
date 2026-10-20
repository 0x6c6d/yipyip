#!/usr/bin/env bash
# Verification for T506 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: tool blocks and highlighting" <<'CMD'
go_tests ./internal/tui TestToolBlockRendered TestToolResultCollapsed TestToolResultExpand TestHighlightGo TestHighlightUnknownLang TestFenceHighlightedInView
CMD
expect_ok "chroma pinned to v2.27.0" <<'CMD'
grep -q 'github.com/alecthomas/chroma/v2 v2.27.0' go.mod
CMD
expect_ok "exported API" <<'CMD'
has_symbols ./internal/tui Highlight
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_fail "negative: no third-party module beyond the allowed ones" <<'CMD'
out="$(go list -m -f '{{if not .Indirect}}{{.Path}}{{end}}' all)" || exit 125
printf '%s\n' "$out" | grep -v -x -e yipyip -e github.com/charmbracelet/bubbletea -e github.com/charmbracelet/bubbles -e github.com/charmbracelet/lipgloss -e github.com/alecthomas/chroma/v2 | no_match .
CMD
finish
