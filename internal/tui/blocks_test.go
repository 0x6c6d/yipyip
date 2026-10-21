package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"yipyip/internal/agent"
)

func feed(m tea.Model, evs ...agent.Event) tea.Model {
	for _, ev := range evs {
		m, _ = m.Update(EventMsg{Event: ev})
	}
	return m
}

func toolCallEv(tool, args string) agent.Event {
	return agent.Event{Kind: agent.EvToolCall, AgentID: "main", Tool: tool, Args: args}
}

func toolResultEv(tool, text string) agent.Event {
	return agent.Event{Kind: agent.EvToolResult, AgentID: "main", Tool: tool, Text: text}
}

func tenLines() string {
	var ls []string
	for i := 1; i <= 10; i++ {
		ls = append(ls, fmt.Sprintf("line %d", i))
	}
	return strings.Join(ls, "\n")
}

func newBlockModel() tea.Model {
	var m tea.Model = New(Options{StartDir: "/work"})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return m
}

func TestToolBlockRendered(t *testing.T) {
	m := feed(newBlockModel(),
		toolCallEv("read_file", `{"path": "a.txt"}`),
		toolResultEv("read_file", "hello"))
	v := m.View()
	for _, want := range []string{"read_file", "a.txt", "hello", "● read_file(a.txt)"} {
		if !strings.Contains(v, want) {
			t.Fatalf("View() missing %q:\n%s", want, v)
		}
	}
	m = feed(m, agent.Event{Kind: agent.EvToolCall, AgentID: "sub1", Tool: "glob", Args: `{"pattern":"*"}`})
	if v := m.View(); !strings.Contains(v, "● [sub1] glob(*)") {
		t.Fatalf("subagent prefix missing:\n%s", v)
	}
}

