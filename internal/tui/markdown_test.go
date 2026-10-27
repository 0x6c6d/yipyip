package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"yipyip/internal/agent"
)

// mdStrip renders src at width 80 and strips the ANSI styling.
func mdStrip(src string) string {
	return ansi.Strip(renderMarkdown(src, 80))
}

func TestMarkdownSkeletonHeadings(t *testing.T) {
	cases := map[string]string{
		"# One":             "One",
		"## Two":            "Two",
		"### Three":         "Three",
		"#### Four":         "Four",
		"##### Five":        "Five",
		"###### Six":        "Six",
		"   # Indented":     "Indented",
		"## Closed ##":      "Closed",
		"# Kept#":           "Kept#",
		"#":                 "",
		"### ###":           "",
		"####### x":         "####### x",
		"#x":                "#x",
		"    # four spaces": "    # four spaces",
		"# a\r":             "a",
	}
	for src, want := range cases {
		if got := mdStrip(src); got != want {
			t.Errorf("renderMarkdown(%q) = %q, want %q", src, got, want)
		}
	}
	withColorProfile(t, termenv.ANSI256)
	for _, src := range []string{"# Title", "### Sub"} {
		raw := renderMarkdown(src, 80)
		if raw == ansi.Strip(raw) {
			t.Errorf("heading %q not styled: %q", src, raw)
		}
	}
	if got := mdStrip("# Title\nbody\n## Sub"); got != "Title\nbody\nSub" {
		t.Errorf("multi-line headings = %q", got)
	}
}

func TestMarkdownSkeletonBold(t *testing.T) {
	cases := map[string]string{
		"**b**":                  "b",
		"__b__":                  "b",
		"a **bold** c":           "a bold c",
		"# a **b** c":            "a b c",
		"**x":                    "**x",
		"**unclosed bold":        "**unclosed bold",
		"snake__case":            "snake__case",
		"snake__case__x":         "snake__case__x",
		"** x**":                 "** x**",
		"**x **":                 "**x **",
		"`**x**`":                "**x**",
		"a `**x**` **y**":        "a **x** y",
		"**a `**` b**":           "a ** b",
		"``code ` **x**`` **y**": "code ` **x** y",
		"`unclosed **y**":        "`unclosed y",
	}
	for src, want := range cases {
		if got := mdStrip(src); got != want {
			t.Errorf("renderMarkdown(%q) = %q, want %q", src, got, want)
		}
	}
	withColorProfile(t, termenv.ANSI256)
	raw := renderMarkdown("**b**", 80)
	if raw == "b" || ansi.Strip(raw) != "b" {
		t.Errorf("bold not styled: %q", raw)
	}
	if raw := renderMarkdown("`**x**`", 80); ansi.Strip(raw) != "**x**" || strings.Count(raw, "\x1b[1m") > 0 {
		t.Errorf("code span interpreted: %q", raw)
	}
}

func TestMarkdownSkeletonFence(t *testing.T) {
	src := "before\n```go\nx := 1 // **no** # | y |\n# not a heading\n```\nafter **b**"
	out := mdStrip(src)
	if strings.Contains(out, "```") {
		t.Errorf("fence line left in %q", out)
	}
	want := "before\nx := 1 // **no** # | y |\n# not a heading\nafter b"
	if out != want {
		t.Errorf("closed fence = %q, want %q", out, want)
	}
	if got := mdStrip("```nosuchlang\na **b** | c\n```"); got != "a **b** | c" {
		t.Errorf("unknown language fence = %q", got)
	}
	for _, unclosed := range []string{
		"```go\nfunc main() {}",
		"text\n```go\n# h\n**x**",
		"```",
	} {
		if got := renderMarkdown(unclosed, 80); got != unclosed {
			t.Errorf("unclosed fence output %q differs from input %q", got, unclosed)
		}
	}
	withColorProfile(t, termenv.ANSI256)
	code := "func main() {}"
	if got := renderMarkdown("```go\n"+code+"\n```", 80); got != highlightFences("```go\n"+code+"\n```") {
		t.Errorf("closed fence %q differs from highlightFences", got)
	}
}

func TestMarkdownSkeletonPassThrough(t *testing.T) {
	plain := []string{
		"",
		"just text",
		"a - b",
		"x | y",
		"snake_case",
		"2 * 3",
		"a * b * c",
		"tab\tseparated",
		"line one\nline two\n\n\nafter blanks",
		"trailing newline\n",
		"under_score_name and a__b__c",
		"emoji ✓ and ünïcödé",
		"invalid \xff utf8",
	}
	for _, src := range plain {
		for _, w := range []int{0, -1, 80} {
			if got := renderMarkdown(src, w); got != src {
				t.Errorf("renderMarkdown(%q, %d) = %q, want it unchanged", src, w, got)
			}
		}
	}
	if got := renderMarkdown("a\r\nb\r", 0); got != "a\nb" {
		t.Errorf("carriage returns kept: %q", got)
	}
	for _, src := range []string{"**\xff**", "# \xff", "`\xff", "__\xff__", "**", "__", "***", "****"} {
		_ = renderMarkdown(src, 80) // must not panic
	}
}

