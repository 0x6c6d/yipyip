#!/usr/bin/env bash
# Verification for T205 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"

probe_src t205 <<'GO'
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestZZVerifyT205(t *testing.T) {
	for src, want := range map[string]string{
		"*it* and _it2_":                 "it and it2",
		"~~gone~~ text":                  "gone text",
		"__b__ x":                        "b x",
		"snake_case_name stays":          "snake_case_name stays",
		"2 * 3 * 4":                      "2 * 3 * 4",
		"**unclosed and *also":           "**unclosed and *also",
		"`a **b** [l](u)` code":          "a **b** [l](u) code",
		"``a ` b`` two":                  "a ` b two",
		"see [docs](https://x.io/a) now": "see docs (https://x.io/a) now",
		"[https://x.io](https://x.io)":   "https://x.io",
		"<https://x.io/p>":               "https://x.io/p",
		"![img](a.png)":                  "![img](a.png)",
		"[no link]":                      "[no link]",
		`\*lit\* and \_u\_`:              "*lit* and _u_",
		`C:\path`:                        `C:\path`,
	} {
		if got := ansi.Strip(renderMarkdown(src, 80)); got != want {
			t.Errorf("render(%q) = %q, want %q", src, got, want)
		}
	}
}

func TestZZVerifyT205Neg(t *testing.T) {
	for _, src := range []string{"snake_case_name", "2 * 3 * 4", "file_name_here.go and snake__double__name", "`**x** _y_ [a](b)`"} {
		want := src
		if strings.HasPrefix(src, "`") {
			want = strings.Trim(src, "`")
		}
		if got := ansi.Strip(renderMarkdown(src, 80)); got != want {
			t.Logf("%q rendered as %q, want %q", src, got, want)
			return
		}
	}
	t.Fatal("REJECTED: intraword underscores, spaced stars and code span content stay literal")
}

func TestZZVerifyT205Secret(t *testing.T) {
	for _, answer := range []string{"a SEC`RET`123 b", "a SEC~~RET~~123 b"} {
		m, _ := newSessionModel(t, answerServer(t, answer))
		m = tabTurn(t, m, "q")
		if v := plainView(m); strings.Contains(v, "SECRET123") {
			t.Logf("answer %q shows the secret:\n%s", answer, v)
			return
		}
	}
	t.Fatal("REJECTED: code span and strike reassembly never shows the secret")
}
GO

expect_ok "T205 tests" <<'CMD'
go_tests ./internal/tui TestMarkdownInlineEmphasis TestMarkdownInlineCode TestMarkdownInlineLinks TestMarkdownInlineEscapes TestMarkdownSecretReassembly
CMD
expect_ok "probe: inline forms" <<'CMD'
go_probe internal/tui t205 TestZZVerifyT205
CMD
expect_fail "negative: intraword underscores, spaced stars or code content interpreted" <<'CMD'
go_probe_rejects internal/tui t205 TestZZVerifyT205Neg
CMD
expect_fail "negative: inline reassembly shows SECRET123 in the view" <<'CMD'
go_probe_rejects internal/tui t205 TestZZVerifyT205Secret
CMD
expect_ok "earlier Markdown tests still pass" <<'CMD'
go test ./internal/tui -run 'Markdown|Render|Highlight|Copy' -count=1
CMD
expect_ok "security files and go.mod unchanged" <<'CMD'
v9_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
