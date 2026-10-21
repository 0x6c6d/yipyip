package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
	for _, want := range []string{"read_file", "a.txt", "hello", `{"path":"a.txt"}`, "╭"} {
		if !strings.Contains(v, want) {
			t.Fatalf("View() missing %q:\n%s", want, v)
		}
	}
	m = feed(m, agent.Event{Kind: agent.EvToolCall, AgentID: "sub1", Tool: "glob", Args: `{"pattern":"*"}`})
	if v := m.View(); !strings.Contains(v, "[sub1] glob") {
		t.Fatalf("subagent prefix missing:\n%s", v)
	}
}

func TestToolResultCollapsed(t *testing.T) {
	m := feed(newBlockModel(), toolCallEv("bash", `{"command":"seq"}`), toolResultEv("bash", tenLines()))
	v := m.View()
	for _, want := range []string{"line 1", "line 2", "line 3", "7 more lines", "ctrl+o to expand"} {
		if !strings.Contains(v, want) {
			t.Fatalf("View() missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "line 4") {
		t.Fatalf("collapsed result shows line 4:\n%s", v)
	}
	m = feed(newBlockModel(), toolCallEv("bash", `{}`), toolResultEv("bash", "a\nb\nc"))
	if strings.Contains(m.View(), "more lines") {
		t.Fatal("3-line result must show no hint")
	}
}

func TestToolResultExpand(t *testing.T) {
	m := feed(newBlockModel(), toolCallEv("bash", `{}`), toolResultEv("bash", tenLines()))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if v := m.View(); !strings.Contains(v, "line 10") || strings.Contains(v, "more lines") {
		t.Fatalf("expanded view wrong:\n%s", v)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if v := m.View(); strings.Contains(v, "line 10") || !strings.Contains(v, "7 more lines") {
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
	if strings.Count(v, "╭") != 3 || strings.Index(v, "RES-ONE") > strings.Index(v, `"two"`) {
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