func TestMarkdownSkeletonView(t *testing.T) {
	m := feed(newBlockModel(), agent.Event{Kind: agent.EvText, AgentID: "main", Text: "# Big Title\n\nplain **bold** words"}).(Model)
	v := plainView(m)
	var title, body bool
	for _, line := range strings.Split(v, "\n") {
		title = title || strings.HasPrefix(line, "● Big Title")
		body = body || strings.TrimRight(line, " ") == "  plain bold words"
	}
	if !title || !body {
		t.Errorf("rendered assistant block missing (title %v, body %v):\n%s", title, body, v)
	}
	if strings.Contains(v, "**") || strings.Contains(v, "# Big") {
		t.Errorf("markup left in the view:\n%s", v)
	}
}

func TestMarkdownSkeletonOtherBlocksRaw(t *testing.T) {
	m, _ := newSessionModel(t, answerServer(t, "ok"))
	m = tabTurn(t, m, "**mine**")
	m = feed(m,
		agent.Event{Kind: agent.EvToolCall, AgentID: "main", Tool: "read_file", Args: `{"path":"a.md"}`},
		agent.Event{Kind: agent.EvToolResult, AgentID: "main", Tool: "read_file", Text: "# tool **raw**"},
		agent.Event{Kind: agent.EvInfo, Text: "info **raw**"}).(Model)
	v := plainView(m)
	for _, want := range []string{"# tool **raw**", "info **raw**", "> **mine**"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks the raw markup %q:\n%s", want, v)
		}
	}
}

func TestMarkdownSecretReassembly(t *testing.T) {
	cases := []struct {
		name, answer string
		want         []string
	}{
		{"joined secret falls back", "joined **SEC**RET123 here", []string{"joined **SEC**RET123 here"}},
		{"redacted then rendered", "key SECRET123 and **bold**", []string{"key [redacted] and bold"}},
		{"code span joins", "SEC`RET`123", []string{"SEC`RET`123"}},
		{"strike joins", "SEC~~RET~~123", []string{"SEC~~RET~~123"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newSessionModel(t, answerServer(t, c.answer))
			m = tabTurn(t, m, "q")
			v := plainView(m)
			if strings.Contains(v, "SECRET123") {
				t.Fatalf("the secret reached the view:\n%s", v)
			}
			for _, want := range c.want {
				if !strings.Contains(v, want) {
					t.Errorf("view lacks %q:\n%s", want, v)
				}
			}
		})
	}
}

// mdCheckInline reports every case whose ANSI-stripped rendering differs
// from the wanted text.
func mdCheckInline(t *testing.T, cases map[string]string) {
	t.Helper()
	for src, want := range cases {
		if got := mdStrip(src); got != want {
			t.Errorf("renderMarkdown(%q) = %q, want %q", src, got, want)
		}
	}
}

func TestMarkdownInlineEmphasis(t *testing.T) {
	mdCheckInline(t, map[string]string{
		"*it* and _it2_":        "it and it2",
		"~~gone~~ text":         "gone text",
		"__b__ x":               "b x",
		"snake_case_name stays": "snake_case_name stays",
		"2 * 3 * 4":             "2 * 3 * 4",
		"**unclosed and *also":  "**unclosed and *also",
		"in*tra*word":           "intraword",
		"a ~single~ b":          "a ~single~ b",
		"a * x*":                "a * x*",
		"_x _":                  "_x _",
		"*a **b** c*":           "a b c",
		"file_name_here.go":     "file_name_here.go",
		"snake__double__name":   "snake__double__name",
		"~~a *b* c~~":           "a b c",
	})
	for _, src := range []string{"*\xff*", "[*](*)", "![", "[", "<", "<https://", "\\"} {
		_ = renderMarkdown(src, 80) // must not panic
	}
	markers := strings.NewReplacer("*", "", "_", "", "~", "")
	for _, src := range []string{"***x***", "a ***x*** b", "x *", "_", "~~", "a ~~~ b", "_*_*", "**a*b**c*"} {
		if got := mdStrip(src); markers.Replace(got) != markers.Replace(src) {
			t.Errorf("renderMarkdown(%q) = %q lost characters other than markers", src, got)
		}
	}
	withColorProfile(t, termenv.ANSI256)
	for _, src := range []string{"*it*", "_it_", "~~gone~~"} {
		if raw := renderMarkdown(src, 80); raw == ansi.Strip(raw) {
			t.Errorf("%q not styled: %q", src, raw)
		}
	}
}

func TestMarkdownInlineCode(t *testing.T) {
	mdCheckInline(t, map[string]string{
		"`a **b** [l](u)` code": "a **b** [l](u) code",
		"``a ` b`` two":         "a ` b two",
		"` x `":                 "x",
		"`  `":                  "  ",
		"`\\*x*`":               "\\*x*",
		"`_y_ <https://a.b>`":   "_y_ <https://a.b>",
		"`unclosed *it*":        "`unclosed it",
	})
	withColorProfile(t, termenv.ANSI256)
	raw := renderMarkdown("`code`", 80)
	if raw == ansi.Strip(raw) || ansi.Strip(raw) != "code" {
		t.Errorf("code span not styled: %q", raw)
	}
}

func TestMarkdownInlineLinks(t *testing.T) {
	mdCheckInline(t, map[string]string{
		"see [docs](https://x.io/a) now": "see docs (https://x.io/a) now",
		"[https://x.io](https://x.io)":   "https://x.io",
		"<https://x.io/p>":               "https://x.io/p",
		"![img](a.png)":                  "![img](a.png)",
		"[no link]":                      "[no link]",
		"[**b** x](u)":                   "b x (u)",
		"[a [b] c](u)":                   "a [b] c (u)",
		"[a](has space)":                 "[a](has space)",
		"[a]()":                          "[a]()",
		"[a] (u)":                        "[a] (u)",
		"<http://a b>":                   "<http://a b>",
		"<ftp://x>":                      "<ftp://x>",
		"a < b > c":                      "a < b > c",
		"![*x*](y) !":                    "![*x*](y) !",
	})
}

