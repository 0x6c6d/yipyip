package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"yipyip/internal/agent"
)

func renderModel() Model { return newBlockModel().(Model) }

func vpLines(m Model) []string { return strings.Split(m.viewport.View(), "\n") }

func TestRenderUserBlock(t *testing.T) {
	withColorProfile(t, termenv.ANSI256)
	m, _ := renderModel().Command("hello")
	var found string
	for _, l := range vpLines(m) {
		if strings.TrimSpace(ansi.Strip(l)) == "> hello" {
			found = l
		}
	}
	if found == "" {
		t.Fatalf("no plain line %q in viewport:\n%s", "> hello", ansi.Strip(m.viewport.View()))
	}
	if !strings.Contains(found, "48;5;236") {
		t.Errorf("user line lacks the shaded background: %q", found)
	}
	if w := lipgloss.Width(found); w != m.viewport.Width {
		t.Errorf("user line width = %d, want %d", w, m.viewport.Width)
	}
	m, _ = m.Command("a\nb")
	pl := strings.Split(ansi.Strip(m.viewport.View()), "\n")
	for i, l := range pl {
		pl[i] = strings.TrimRight(l, " ")
	}
	plain := strings.Join(pl, "\n")
	if !strings.Contains(plain, "> a\n  b") {
		t.Errorf("multi-line message not indented:\n%s", plain)
	}
}

func TestRenderAssistantBlock(t *testing.T) {
	m := feed(newBlockModel(), agent.Event{Kind: agent.EvText, AgentID: "main", Text: "first\nsecond"}).(Model)
	lines := strings.Split(ansi.Strip(m.viewport.View()), "\n")
	var first, second bool
	for _, l := range lines {
		l = strings.TrimRight(l, " ")
		first = first || l == "● first"
		second = second || l == "  second"
	}
	if !first || !second {
		t.Fatalf("want lines %q and %q:\n%q", "● first", "  second", lines)
	}
}

func TestRenderErrorBlock(t *testing.T) {
	withColorProfile(t, termenv.ANSI256)
	m := feed(newBlockModel(), agent.Event{Kind: agent.EvError, Text: "boom"}).(Model)
	v := m.viewport.View()
	if !strings.Contains(v, "31m") {
		t.Errorf("error not red: %q", v)
	}
	if !strings.Contains(ansi.Strip(v), "boom") {
		t.Errorf("error text missing: %q", v)
	}
	if strings.ContainsAny(ansi.Strip(v), "╭│╰╮╯") {
		t.Errorf("error block has border characters:\n%s", ansi.Strip(v))
	}
}

func TestRenderBlankLineBetweenBlocks(t *testing.T) {
	m, _ := renderModel().Command("question")
	m = feed(m,
		agent.Event{Kind: agent.EvText, AgentID: "main", Text: "answer"},
		agent.Event{Kind: agent.EvInfo, Text: "note"}).(Model)
	lines := strings.Split(ansi.Strip(m.viewport.View()), "\n")
	var got []string
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	// Trim the empty tail of the viewport.
	end := len(lines)
	for end > 0 && lines[end-1] == "" {
		end--
	}
	got = lines[:end]
	want := []string{"> question", "", "no model configured", "", "● answer", "", "note"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines = %q, want %q", got, want)
	}
}

func TestRenderNoBordersOutsideInput(t *testing.T) {
	m, _ := renderModel().Command("hi")
	m = feed(m,
		agent.Event{Kind: agent.EvText, AgentID: "main", Text: "reply"},
		toolCallEv("bash", `{"command":"ls"}`),
		toolResultEv("bash", "file"),
		agent.Event{Kind: agent.EvError, Text: "bad"},
		agent.Event{Kind: agent.EvInfo, Text: "fyi"}).(Model)
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	top := -1
	for i, l := range lines {
		if strings.Contains(l, "╭") {
			top = i
			break
		}
	}
	if top < 0 {
		t.Fatalf("input box top border not found:\n%s", strings.Join(lines, "\n"))
	}
	for _, l := range lines[:top] {
		if strings.ContainsAny(l, "╭│╰╮╯") {
			t.Fatalf("border character above the input box: %q", l)
		}
	}
}