func TestToolResultCollapsed(t *testing.T) {
	m := feed(newBlockModel(), toolCallEv("bash", `{"command":"seq"}`), toolResultEv("bash", tenLines()))
	v := m.View()
	for _, want := range []string{"line 1", "line 2", "line 3", "… +7 lines (ctrl+o to expand)"} {
		if !strings.Contains(v, want) {
			t.Fatalf("View() missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "line 4") {
		t.Fatalf("collapsed result shows line 4:\n%s", v)
	}
	m = feed(newBlockModel(), toolCallEv("bash", `{}`), toolResultEv("bash", "a\nb\nc"))
	if strings.Contains(m.View(), "ctrl+o to expand") {
		t.Fatal("3-line result must show no hint")
	}
}

func TestToolResultExpand(t *testing.T) {
	m := feed(newBlockModel(), toolCallEv("bash", `{}`), toolResultEv("bash", tenLines()))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if v := m.View(); !strings.Contains(v, "line 10") || strings.Contains(v, "ctrl+o to expand") {
		t.Fatalf("expanded view wrong:\n%s", v)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if v := m.View(); strings.Contains(v, "line 10") || !strings.Contains(v, "+7 lines (ctrl+o to expand)") {
		t.Fatalf("collapsed again view wrong:\n%s", v)
	}
}

func TestToolResultsMatchByOrder(t *testing.T) {
	m := feed(newBlockModel(),
		toolCallEv("read_file", `{"path":"one"}`),
		toolCallEv("read_file", `{"path":"two"}`),
		toolResultEv("read_file", "RES-ONE"),
		toolResultEv("read_file", "RES-TWO"))
	v := m.View()
	if strings.Count(v, "●") != 2 || strings.Index(v, "RES-ONE") > strings.Index(v, "read_file(two)") {
		t.Fatalf("results not matched to their blocks:\n%s", v)
	}
}

func TestHighlightGo(t *testing.T) {
	out := Highlight("func main() {}", "go")
	if !strings.Contains(out, "\x1b[") || !strings.Contains(out, "main") {
		t.Fatalf("Highlight = %q", out)
	}
}

func TestHighlightUnknownLang(t *testing.T) {
	const code = "some code\nmore"
	for _, lang := range []string{"nosuchlang-xyz", ""} {
		if got := Highlight(code, lang); got != code {
			t.Fatalf("Highlight(%q) = %q, want unchanged", lang, got)
		}
	}
}

func TestFenceHighlightedInView(t *testing.T) {
	text := func(s string) agent.Event { return agent.Event{Kind: agent.EvText, AgentID: "main", Text: s} }
	m := feed(newBlockModel(), text("before\n```go\nfunc main() {}\n"))
	if v := m.View(); !strings.Contains(v, "func main() {}") {
		t.Fatalf("unclosed fence not shown plain:\n%s", v)
	}
	m = feed(m, text("```\nafter"))
	v := m.View()
	if !strings.Contains(v, "\x1b[") || strings.Contains(v, "func main() {}") {
		t.Fatalf("closed fence not highlighted:\n%q", v)
	}
	for _, want := range []string{"before", "after", "main"} {
		if !strings.Contains(v, want) {
			t.Fatalf("View() missing %q", want)
		}
	}
	m = feed(newBlockModel(), text("```nosuch\nx := 1\n```"))
	if v := m.View(); !strings.Contains(v, "x := 1") {
		t.Fatalf("unknown language fence not plain:\n%s", v)
	}
}

func TestPageKeysScrollViewport(t *testing.T) {
	m := newBlockModel()
	for i := 0; i < 80; i++ {
		m = feed(m, agent.Event{Kind: agent.EvInfo, Text: fmt.Sprintf("info %d", i)})
	}
	if !m.(Model).viewport.AtBottom() {
		t.Fatal("viewport does not follow the bottom")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.(Model).viewport.AtBottom() {
		t.Fatal("PgUp did not scroll")
	}
	m = feed(m, agent.Event{Kind: agent.EvInfo, Text: "late"})
	if m.(Model).viewport.AtBottom() {
		t.Fatal("new content yanked a scrolled-up viewport to the bottom")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if !m.(Model).viewport.AtBottom() {
		t.Fatal("PgDn did not return to the bottom")
	}
}

func plainLines(m tea.Model) []string {
	var out []string
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

func hasLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func TestRenderToolCallLine(t *testing.T) {
	m := feed(newBlockModel(), toolCallEv("bash", `{"command":"ls"}`), toolResultEv("bash", "a.txt\nb.txt"))
	ls := plainLines(m)
	for _, want := range []string{"● bash(ls)", "  ⎿  a.txt", "     b.txt"} {
		if !hasLine(ls, want) {
			t.Fatalf("missing line %q:\n%s", want, strings.Join(ls, "\n"))
		}
	}
}

func TestRenderToolSummaries(t *testing.T) {
	cases := []struct{ tool, args, want string }{
		{"read_file", `{"path":"a.txt"}`, "● read_file(a.txt)"},
		{"glob", `{"pattern":"**/*.go"}`, "● glob(**/*.go)"},
		{"spawn_agent", `{"task":"do x"}`, "● spawn_agent(do x)"},
		{"other", `{"a":1}`, `● other({"a":1})`},
		{"bash", `{"x":1}`, `● bash({"x":1})`},
		{"bash", `not json`, `● bash(not json)`},
		{"bash", `{"command":"a\nb"}`, `● bash(a b)`},
	}
	for _, c := range cases {
		m := feed(newBlockModel(), toolCallEv(c.tool, c.args))
		if ls := plainLines(m); !hasLine(ls, c.want) {
			t.Fatalf("missing line %q:\n%s", c.want, strings.Join(ls, "\n"))
		}
	}
	m := feed(newBlockModel(), agent.Event{Kind: agent.EvToolCall, AgentID: "a1", Tool: "bash", Args: `{"command":"ls"}`})
	if ls := plainLines(m); !hasLine(ls, "● [a1] bash(ls)") {
		t.Fatalf("missing subagent line:\n%s", strings.Join(ls, "\n"))
	}
	long := strings.Repeat("x", 300)
	m = feed(newBlockModel(), toolCallEv("bash", `{"command":"`+long+`"}`))
	if ls := plainLines(m); !strings.Contains(strings.Join(ls, ""), strings.Repeat("x", 199)+"…)") {
		t.Fatalf("long summary not cut:\n%s", strings.Join(ls, "\n"))
	}
}

func TestRenderToolCollapsed(t *testing.T) {
	m := feed(newBlockModel(), toolCallEv("bash", `{"command":"seq"}`), toolResultEv("bash", "1\n2\n3\n4\n5\n6\n7"))
	ls := plainLines(m)
	for _, want := range []string{"  ⎿  1", "     2", "     3", "     … +4 lines (ctrl+o to expand)"} {
		if !hasLine(ls, want) {
			t.Fatalf("missing line %q:\n%s", want, strings.Join(ls, "\n"))
		}
	}
	if hasLine(ls, "     4") {
		t.Fatal("collapsed result shows line 4")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	ls = plainLines(m)
	for _, want := range []string{"     4", "     5", "     6", "     7"} {
		if !hasLine(ls, want) {
			t.Fatalf("expanded: missing line %q:\n%s", want, strings.Join(ls, "\n"))
		}
	}
	if strings.Contains(strings.Join(ls, "\n"), "ctrl+o to expand") {
		t.Fatal("expanded view still shows the hint")
	}
}

func TestRenderToolDotColors(t *testing.T) {
	withColorProfile(t, termenv.ANSI256)
	header := func(m tea.Model) string {
		for _, l := range strings.Split(m.View(), "\n") {
			if strings.Contains(l, "bash") {
				return l
			}
		}
		return ""
	}
	m := feed(newBlockModel(), toolCallEv("bash", `{"command":"ls"}`))
	if h := header(m); !strings.Contains(h, "38;5;244") {
		t.Fatalf("pending bullet not dim: %q", h)
	}
	ok := feed(m, toolResultEv("bash", "fine"))
	if h := header(ok); !strings.Contains(h, "32m") {
		t.Fatalf("success bullet not green: %q", h)
	}
	bad := feed(newBlockModel(), toolCallEv("bash", `{"command":"ls"}`),
		agent.Event{Kind: agent.EvToolResult, AgentID: "main", Tool: "bash", Text: "boom", IsError: true})
	if h := header(bad); !strings.Contains(h, "31m") {
		t.Fatalf("error bullet not red: %q", h)
	}
}

func TestRenderToolNoBorder(t *testing.T) {
	m := feed(newBlockModel(), toolCallEv("bash", `{"command":"ls"}`), toolResultEv("bash", tenLines()))
	v := m.(Model).viewport.View() // the input box has its own border
	for _, b := range []string{"╭", "│", "╰", "╮", "╯"} {
		if strings.Contains(v, b) {
			t.Fatalf("tool block contains border %q:\n%s", b, v)
		}
	}
}