func TestMarkdownInlineEscapes(t *testing.T) {
	mdCheckInline(t, map[string]string{
		"\\*lit\\* and \\_u\\_": "*lit* and _u_",
		"C:\\path":              "C:\\path",
		"\\[a](b)":              "[a](b)",
		"\\`x`":                 "`x`",
		"a\\\\b":                "a\\b",
		"\\~~x~~":               "~~x~~",
		"*a \\* b*":             "a * b",
		"end\\":                 "end\\",
	})
}

// mdTableLines renders src at natural width and returns the ANSI-stripped
// lines with trailing spaces trimmed.
func mdTableLines(src string) []string {
	var out []string
	for _, l := range strings.Split(ansi.Strip(renderMarkdown(src, 0)), "\n") {
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

// mdCheckLines reports a mismatch between the rendered lines and want.
func mdCheckLines(t *testing.T, src string, want []string) {
	t.Helper()
	got := strings.Join(mdTableLines(src), "\n")
	if w := strings.Join(want, "\n"); got != w {
		t.Errorf("renderMarkdown(%q):\n%s\nwant:\n%s", src, got, w)
	}
}

func TestMarkdownTableBasic(t *testing.T) {
	src := "intro\n| Name | Qty | Mid |\n|:-----|----:|:---:|\n| apple | 3 | a |\n| kiwi | 12 | bbbbb |\noutro"
	mdCheckLines(t, src, []string{
		"intro",
		"┌───────┬─────┬───────┐",
		"│ Name  │ Qty │  Mid  │",
		"├───────┼─────┼───────┤",
		"│ apple │   3 │   a   │",
		"│ kiwi  │  12 │ bbbbb │",
		"└───────┴─────┴───────┘",
		"outro",
	})
	// A blank line ends the table; the end of the input does too.
	mdCheckLines(t, "a | b\n--|--\nc | d\n\nafter", []string{
		"┌───┬───┐",
		"│ a │ b │",
		"├───┼───┤",
		"│ c │ d │",
		"└───┴───┘",
		"",
		"after",
	})
	mdCheckLines(t, "| h |\n|---|", []string{
		"┌───┐",
		"│ h │",
		"├───┤",
		"└───┘",
	})
}

func TestMarkdownTableAlign(t *testing.T) {
	src := "| left | right | center | plain |\n|:---|---:|:---:|---|\n| a | b | c | d |\n| xx | yy | zz | ww |"
	mdCheckLines(t, src, []string{
		"┌──────┬───────┬────────┬───────┐",
		"│ left │ right │ center │ plain │",
		"├──────┼───────┼────────┼───────┤",
		"│ a    │     b │   c    │ d     │",
		"│ xx   │    yy │   zz   │ ww    │",
		"└──────┴───────┴────────┴───────┘",
	})
	// Alignment applies to the header cells as well.
	mdCheckLines(t, "| r | c |\n|--:|:-:|\n| long | wider |", []string{
		"┌──────┬───────┐",
		"│    r │   c   │",
		"├──────┼───────┤",
		"│ long │ wider │",
		"└──────┴───────┘",
	})
}

func TestMarkdownTableCells(t *testing.T) {
	mdCheckLines(t, "| A | B |\n|---|---|\n| a \\| b | `p|q` |\n| **b** |\n| 1 | 2 | 3 |", []string{
		"┌───────┬─────┐",
		"│ A     │ B   │",
		"├───────┼─────┤",
		"│ a | b │ p|q │",
		"│ b     │     │",
		"│ 1     │ 2   │",
		"└───────┴─────┘",
	})
	cases := map[string][]string{
		"a\\|b":      {`a|b`},
		"| a | b |":  {"a", "b"},
		"a | b":      {"a", "b"},
		"|a||b|":     {"a", "", "b"},
		"| `x|y` |":  {"`x|y`"},
		"| `x |":     {"`x"},
		"|":          {""},
		"||":         {""},
		"a \\| b |":  {"a | b"},
		"| ``a|`` |": {"``a|``"},
	}
	for src, want := range cases {
		got, _ := mdSplitRow(src)
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("mdSplitRow(%q) = %q, want %q", src, got, want)
		}
	}
}

func TestMarkdownTableNotATable(t *testing.T) {
	for _, src := range []string{
		"| a | b |\n| c | d |",
		"| a | b |",
		"| a | b |\n|---|---|---|",
		"| a | b |\n|---|",
		"| a | b |\n|---|-x-|",
		"| a | b |\n|---|- -|",
		"| a | b |\n|---|::|",
		"a | b\n--- | --- | ---",
		"a | b\n\n|---|---|",
		"no pipe\n|---|",
	} {
		if got := renderMarkdown(src, 80); got != src {
			t.Errorf("renderMarkdown(%q) = %q, want it unchanged", src, got)
		}
	}
	// A '|' inside a code span does not split, so this is no table either.
	if got := mdStrip("| a | `b |` |\n|---|---|---|"); got != "| a | b | |\n|---|---|---|" {
		t.Errorf("code span row rendered as %q", got)
	}
	// A lone "---" under a row is a horizontal rule, not a separator row.
	mdCheckLines(t, "a | b\n---", []string{"a | b", strings.Repeat("─", 40)})
}

func TestMarkdownTableHeaderBold(t *testing.T) {
	withColorProfile(t, termenv.ANSI256)
	lines := strings.Split(renderMarkdown("| Head | Er |\n|---|---|\n| x | y |", 0), "\n")
	if len(lines) != 5 {
		t.Fatalf("want 5 table lines, got %d: %q", len(lines), lines)
	}
	if lines[1] == ansi.Strip(lines[1]) {
		t.Errorf("header row not styled: %q", lines[1])
	}
	if lines[3] != ansi.Strip(lines[3]) {
		t.Errorf("body row styled: %q", lines[3])
	}
	if !strings.Contains(ansi.Strip(lines[1]), "│ Head │ Er │") {
		t.Errorf("header row = %q", ansi.Strip(lines[1]))
	}
}

// mdCheckFits reports a table line wider than width or of a different width
// than the first line; lines are ANSI-stripped.
func mdCheckFits(t *testing.T, out string, width int) []string {
	t.Helper()
	lines := strings.Split(ansi.Strip(out), "\n")
	w0 := ansi.StringWidth(lines[0])
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > width || w != w0 {
			t.Errorf("line width %d (first %d, limit %d): %q", w, w0, width, l)
		}
	}
	return lines
}

