#!/usr/bin/env bash
# Verification for T206 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"

probe_src t206 <<'GO'
package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestZZVerifyT206(t *testing.T) {
	for _, src := range []string{
		"| a | b |\n| c",
		"some **unclosed",
		"an `unclosed code",
		"```go\n| x | y |\n|---|---|\n# not\n**b**",
	} {
		if got := ansi.Strip(renderMarkdown(src, 40)); got != src {
			t.Errorf("incomplete %q rendered as %q", src, got)
		}
	}
	fence := ansi.Strip(renderMarkdown("```nosuch\n| x |\n|---|\n**b** # h\n```", 40))
	for _, want := range []string{"| x |", "|---|", "**b** # h"} {
		if !strings.Contains(fence, want) {
			t.Errorf("fence lacks %q: %q", want, fence)
		}
	}
	if strings.Contains(fence, "```") || strings.Contains(fence, "┌") {
		t.Errorf("fence interpreted: %q", fence)
	}
	done := ansi.Strip(renderMarkdown("| a | b |\n|---|---|\n| c | d |", 40))
	if !strings.Contains(done, "┌") || strings.Contains(done, "|---") {
		t.Errorf("finished table not rendered: %q", done)
	}
}

func TestZZVerifyT206Robust(t *testing.T) {
	inputs := []string{"", "|", "||", "|-|", "| a |\n|-|-|-|", "**", "*", "_", "~~", "`", "``", "```",
		"[", "](", "[a](", "<http", "> ", "- ", "1.", "#", "######", "\x00\xff\xfe",
		"| 漢字 | 😀 |\n|---|---|\n| 😀😀😀😀😀😀😀😀😀😀 | x |",
		strings.Repeat(">", 5000) + " deep",
		strings.Repeat("**a", 2000),
		strings.Repeat("| x ", 300) + "|\n" + strings.Repeat("|-", 300) + "|\n" + strings.Repeat("| y ", 300) + "|",
	}
	for _, src := range inputs {
		for _, w := range []int{-1, 0, 1, 2, 5, 40} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("panic for %.40q at width %d: %v", src, w, r)
					}
				}()
				a, b := renderMarkdown(src, w), renderMarkdown(src, w)
				if a != b {
					t.Errorf("not deterministic for %.40q at width %d", src, w)
				}
			}()
		}
	}
	big := strings.Repeat("| a **b** `c` | [d](e) |\n|---|---|\n| x | y |\n\n- item _i_\n  - sub *s*\n> q ~~z~~\n\n", 1200)
	start := time.Now()
	_ = renderMarkdown(big, 80)
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("%d bytes took %v", len(big), d)
	}
}

func TestZZVerifyT206Neg(t *testing.T) {
	src := "```\n# h\n| a | b |\n|---|---|\n- i\n> q\n```\nand `# x | y |` here"
	got := ansi.Strip(renderMarkdown(src, 40))
	for _, want := range []string{"# h", "| a | b |", "|---|---|", "- i", "> q", "# x | y |"} {
		if !strings.Contains(got, want) {
			t.Logf("fence or code content interpreted, %q missing:\n%s", want, got)
			return
		}
	}
	if strings.Contains(got, "┌") || strings.Contains(got, "•") || strings.Contains(got, "│") {
		t.Logf("fence content interpreted:\n%s", got)
		return
	}
	t.Fatal("REJECTED: markdown inside fences and inline code is not interpreted")
}
GO

expect_ok "T206 tests and fuzz seed corpus" <<'CMD'
go_tests ./internal/tui TestMarkdownIncomplete TestMarkdownFenceUntouched TestMarkdownNoWidth TestMarkdownNoPanic TestMarkdownLargeInput TestMarkdownStreamingView FuzzMarkdown
CMD
expect_ok "FuzzMarkdown has at least 20 seeds" <<'CMD'
test "$(grep -c -e 'f\.Add(' internal/tui/markdown_test.go)" -ge 1 && go test ./internal/tui -run '^FuzzMarkdown$' -count=1 -v 2>&1 | grep -c -e '^    --- PASS: FuzzMarkdown/seed#' | awk '{ exit !($1 >= 20) }'
CMD
expect_ok "no committed fuzz corpus" <<'CMD'
test ! -e internal/tui/testdata/fuzz
CMD
expect_ok "probe: incomplete, fenced and finished input" <<'CMD'
go_probe internal/tui t206 TestZZVerifyT206
CMD
expect_ok "probe: no panic, deterministic, about 100 KB under 2 s" <<'CMD'
go_probe internal/tui t206 TestZZVerifyT206Robust
CMD
expect_fail "negative: markdown inside fences or inline code interpreted" <<'CMD'
go_probe_rejects internal/tui t206 TestZZVerifyT206Neg
CMD
expect_ok "all Markdown tests" <<'CMD'
go test ./internal/tui -run 'Markdown|Render|Highlight|Copy' -count=1
CMD
expect_ok "security files and go.mod unchanged" <<'CMD'
v9_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
