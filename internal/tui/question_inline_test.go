package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/agent"
)

// plainContent returns the transcript lines without styling.
func plainContent(m Model) string { return ansi.Strip(strings.Join(m.lines, "\n")) }

// lineIndex returns the index of the first line of s containing sub, or -1.
func lineIndex(s, sub string) int {
	for i, l := range strings.Split(s, "\n") {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

func TestQuestionInline(t *testing.T) {
	m, srv, sess := askQuestion(t, "Which one?", []string{"a", "b"}, false)
	v := plain(m)
	ask, title := lineIndex(v, "ask me"), lineIndex(v, questionTitle)
	if ask < 0 || title < 0 || ask > title {
		t.Fatalf("user message not above the question box:\n%s", v)
	}
	content := plainContent(m)
	if !strings.Contains(content, questionFreeRow) || !strings.Contains(content, "Which one?") {
		t.Fatalf("question box not in the transcript lines:\n%s", content)
	}
	// No overlay: every viewport row above the box is a transcript line.
	rows := strings.Split(v, "\n")
	for _, l := range rows[1:title] {
		if l = strings.TrimRight(l, " "); l != "" && !strings.Contains(content, l) {
			t.Errorf("view row %q above the box is not conversation:\n%s", l, v)
		}
	}

	m = press(m, tea.KeyDown, tea.KeySpace, tea.KeyEnter)
	if got := questionToolResult(t, srv, sess); got != "b" {
		t.Fatalf("tool result = %q, want b", got)
	}
	m = m.Drain()
	v = plain(m)
	if !strings.Contains(v, "Q: Which one? → A: b") || strings.Contains(v, questionFreeRow) {
		t.Fatalf("answer line missing or box still shown:\n%s", v)
	}

	m.apply(agent.Event{Kind: agent.EvQuestion, Question: &agent.QuestionRequest{
		Question: "Second\nquestion?",
		Options:  []string{"x"},
	}})
	if !strings.Contains(plain(m), questionFreeRow) {
		t.Fatalf("second question not shown:\n%s", plain(m))
	}
	m = press(m, tea.KeyEsc)
	v = plain(m)
	if !strings.Contains(v, "Q: Second question? → A: cancelled") || strings.Contains(v, questionFreeRow) {
		t.Fatalf("cancelled answer line missing or box still shown:\n%s", v)
	}
}

func TestQuestionInlineScrolls(t *testing.T) {
	m, _, _ := askQuestion(t, "Which one?", []string{"a", "b"}, false)
	long := make([]block, 0, 40)
	for i := 0; i < 40; i++ {
		long = append(long, block{kind: blockInfo, text: "earlier line"})
	}
	m.blocks = append(long, m.blocks...)
	m.refreshTo(true)
	v := plain(m)
	hint := lineIndex(v, questionHint)
	if hint < 0 || !strings.Contains(v, questionFreeRow) {
		t.Fatalf("question box not in the view:\n%s", v)
	}
	// Between the hint and the input box only the box's bottom border and the
	// Thinking line may follow: the box ends the visible conversation.
	rows := strings.Split(v, "\n")
	input := hint + 1
	for input < len(rows) && !strings.HasPrefix(rows[input], "╭") {
		input++
	}
	if input >= len(rows) || input-hint > 3 {
		t.Fatalf("question box not at the bottom of the view:\n%s", v)
	}
	for i := 0; i < 10; i++ {
		m = upd(m, tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	}
	v = plain(m)
	if strings.Contains(v, questionFreeRow) || strings.Contains(v, questionHint) {
		t.Fatalf("question box still visible after scrolling up:\n%s", v)
	}
	if !strings.Contains(v, "earlier line") {
		t.Fatalf("scrolled view lacks the earlier conversation:\n%s", v)
	}
}

func TestQuestionInlineSecret(t *testing.T) {
	m, srv, sess := askQuestion(t, "use SECRET123?", []string{"yes SECRET123", "no"}, false)
	clip := &fakeClip{}
	m.clip = clip.write
	var seen []string
	check := func(m Model) {
		seen = append(seen, m.View(), strings.Join(m.lines, "\n"))
	}
	check(m)
	v := plain(m)
	if !strings.Contains(v, "use [redacted]?") || !strings.Contains(v, "yes [redacted]") {
		t.Fatalf("question box lacks the redacted text:\n%s", v)
	}
	m = press(m, tea.KeyDown, tea.KeyDown)
	m = keyRunes(m, "my SECRET123")
	check(m)
	if !strings.Contains(plain(m), "my [redacted]") {
		t.Fatalf("free text not shown redacted:\n%s", plain(m))
	}
	x, y := findCell(t, m, questionFreeRow)
	m, _ = drag(m, 0, max(y-6, 1), x+40, y)
	check(m)
	m = press(m, tea.KeyEnter)
	if got := questionToolResult(t, srv, sess); got == "" {
		t.Fatal("no tool result")
	}
	m = m.Drain()
	check(m)
	if !strings.Contains(plain(m), "Q: use [redacted]? → A: my [redacted]") {
		t.Fatalf("answer line missing:\n%s", plain(m))
	}
	x, y = findCell(t, m, "Q: use")
	m, _ = drag(m, x, y, x+60, y)
	check(m)
	if len(clip.texts) < 2 {
		t.Fatalf("clipboard got %d texts, want 2", len(clip.texts))
	}
	if !strings.Contains(strings.Join(clip.texts, "\n"), "[redacted]") {
		t.Errorf("clipboard lacks the redacted text: %q", clip.texts)
	}
	seen = append(seen, clip.texts...)
	for _, s := range seen {
		if strings.Contains(s, "SECRET123") {
			t.Fatalf("secret leaked:\n%s", s)
		}
	}
}