func TestMarkdownTableWrap(t *testing.T) {
	src := "| Column one | Column two | Column three |\n|---|---|---|\n| " +
		strings.TrimSpace(strings.Repeat("word ", 12)) + " | short | " + strings.Repeat("x", 50) + " |"
	out := ansi.Strip(renderMarkdown(src, 40))
	lines := mdCheckFits(t, out, 40)
	if !strings.HasPrefix(lines[0], "┌") || !strings.HasPrefix(lines[len(lines)-1], "└") {
		t.Fatalf("not a box:\n%s", out)
	}
	if n := strings.Count(out, "word"); n != 12 {
		t.Errorf("%d words, want 12:\n%s", n, out)
	}
	if n := strings.Count(out, "x"); n != 50 {
		t.Errorf("%d x characters, want 50:\n%s", n, out)
	}
	for _, w := range []string{"Column", "one", "two", "three", "short"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q:\n%s", w, out)
		}
	}
	// The header separator follows the last header line, not the first.
	if !strings.HasPrefix(lines[1], "│") || !strings.HasPrefix(lines[2], "│") {
		t.Errorf("wrapped header lines missing:\n%s", out)
	}
	if strings.Count(out, "├") != 1 {
		t.Errorf("want exactly one header separator:\n%s", out)
	}
}

func TestMarkdownTableWrapAlign(t *testing.T) {
	src := "| name | amount |\n|---|---:|\n| " + strings.Repeat("a", 30) + " | 1 22 333 4444 55555 |"
	out := ansi.Strip(renderMarkdown(src, 30))
	lines := mdCheckFits(t, out, 30)
	body := 0
	for _, l := range lines[3 : len(lines)-1] {
		cols := strings.Split(l, "│")
		if len(cols) != 4 {
			t.Fatalf("bad row %q", l)
		}
		cell := cols[2]
		if strings.TrimSpace(cell) == "" {
			continue
		}
		body++
		if !strings.HasSuffix(cell, " ") || strings.HasSuffix(cell, "  ") {
			t.Errorf("cell %q not right-aligned:\n%s", cell, out)
		}
	}
	if body < 2 {
		t.Errorf("right-aligned cell not wrapped (%d lines):\n%s", body, out)
	}
}

func TestMarkdownTableFitsUnchanged(t *testing.T) {
	src := "| Name | Qty |\n|:---|---:|\n| apple | 3 |\n| **kiwi** | 12 |"
	if a, b := renderMarkdown(src, 0), renderMarkdown(src, 80); a != b {
		t.Errorf("a fitting table differs at width 0 and 80:\n%s\n%s", a, b)
	}
	// Exactly fitting is still the natural layout.
	natural := renderMarkdown(src, 0)
	w := ansi.StringWidth(strings.Split(natural, "\n")[0])
	if got := renderMarkdown(src, w); got != natural {
		t.Errorf("a table exactly %d wide changed:\n%s", w, got)
	}
}

func TestMarkdownTableTooNarrow(t *testing.T) {
	src := "|a|b|c|d|e|f|g|h|i|j|\n|-|-|-|-|-|-|-|-|-|-|\n|1|2|3|4|5|6|7|8|9|0|"
	if got := renderMarkdown(src, 20); got != src {
		t.Errorf("renderMarkdown at width 20 = %q, want the source", got)
	}
	// Only the table falls back; the text around it is rendered as usual.
	got := renderMarkdown("**x**\n"+src+"\nafter", 20)
	if want := "x\n" + src + "\nafter"; ansi.Strip(got) != want {
		t.Errorf("got %q, want %q", ansi.Strip(got), want)
	}
}

