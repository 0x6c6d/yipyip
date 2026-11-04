#!/usr/bin/env bash
# Verification for T204 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"

probe_src t204 <<'GO'
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func zzvRenderW(src string, w int) string {
	var out []string
	for _, l := range strings.Split(ansi.Strip(renderMarkdown(src, w)), "\n") {
		out = append(out, strings.TrimRight(l, " "))
	}
	return strings.Join(out, "\n")
}

func TestZZVerifyT204(t *testing.T) {
	rule := strings.Repeat("─", 30)
	src := "> quoted **x**\n> > nested\n> back\n\n---\n***\n___\n--"
	want := "│ quoted x\n│ │ nested\n│ back\n\n" + rule + "\n" + rule + "\n" + rule + "\n--"
	if got := zzvRenderW(src, 30); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	for _, w := range []int{0, -1} {
		if got := zzvRenderW("---", w); got != strings.Repeat("─", 40) {
			t.Errorf("rule at width %d = %q, want 40 ─", w, got)
		}
	}
	deep := strings.Repeat(">", 5000) + " deep text"
	if got := zzvRenderW(deep, 80); !strings.Contains(got, "deep text") {
		t.Errorf("deep quote lost its text")
	}
}

func TestZZVerifyT204Neg(t *testing.T) {
	for _, src := range []string{"--", "a --- b", "-- -"} {
		if got := zzvRenderW(src, 30); got != src {
			t.Logf("%q rendered as %q", src, got)
			return
		}
	}
	t.Fatal("REJECTED: lines that are not rules stay literal")
}
GO

expect_ok "T204 tests" <<'CMD'
go_tests ./internal/tui TestMarkdownQuote TestMarkdownQuoteInner TestMarkdownQuoteDeep TestMarkdownRule TestMarkdownRuleNotRule
CMD
expect_ok "probe: quotes, nested quotes, rules" <<'CMD'
go_probe internal/tui t204 TestZZVerifyT204
CMD
expect_fail "negative: non-rules rendered as rules" <<'CMD'
go_probe_rejects internal/tui t204 TestZZVerifyT204Neg
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
