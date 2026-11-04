#!/usr/bin/env bash
# Verification for T201 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"

probe_src t201 <<'GO'
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func zzvLines(s string) []string {
	var out []string
	for _, l := range strings.Split(ansi.Strip(s), "\n") {
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

func TestZZVerifyT201(t *testing.T) {
	src := "intro\n| Name | Qty | Mid |\n|:-----|----:|:---:|\n| apple | 3 | a |\n| kiwi | 12 | bbbbb |\noutro"
	got := strings.Join(zzvLines(renderMarkdown(src, 0)), "\n")
	want := strings.Join([]string{
		"intro",
		"┌───────┬─────┬───────┐",
		"│ Name  │ Qty │  Mid  │",
		"├───────┼─────┼───────┤",
		"│ apple │   3 │   a   │",
		"│ kiwi  │  12 │ bbbbb │",
		"└───────┴─────┴───────┘",
		"outro",
	}, "\n")
	if got != want {
		t.Fatalf("table:\n%s\nwant:\n%s", got, want)
	}
	cells := zzvLines(renderMarkdown("| A | B |\n|---|---|\n| x \\| y | `p|q` |\n| **b** |", 0))
	j := strings.Join(cells, "\n")
	for _, w := range []string{"x | y", "p|q", "│ b "} {
		if !strings.Contains(j, w) {
			t.Errorf("cells lack %q:\n%s", w, j)
		}
	}
	if strings.Contains(j, "**") || strings.Contains(j, "\\|") || strings.Contains(j, "|---") {
		t.Errorf("markup left in cells:\n%s", j)
	}
	if len(cells) != 6 {
		t.Errorf("want 6 table lines (top, header, rule, 2 rows, bottom), got %d:\n%s", len(cells), j)
	}
}

func TestZZVerifyT201Neg(t *testing.T) {
	for _, src := range []string{
		"| a | b |\n| c | d |",
		"a | b\n--- | --- | ---",
		"| a | b |\n|---|-x-|",
		"| a | b |",
	} {
		if got := ansi.Strip(renderMarkdown(src, 0)); got != src {
			t.Logf("no valid separator, but %q rendered as %q", src, got)
			return
		}
	}
	t.Fatal("REJECTED: pipe text without a valid separator line stays plain")
}
GO

expect_ok "T201 tests" <<'CMD'
go_tests ./internal/tui TestMarkdownTableBasic TestMarkdownTableAlign TestMarkdownTableCells TestMarkdownTableNotATable TestMarkdownTableHeaderBold
CMD
expect_ok "probe: exact table layout and cell splitting" <<'CMD'
go_probe internal/tui t201 TestZZVerifyT201
CMD
expect_fail "negative: pipe text without a valid separator becomes a table" <<'CMD'
go_probe_rejects internal/tui t201 TestZZVerifyT201Neg
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