func TestMarkdownTableWideChars(t *testing.T) {
	src := "| 名前 | 説明 |\n|---|---|\n| 日本語のテキスト | 😀😀😀 絵文字 😀😀😀 と 漢字 の 説明文 |"
	out := renderMarkdown(src, 30)
	lines := mdCheckFits(t, out, 30)
	if !strings.HasPrefix(lines[0], "┌") {
		t.Errorf("not a box:\n%s", ansi.Strip(out))
	}
	plain := strings.ReplaceAll(ansi.Strip(out), " ", "")
	for _, s := range []string{"名前", "説明", "😀😀😀", "漢字"} {
		if !strings.Contains(strings.ReplaceAll(strings.ReplaceAll(plain, "│", ""), "\n", ""), s) {
			t.Errorf("lost %q:\n%s", s, ansi.Strip(out))
		}
	}
}

func TestMarkdownTableWrapHeaderBold(t *testing.T) {
	withColorProfile(t, termenv.ANSI256)
	src := "| alpha beta gamma delta | b |\n|---|---|\n| x | y |"
	lines := strings.Split(renderMarkdown(src, 16), "\n")
	header := 0
	for _, l := range lines {
		if strings.HasPrefix(ansi.Strip(l), "├") {
			break
		}
		if strings.HasPrefix(ansi.Strip(l), "│") {
			header++
			cell := strings.Split(l, "│")[1]
			if !strings.Contains(cell, "\x1b[") || strings.Count(cell, "\x1b[0m") < 1 {
				t.Errorf("header line not bold on its own: %q", l)
			}
			if !strings.HasSuffix(strings.TrimRight(cell, " "), "m") {
				t.Errorf("header style not closed in its cell: %q", l)
			}
		}
	}
	if header < 2 {
		t.Errorf("header not wrapped: %q", lines)
	}
}

func TestMarkdownTableView(t *testing.T) {
	m, _ := newBlockModel().Update(tea.WindowSizeMsg{Width: 40, Height: 40})
	text := "Here:\n\n| Column one | Column two | Column three |\n|---|---:|---|\n| " +
		strings.TrimSpace(strings.Repeat("word ", 12)) + " | short | " + strings.Repeat("x", 50) + " |"
	m = feed(m, agent.Event{Kind: agent.EvText, AgentID: "main", Text: text})
	v := plainView(m.(Model))
	if !strings.Contains(v, "┌") || !strings.Contains(v, "└") {
		t.Fatalf("no table in the view:\n%s", v)
	}
	// The two status rows keep every field by design (README) and may be
	// wider than a narrow terminal; the view pads the other lines to them
	// with trailing spaces, so those are trimmed before measuring.
	lines := strings.Split(v, "\n")
	for _, l := range lines[:len(lines)-2] {
		if w := ansi.StringWidth(strings.TrimRight(l, " ")); w > 40 {
			t.Errorf("view line %d wide: %q", w, l)
		}
	}
}

// TestMarkdownTableShrinkOrder checks the shrinking against its definition:
// the widest column (the leftmost on a tie) loses one column at a time,
// never below min(3, natural width).
func TestMarkdownTableShrinkOrder(t *testing.T) {
	reference := func(widths []int, width int) []int {
		w := append([]int(nil), widths...)
		for mdTableWidth(w) > width {
			widest := 0
			for c := range w {
				if w[c] > w[widest] {
					widest = c
				}
			}
			w[widest]--
		}
		return w
	}
	seed := uint32(7)
	rnd := func(n int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>8) % n
	}
	for range 2000 {
		widths := make([]int, 1+rnd(6))
		for c := range widths {
			widths[c] = rnd(40)
		}
		width := rnd(mdTableWidth(widths) + 5)
		got := append([]int(nil), widths...)
		mins := make([]int, len(widths))
		for c, w := range widths {
			mins[c] = min(3, w)
		}
		fits := mdTableWidth(mins) <= width
		if ok := mdShrinkColumns(got, width); ok != fits {
			t.Fatalf("mdShrinkColumns(%v, %d) = %v, want %v", widths, width, ok, fits)
		}
		if !fits {
			continue
		}
		if want := reference(widths, width); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("mdShrinkColumns(%v, %d) = %v, want %v", widths, width, got, want)
		}
	}
}

func TestMarkdownListBullets(t *testing.T) {
	mdCheckLines(t, "intro\n- a **b**\n* `c **d**`\n+ e __f__\n-   wide\nafter",
		[]string{"intro", "• a b", "• c **d**", "• e f", "• wide", "after"})
	mdCheckLines(t, "- ", []string{"•"})
	withColorProfile(t, termenv.ANSI256)
	if raw := renderMarkdown("- **b**", 80); raw == ansi.Strip(raw) || ansi.Strip(raw) != "• b" {
		t.Errorf("bold inside an item not styled: %q", raw)
	}
}

func TestMarkdownListNested(t *testing.T) {
	mdCheckLines(t, "- one\n  more of one\n- two **b**\n  - nested\n    - deeper\n* star\n+ plus\n\n1. first\n2) second\n10. tenth",
		[]string{"• one", "  more of one", "• two b", "  • nested", "    • deeper", "• star", "• plus", "", "1. first", "2) second", "10. tenth"})
	mdCheckLines(t, "- a\n    - b\n        - c", []string{"• a", "  • b", "    • c"})
	mdCheckLines(t, "- a\n\t- b\n\t\t- c", []string{"• a", "  • b", "    • c"})
	mdCheckLines(t, "1. a\n   - b\n   - c\n2. d", []string{"1. a", "  • b", "  • c", "2. d"})
	mdCheckLines(t, "- a\n - b", []string{"• a", "• b"})
}

