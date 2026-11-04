#!/usr/bin/env bash
# Verification for T203 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"

probe_src t203 <<'GO'
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func zzvRender(src string) string {
	var out []string
	for _, l := range strings.Split(ansi.Strip(renderMarkdown(src, 80)), "\n") {
		out = append(out, strings.TrimRight(l, " "))
	}
	return strings.Join(out, "\n")
}

func TestZZVerifyT203(t *testing.T) {
	cases := []struct{ src, want string }{
		{
			"- one\n  more of one\n- two **b**\n  - nested\n    - deeper\n* star\n+ plus\n\n1. first\n2) second\n10. tenth",
			"• one\n  more of one\n• two b\n  • nested\n    • deeper\n• star\n• plus\n\n1. first\n2) second\n10. tenth",
		},
		{"- a\n    - b\n        - c", "• a\n  • b\n    • c"},
	}
	for _, c := range cases {
		if got := zzvRender(c.src); got != c.want {
			t.Errorf("render(%q) =\n%s\nwant:\n%s", c.src, got, c.want)
		}
	}
}

func TestZZVerifyT203Neg(t *testing.T) {
	for src, want := range map[string]string{
		"-no space":      "-no space",
		"1.5 apples":     "1.5 apples",
		"+1 vote":        "+1 vote",
		"**bold** start": "bold start",
	} {
		if got := zzvRender(src); got != want || strings.Contains(got, "•") {
			t.Logf("%q rendered as %q, want %q", src, got, want)
			return
		}
	}
	t.Fatal("REJECTED: markers without a following space are not list items")
}
GO

expect_ok "T203 tests" <<'CMD'
go_tests ./internal/tui TestMarkdownListBullets TestMarkdownListNested TestMarkdownListNumbered TestMarkdownListContinuation TestMarkdownListNotItems
CMD
expect_ok "probe: nested, numbered and continued lists" <<'CMD'
go_probe internal/tui t203 TestZZVerifyT203
CMD
expect_fail "negative: non-items get a bullet or change" <<'CMD'
go_probe_rejects internal/tui t203 TestZZVerifyT203Neg
CMD
expect_ok "earlier Markdown tests still pass" <<'CMD'
go test ./internal/tui -run 'Markdown|Render|Highlight' -count=1
CMD
expect_ok "security files and go.mod unchanged" <<'CMD'
v9_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
