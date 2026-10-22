package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

func questionCall(q string, options []string, multiple bool) providertest.Response {
	args, _ := json.Marshal(map[string]any{"question": q, "options": options, "multiple": multiple})
	return providertest.Response{ToolCalls: []providertest.ToolCall{
		{ID: "q1", Name: "question", Arguments: string(args)},
	}}
}

// askQuestion runs a turn in which the model calls the question tool and
// returns the model once the inline question box is shown.
func askQuestion(t *testing.T, q string, options []string, multiple bool) (Model, *providertest.Server, *agent.Session) {
	t.Helper()
	srv := providertest.New(questionCall(q, options, multiple), providertest.Response{Text: "thanks"})
	t.Cleanup(srv.Close)
	m, sess := newSessionModel(t, srv)
	m, _ = m.Command("ask me")
	deadline := time.Now().Add(5 * time.Second)
	for {
		m = m.Drain()
		if strings.Contains(plain(m), questionFreeRow) {
			return m, srv, sess
		}
		if time.Now().After(deadline) {
			t.Fatalf("question box never shown:\n%s", plain(m))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// countMarked counts the selected radio rows ("● label") of the inline box
// (its rows sit between │ borders); the tool-call bullet of the conversation
// is also a ●, but never inside the box.
func countMarked(v string) int {
	n := 0
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "│") && strings.Contains(l, "● ") {
			n++
		}
	}
	return n
}

func press(m Model, types ...tea.KeyType) Model {
	for _, ty := range types {
		next, _ := m.Update(tea.KeyMsg{Type: ty})
		m = next.(Model)
	}
	return m
}

// questionToolResult finishes the turn and returns the tool result the
// provider received in the follow-up request.
func questionToolResult(t *testing.T, srv *providertest.Server, sess *agent.Session) string {
	t.Helper()
	waitSession(t, sess)
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	for _, msg := range reqs[1].Messages {
		if msg.Role == "tool" {
			return msg.Content
		}
	}
	t.Fatal("no tool result in the follow-up request")
	return ""
}

func TestQuestionOverlaySingleChoiceRadio(t *testing.T) {
	m, _, _ := askQuestion(t, "Which one?", []string{"a", "b", "c"}, false)
	v := plain(m)
	for _, want := range []string{"○ a", "○ b", "○ c", "Which one?", questionTitle, questionHint} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if n := countMarked(v); n != 0 {
		t.Errorf("nothing selected yet but %d options are marked:\n%s", n, v)
	}
	m = press(m, tea.KeyDown, tea.KeySpace)
	v = plain(m)
	if n := countMarked(v); n != 1 || !strings.Contains(v, "● b") {
		t.Errorf("want exactly one ● on b:\n%s", v)
	}
	m = press(m, tea.KeyUp, tea.KeySpace)
	v = plain(m)
	if n := countMarked(v); n != 1 || !strings.Contains(v, "● a") {
		t.Errorf("want the ● moved to a:\n%s", v)
	}
}

func TestQuestionOverlayMultipleCheckbox(t *testing.T) {
	m, _, _ := askQuestion(t, "Pick some", []string{"a", "b", "c"}, true)
	v := plain(m)
	for _, want := range []string{"[ ] a", "[ ] b", "[ ] c", questionFreeRow} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	m = press(m, tea.KeySpace, tea.KeyDown, tea.KeyDown, tea.KeySpace)
	v = plain(m)
	for _, want := range []string{"[x] a", "[ ] b", "[x] c"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	m = press(m, tea.KeySpace) // toggles c off again
	if v = plain(m); !strings.Contains(v, "[ ] c") {
		t.Errorf("space did not toggle off:\n%s", v)
	}
}

func TestQuestionEnterReturnsChosenLabel(t *testing.T) {
	m, srv, sess := askQuestion(t, "Which?", []string{"a", "b", "c"}, false)
	m = press(m, tea.KeyDown, tea.KeySpace, tea.KeyEnter)
	if strings.Contains(plain(m), questionFreeRow) {
		t.Error("question box still shown after Enter")
	}
	if got := questionToolResult(t, srv, sess); got != "b" {
		t.Errorf("tool result = %q, want b", got)
	}
}

func TestQuestionEnterMultipleJoinsComma(t *testing.T) {
	m, srv, sess := askQuestion(t, "Which?", []string{"x", "z", "y"}, true)
	m = press(m, tea.KeySpace, tea.KeyDown, tea.KeyDown, tea.KeySpace, tea.KeyEnter)
	if got := questionToolResult(t, srv, sess); got != "x, y" {
		t.Errorf("tool result = %q, want %q", got, "x, y")
	}
}

func TestQuestionFreeTextAnswer(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		m, srv, sess := askQuestion(t, "Which?", []string{"a", "b"}, multiple)
		m = press(m, tea.KeyDown, tea.KeyDown)
		// k, j and space are typed into the buffer on the free-text row.
		m = keyRunes(m, "yes")
		m = press(m, tea.KeySpace)
		m = keyRunes(m, "please")
		m = keyRunes(m, "x")
		m = press(m, tea.KeyBackspace)
		if v := plain(m); !strings.Contains(v, "yes please") {
			t.Errorf("multiple=%v: typed text not shown:\n%s", multiple, v)
		}
		m = press(m, tea.KeyEnter)
		if got := questionToolResult(t, srv, sess); got != "yes please" {
			t.Errorf("multiple=%v: tool result = %q, want %q", multiple, got, "yes please")
		}
	}
}

func TestQuestionEscCancelled(t *testing.T) {
	m, srv, sess := askQuestion(t, "Which?", []string{"a", "b"}, false)
	m = press(m, tea.KeyEsc)
	v := plain(m)
	for _, gone := range []string{questionFreeRow, questionHint, "○"} {
		if strings.Contains(v, gone) {
			t.Errorf("question box remnant %q after Esc:\n%s", gone, v)
		}
	}
	if len(m.questions) != 0 {
		t.Error("question still pending")
	}
	if got := questionToolResult(t, srv, sess); got != "cancelled" {
		t.Errorf("tool result = %q, want cancelled", got)
	}
}

func TestQuestionNothingSelectedEnterStays(t *testing.T) {
	m, srv, sess := askQuestion(t, "Which?", []string{"a", "b"}, false)
	m = press(m, tea.KeyEnter)
	if !strings.Contains(plain(m), questionFreeRow) || len(m.questions) != 1 {
		t.Fatalf("question closed on Enter with nothing selected:\n%s", plain(m))
	}
	m = press(m, tea.KeyDown, tea.KeyDown, tea.KeyEnter) // empty free text
	if len(m.questions) != 1 {
		t.Fatal("question closed on Enter with empty free text")
	}
	if sess.Busy() && len(srv.Requests()) > 1 {
		t.Fatalf("a result was sent: %d requests", len(srv.Requests()))
	}
	if got := len(srv.Requests()); got != 1 {
		t.Fatalf("requests = %d, want 1 (the turn still waits)", got)
	}
	m = press(m, tea.KeyEsc) // finish the turn
	if got := questionToolResult(t, srv, sess); got != "cancelled" {
		t.Errorf("tool result = %q, want cancelled", got)
	}
}

func TestQuestionRedactsSecretInPrompt(t *testing.T) {
	m, _, _ := askQuestion(t, "start", []string{"a"}, false)
	m = press(m, tea.KeyEsc)
	// The model itself redacts what it is given, even from a raw event.
	m.apply(agent.Event{Kind: agent.EvQuestion, Question: &agent.QuestionRequest{
		Question: "use key SECRET123?",
		Options:  []string{"yes SECRET123", "no"},
	}})
	v := plain(m)
	if strings.Contains(v, "SECRET123") {
		t.Fatalf("question box leaks the secret:\n%s", v)
	}
	if !strings.Contains(v, "use key [redacted]?") || !strings.Contains(v, "○ yes [redacted]") {
		t.Fatalf("question box lacks the redacted text:\n%s", v)
	}
	if !strings.Contains(v, questionFreeRow) {
		t.Errorf("free-text row missing:\n%s", v)
	}
}