func TestMarkdownListNumbered(t *testing.T) {
	mdCheckLines(t, "1. first **b**\n2) second\n10. tenth\n123456789. big",
		[]string{"1. first b", "2) second", "10. tenth", "123456789. big"})
	mdCheckLines(t, "1234567890. too long", []string{"1234567890. too long"})
	mdCheckLines(t, "1.\n2)x\n3:  no", []string{"1.", "2)x", "3:  no"})
}

func TestMarkdownListContinuation(t *testing.T) {
	mdCheckLines(t, "- item\n  cont **b**\n\n  after blank\nnot cont",
		[]string{"• item", "  cont b", "", "  after blank", "not cont"})
	mdCheckLines(t, "10. x\n    more\n   less", []string{"10. x", "    more", "   less"})
	mdCheckLines(t, "- a\n  - b\n    more\n  back", []string{"• a", "  • b", "    more", "  back"})
	mdCheckLines(t, "- a\n    - b\n      more", []string{"• a", "  • b", "    more"})
	mdCheckLines(t, "- a\n b", []string{"• a", " b"})
	mdCheckLines(t, "- a\n\n\npara", []string{"• a", "", "", "para"})
	mdCheckLines(t, "- a\n\n- b", []string{"• a", "", "• b"})
	out := mdTableLines("1. Run:\n   ```sh\n   make **x**\n   ```\n2. Done")
	if got := strings.Join(out, "\n"); strings.Contains(got, "```") || !strings.Contains(got, "make **x**") ||
		out[0] != "1. Run:" || out[len(out)-1] != "2. Done" {
		t.Errorf("fence inside a list item not highlighted:\n%s", got)
	}
}

func TestMarkdownListNotItems(t *testing.T) {
	for src, want := range map[string]string{
		"-no space":      "-no space",
		"1.5 apples":     "1.5 apples",
		"+1 vote":        "+1 vote",
		"**bold** start": "bold start",
		"---":            strings.Repeat("─", 80),
		"***":            strings.Repeat("─", 80),
		"___":            strings.Repeat("─", 80),
		"a\n-no space":   "a\n-no space",
	} {
		if got := mdStrip(src); got != want {
			t.Errorf("renderMarkdown(%q) = %q, want %q", src, got, want)
		}
	}
}

