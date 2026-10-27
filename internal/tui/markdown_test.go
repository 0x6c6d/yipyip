package tui

import (
	"strings"
	"testing"

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
		"`**x**`":                "`**x**`",
		"a `**x**` **y**":        "a `**x**` y",
		"**a `**` b**":           "a `**` b",
		"``code ` **x**`` **y**": "``code ` **x**`` y",
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
	if raw := renderMarkdown("`**x**`", 80); raw != "`**x**`" {
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
		"1. one\n- two\n> quote\n---",
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
