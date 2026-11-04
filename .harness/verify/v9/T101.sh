#!/usr/bin/env bash
# Verification for T101 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"

probe_src t101 <<'GO'
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestZZVerifyT101(t *testing.T) {
	src := "# Title\n\nsome **bold** text\n\n```go\nx := 1 // **no** # | y |\n```\nlast line"
	out := ansi.Strip(renderMarkdown(src, 80))
	for _, want := range []string{"Title", "some bold text", "x := 1 // **no** # | y |", "last line"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	for _, bad := range []string{"# Title", "**bold**", "```"} {
		if strings.Contains(out, bad) {
			t.Errorf("markup %q left in %q", bad, out)
		}
	}
	for _, plain := range []string{"just text", "a - b and x | y", "snake_case and 2 * 3", "```go\nunclosed **x**\n# h"} {
		for _, w := range []int{-1, 0, 80} {
			if got := renderMarkdown(plain, w); got != plain {
				t.Errorf("renderMarkdown(%q, %d) = %q, want it unchanged", plain, w, got)
			}
		}
	}
}

func TestZZVerifyT101Neg(t *testing.T) {
	for _, src := range []string{"####### seven", "#nospace", "**unclosed bold"} {
		if got := ansi.Strip(renderMarkdown(src, 80)); got != src {
			t.Logf("invalid markup %q was interpreted: %q", src, got)
			return
		}
	}
	t.Fatal("REJECTED: seven hashes, a hash without space and unclosed bold stay literal")
}
GO

expect_ok "T101 tests" <<'CMD'
go_tests ./internal/tui TestMarkdownSkeletonHeadings TestMarkdownSkeletonBold TestMarkdownSkeletonFence TestMarkdownSkeletonPassThrough
CMD
expect_ok "existing highlight tests" <<'CMD'
go_tests ./internal/tui TestHighlightGo TestHighlightUnknownLang TestFenceHighlightedInView
CMD
expect_ok "probe: heading, bold, fence, pass-through" <<'CMD'
go_probe internal/tui t101 TestZZVerifyT101
CMD
expect_fail "negative: invalid headings and unclosed bold are not interpreted" <<'CMD'
go_probe_rejects internal/tui t101 TestZZVerifyT101Neg
CMD
expect_ok "markdown.go imports only stdlib, lipgloss, x/ansi" <<'CMD'
test -f internal/tui/markdown.go && ! go list -f '{{join .Imports "\n"}}' internal/tui/markdown.go | grep -e '\.' | grep -v -e '^github.com/charmbracelet/lipgloss$' -e '^github.com/charmbracelet/x/ansi$' -e '^github.com/alecthomas/chroma/v2'
CMD
expect_ok "security files and go.mod unchanged" <<'CMD'
v9_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