// mdLinesAt renders src at width and returns the ANSI-stripped lines with
// trailing spaces trimmed.
func mdLinesAt(src string, width int) []string {
	var out []string
	for _, l := range strings.Split(ansi.Strip(renderMarkdown(src, width)), "\n") {
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

// mdCheckLinesAt reports a mismatch between the lines rendered at width and want.
func mdCheckLinesAt(t *testing.T, src string, width int, want []string) {
	t.Helper()
	got := strings.Join(mdLinesAt(src, width), "\n")
	if w := strings.Join(want, "\n"); got != w {
		t.Errorf("renderMarkdown(%q, %d):\n%s\nwant:\n%s", src, width, got, w)
	}
}

func TestMarkdownQuote(t *testing.T) {
	rule := strings.Repeat("─", 30)
	mdCheckLinesAt(t, "> quoted **x**\n> > nested\n> back\n\n---\n***\n___\n--", 30,
		[]string{"│ quoted x", "│ │ nested", "│ back", "", rule, rule, rule, "--"})
	mdCheckLinesAt(t, "> - item", 30, []string{"│ • item"})
	mdCheckLinesAt(t, "   >no space\n    > code-like", 30, []string{"│ no space", "    > code-like"})
	mdCheckLinesAt(t, "> a\nlazy", 30, []string{"│ a", "lazy"})
	mdCheckLinesAt(t, "> ---", 30, []string{"│ " + strings.Repeat("─", 28)})
	if got := mdStrip("> a\n> > b"); strings.Contains(got, ">") {
		t.Errorf("quote marker shown: %q", got)
	}
}

func TestMarkdownQuoteInner(t *testing.T) {
	mdCheckLinesAt(t, "> # Title **b**\n>\n> body **bold**\n> | a | b |\n> |---|---|\n> | 1 | 2 |", 30,
		[]string{"│ Title b", "│", "│ body bold", "│ ┌───┬───┐", "│ │ a │ b │", "│ ├───┼───┤", "│ │ 1 │ 2 │", "│ └───┴───┘"})
	withColorProfile(t, termenv.ANSI256)
	if raw := renderMarkdown("> **b**", 30); raw == ansi.Strip(raw) || ansi.Strip(raw) != "│ b" {
		t.Errorf("bold inside a quote not styled: %q", raw)
	}
}

func TestMarkdownQuoteDeep(t *testing.T) {
	deep := strings.Repeat(">", 5000) + " deep text"
	for _, w := range []int{80, 0} {
		out := mdLinesAt(deep, w)
		if len(out) != 1 || !strings.Contains(out[0], "deep text") {
			t.Fatalf("deep quote at width %d lost its text: %q", w, out)
		}
		if bars := strings.Count(out[0], "│"); bars != mdMaxQuoteDepth {
			t.Errorf("deep quote at width %d has %d bars, want %d", w, bars, mdMaxQuoteDepth)
		}
	}
	spaced := strings.Repeat("> ", 3000) + "x\n" + strings.Repeat("> ", 3000) + "y"
	if out := mdLinesAt(spaced, 80); len(out) != 2 || !strings.HasSuffix(out[0], "x") || !strings.HasSuffix(out[1], "y") {
		t.Errorf("spaced deep quote = %q", out)
	}
}

func TestMarkdownRule(t *testing.T) {
	rule := strings.Repeat("─", 30)
	for _, src := range []string{"---", "***", "___", "-----", "  ***  ", "______"} {
		mdCheckLinesAt(t, src, 30, []string{rule})
	}
	mdCheckLinesAt(t, "para\n---\nafter", 30, []string{"para", rule, "after"})
	for _, w := range []int{0, -1} {
		mdCheckLinesAt(t, "---", w, []string{strings.Repeat("─", 40)})
	}
}

func TestMarkdownRuleNotRule(t *testing.T) {
	for _, src := range []string{"--", "a --- b", "-- -", "-*-", "**"} {
		mdCheckLinesAt(t, src, 30, []string{src})
	}
}

func TestMarkdownIncomplete(t *testing.T) {
	for _, src := range []string{
		"| a | b |",
		"| a | b |\n| c",
		"| a | b |\n|",
		"| a | b |\n|-",
		"| a | b |\n|---|",
		"some **unclosed",
		"some __unclosed",
		"an `unclosed code",
		"an ``unclosed ` code",
		"a [link](http://x",
		"a <http://x",
		"```",
		"```go",
		"```go\n| x | y |\n|---|---|\n# not\n**b**",
		"text\n```\n> q\n- i\n`x`",
		"```nosuch\n**b** _i_ ~~s~~ [l](u)",
	} {
		if got := mdStrip(src); got != src {
			t.Errorf("incomplete %q rendered as %q", src, got)
		}
	}
	done := mdStrip("| a | b |\n|---|---|\n| c | d |")
	if !strings.Contains(done, "┌") || strings.Contains(done, "|---") {
		t.Errorf("finished table not rendered: %q", done)
	}
	if got := mdStrip("some **closed**"); got != "some closed" {
		t.Errorf("finished bold = %q", got)
	}
	if got := mdStrip("```go\nx := 1\n```"); got != "x := 1" {
		t.Errorf("finished fence = %q", got)
	}
}

func TestMarkdownFenceUntouched(t *testing.T) {
	body := []string{"|---|", "| x |", "# h", "**b**", "> q", "- i", "| a | b |\n|---|---|\n| c | d |"}
	for _, lang := range []string{"", "go", "nosuch", "text"} {
		for _, b := range body {
			src := "before\n```" + lang + "\n" + b + "\n```\nafter"
			got := mdStrip(src)
			if want := "before\n" + b + "\nafter"; got != want {
				t.Errorf("fence %q: got %q, want %q", src, got, want)
			}
		}
	}
	// Inline code content is never interpreted.
	for _, code := range []string{"**b**", "# x | y |", "[a](b)", "_i_", "~~s~~", `\*`, "<http://x>"} {
		if got := mdStrip("a `" + code + "` b"); got != "a "+code+" b" {
			t.Errorf("inline code %q rendered as %q", code, got)
		}
	}
	// Inline code in a table cell is not split or interpreted.
	tbl := mdStrip("| h |\n|---|\n| `a | **b**` |")
	if !strings.Contains(tbl, "a | **b**") {
		t.Errorf("table code cell interpreted: %q", tbl)
	}
}

func TestMarkdownNoWidth(t *testing.T) {
	src := "| a | b |\n|---|---|\n| " + strings.Repeat("x", 120) + " | y |\n\n---"
	for _, w := range []int{0, -1} {
		lines := strings.Split(ansi.Strip(renderMarkdown(src, w)), "\n")
		if len(lines) != 7 {
			t.Fatalf("width %d: lines %q", w, lines)
		}
		if got := ansi.StringWidth(lines[0]); got != 120+3*2+1+1 {
			t.Errorf("width %d: table not at natural width (%d): %q", w, got, lines[0])
		}
		if lines[6] != strings.Repeat("─", 40) {
			t.Errorf("width %d: rule %q", w, lines[6])
		}
	}
}

// mdRobustInputs are the inputs every width of TestMarkdownNoPanic renders.
func mdRobustInputs() []string {
	return []string{
		"", "|", "||", "|-|", "| a |\n|-|-|-|", "**", "*", "_", "~~", "`", "``", "```", "[", "](", "[a](",
		"<http", "> ", "- ", "1.", "#", "######", "\x00", "\x00\xff\xfe", "a\xffb **\xfe** `\xff`",
		"| 漢字 | 😀 |\n|---|---|\n| 😀😀😀😀😀😀😀😀😀😀 | x |",
		"| 👩‍👩‍👧 | é |\n|---|---|\n| 中文中文中文 | 🇩🇪🇩🇪 |",
		strings.Repeat(">", 5000) + " deep",
		strings.Repeat("> ", 5000) + "deep **b**",
		strings.Repeat("**a", 2000),
		strings.Repeat("`", 3000),
		strings.Repeat("` ``", 1500),
		strings.Repeat("[", 3000) + strings.Repeat("]", 10),
		strings.Repeat("[a](", 2000),
		strings.Repeat("*_~~", 1000) + "x" + strings.Repeat("~~_*", 1000),
		strings.Repeat("- ", 2000) + "x",
		strings.Repeat("  ", 2000) + "- x",
		strings.Repeat("| x ", 300) + "|\n" + strings.Repeat("|-", 300) + "|\n" + strings.Repeat("| y ", 300) + "|",
		"| a |\n|---|\n" + strings.Repeat("| b |\n", 2000),
		"\x1b[31mred\x1b[0m | \x1b[\n|---|---|\n| \x1b[1m | x |",
	}
}

func TestMarkdownNoPanic(t *testing.T) {
	for _, src := range mdRobustInputs() {
		for _, w := range []int{-1, 0, 1, 2, 5, 40} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("panic for %.60q at width %d: %v", src, w, r)
					}
				}()
				if a, b := renderMarkdown(src, w), renderMarkdown(src, w); a != b {
					t.Errorf("not deterministic for %.60q at width %d", src, w)
				}
			}()
		}
	}
}

