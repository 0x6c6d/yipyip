#!/usr/bin/env bash
# Verification for T202 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"

probe_src t202 <<'GO'
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestZZVerifyT202(t *testing.T) {
	src := "| Column one | Column two | Column three |\n|---|---:|---|\n| " +
		strings.TrimSpace(strings.Repeat("word ", 12)) + " | short | " + strings.Repeat("x", 50) + " |"
	out := ansi.Strip(renderMarkdown(src, 40))
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[0], "┌") || !strings.HasPrefix(lines[len(lines)-1], "└") {
		t.Fatalf("not a box:\n%s", out)
	}
	w0 := ansi.StringWidth(lines[0])
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > 40 || w != w0 {
			t.Errorf("line width %d (first %d, limit 40): %q", w, w0, l)
		}
	}
	if n := strings.Count(out, "x"); n != 50 {
		t.Errorf("%d x characters, want 50:\n%s", n, out)
	}
	if n := strings.Count(out, "word"); n != 12 {
		t.Errorf("%d words, want 12:\n%s", n, out)
	}
	for _, w := range []string{"short", "Column", "one", "two", "three"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q:\n%s", w, out)
		}
	}
	small := "| a | b |\n|---|---|\n| c | d |"
	if a, b := renderMarkdown(small, 0), renderMarkdown(small, 80); a != b {
		t.Errorf("a fitting table differs at width 0 and 80:\n%s\n%s", a, b)
	}
}

func TestZZVerifyT202Neg(t *testing.T) {
	src := "|a|b|c|d|e|f|g|h|i|j|\n|-|-|-|-|-|-|-|-|-|-|\n|1|2|3|4|5|6|7|8|9|0|"
	out := ansi.Strip(renderMarkdown(src, 20))
	for _, l := range strings.Split(out, "\n") {
		if ansi.StringWidth(l) > 20 && strings.ContainsAny(l, "┌├└│") {
			t.Logf("a box wider than 20: %q", l)
			return
		}
	}
	if out != src {
		t.Fatalf("a table that cannot fit must come back as its source, got:\n%s", out)
	}
	t.Fatal("REJECTED: a table that cannot fit is never drawn wider than the width")
}
GO

expect_ok "T202 tests" <<'CMD'
go_tests ./internal/tui TestMarkdownTableWrap TestMarkdownTableWrapAlign TestMarkdownTableFitsUnchanged TestMarkdownTableTooNarrow TestMarkdownTableWideChars TestMarkdownTableView
CMD
expect_ok "probe: wide table wrapped at width 40" <<'CMD'
go_probe internal/tui t202 TestZZVerifyT202
CMD
expect_fail "negative: a table that cannot fit overflows the width" <<'CMD'
go_probe_rejects internal/tui t202 TestZZVerifyT202Neg
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