func TestMarkdownLargeInput(t *testing.T) {
	unit := "# Title **b**\n\n| a **b** `c` | [d](e) |\n|---|---|\n| x | y |\n\n- item _i_\n  - sub *s*\n1. one ~~z~~\n" +
		"> q ~~z~~ <https://x.y>\n> > deeper `code`\n\n---\n\n```go\nx := 1 // **no**\n```\npara \\* text [l](u) ` `` `\n\n"
	big := strings.Repeat(unit, 100*1024/len(unit)+1)
	for _, w := range []int{80, 0} {
		start := time.Now()
		out := renderMarkdown(big, w)
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("%d bytes at width %d took %v", len(big), w, d)
		}
		if !strings.Contains(out, "┌") {
			t.Errorf("width %d: no table rendered", w)
		}
	}
	// Pathological lines stay fast too: no rescans per character.
	for _, src := range []string{
		strings.Repeat("`", 50000),
		strings.Repeat("` ``", 12000),
		strings.Repeat("[", 50000),
		strings.Repeat("[a](", 12000),
		strings.Repeat("**a", 16000),
		strings.Repeat("_a ", 16000),
		strings.Repeat("~~a ", 12000),
		strings.Repeat("<http://", 6000),
		strings.Repeat("| `", 16000),
		strings.Repeat("[", 10000) + "a" + strings.Repeat("](u)", 10000),
		strings.Repeat("**[a _", 5000) + "x" + strings.Repeat("_](u)**", 5000),
	} {
		start := time.Now()
		_ = renderMarkdown(src, 80)
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("%.20q (%d bytes) took %v", src, len(src), d)
		}
	}
}

func TestMarkdownStreamingView(t *testing.T) {
	const width = 40
	m, _ := newBlockModel().Update(tea.WindowSizeMsg{Width: width, Height: 40})
	answer := "Result:\n\n| Name | Value | Note |\n|---|---:|:---:|\n| alpha | 1 | **bold** text here |\n" +
		"| beta | 22 | `code | pipe` |\n| gamma | 333 | " + strings.Repeat("long ", 10) + "|\n\nDone."
	for i := 0; i < len(answer); i += 3 {
		delta := answer[i:min(i+3, len(answer))]
		m = feed(m, agent.Event{Kind: agent.EvText, AgentID: "main", Text: delta})
		v := plainView(m.(Model))
		// The two status rows keep every field by design and may be wider
		// than a narrow terminal (see TestMarkdownTableView).
		lines := strings.Split(v, "\n")
		for _, l := range lines[:len(lines)-2] {
			if w := ansi.StringWidth(strings.TrimRight(l, " ")); w > width {
				t.Fatalf("after %q: view line %d wide: %q", answer[:i+len(delta)], w, l)
			}
		}
	}
	v := plainView(m.(Model))
	if !strings.Contains(v, "┌") || !strings.Contains(v, "└") || strings.Contains(v, "|---") {
		t.Fatalf("final view has no table:\n%s", v)
	}
}

func FuzzMarkdown(f *testing.F) {
	seeds := []string{
		"# Title\n\nSome **bold**, _italic_, ~~strike~~ and `code`.",
		"| a | b |\n|---|:---:|\n| c | d |",
		"- one\n  - two\n1. three\n\n> quote **b**\n\n---",
		"```go\nfunc main() {}\n```\n\n[link](http://x) <https://y> \\*",
		"| a | b |\n| c",
		"```go\n| x |\n# h",
		"| 漢字 | 😀 |\n|---|---|\n| 😀😀😀 | x |",
		strings.Repeat(">", 200) + " deep",
		strings.Repeat("**a", 200),
		strings.Repeat("| x ", 60) + "|\n" + strings.Repeat("|-", 60) + "|\n" + strings.Repeat("| y ", 60) + "|",
	}
	for _, s := range seeds {
		f.Add(s, 40)
	}
	for _, s := range []string{"", "|", "||", "|-|", "**", "*", "_", "~~", "`", "```", "[", "](", "[a](",
		"<http", "> ", "- ", "1.", "#", "######", "\x00\xff\xfe"} {
		f.Add(s, 5)
	}
	f.Add("| a |\n|---|\n| b |", -1)
	f.Add("---", 0)
	f.Fuzz(func(t *testing.T, src string, width int) {
		if len(src) > 1<<16 {
			return
		}
		width %= 400
		a := renderMarkdown(src, width)
		if b := renderMarkdown(src, width); a != b {
			t.Fatalf("not deterministic for %q at width %d", src, width)
		}
		if utf8.ValidString(src) && !utf8.ValidString(a) {
			t.Fatalf("invalid UTF-8 output for %q at width %d: %q", src, width, a)
		}
	})
}
