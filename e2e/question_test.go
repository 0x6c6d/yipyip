package e2e

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
	"yipyip/internal/tui"
)

const questionRow = "type your own answer" // free-text row of the inline question box

func questionResp(q string, options []string, multiple bool) resp {
	args, _ := json.Marshal(map[string]any{"question": q, "options": options, "multiple": multiple})
	return resp{ToolCalls: []providertest.ToolCall{call("q1", "question", string(args))}}
}

func viewText(m tui.Model) string { return ansi.Strip(m.View()) }

// startQuestion submits text and returns once the question box is shown, with the
// turn still running.
func (e *env) startQuestion(t *testing.T, text string) {
	t.Helper()
	m, _ := e.Model.Command(text)
	e.Model = m
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.Model = e.Model.Drain()
		if strings.Contains(viewText(e.Model), questionRow) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("question box never shown:\n%s", viewText(e.Model))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// press sends the keys through the program's Update.
func (e *env) press(types ...tea.KeyType) {
	for _, ty := range types {
		next, _ := e.Model.Update(tea.KeyMsg{Type: ty})
		e.Model = next.(tui.Model)
	}
}

// followupToolResult finishes the turn and returns the tool result message of
// request index idx (the follow-up request of a question call).
func (e *env) followupToolResult(t *testing.T, idx int) string {
	t.Helper()
	waitSession(t, e.Session)
	e.Model = e.Model.Drain()
	reqs := e.Server.Requests()
	if len(reqs) <= idx {
		t.Fatalf("requests = %d, want more than %d", len(reqs), idx)
	}
	var results []string
	for _, msg := range reqs[idx].Messages {
		if msg.Role == "tool" {
			results = append(results, msg.Content)
		}
	}
	if len(results) == 0 {
		t.Fatalf("no tool message in request %d", idx)
	}
	return results[len(results)-1]
}

func TestQuestionSingleChoiceEndsInFollowupRequest(t *testing.T) {
	e := newEnv(t,
		questionResp("Which database?", []string{"postgres", "sqlite"}, false),
		resp{Text: "sqlite it is"})
	e.startQuestion(t, "pick a database")
	e.press(tea.KeyDown, tea.KeySpace, tea.KeyEnter)
	if got := e.followupToolResult(t, 1); got != "sqlite" {
		t.Errorf("tool result = %q, want exactly sqlite", got)
	}
	v := viewText(e.Model)
	if strings.Contains(v, questionRow) {
		t.Errorf("question box still shown after Enter:\n%s", v)
	}
	if !strings.Contains(v, "sqlite it is") {
		t.Errorf("final reply missing:\n%s", v)
	}
}

func TestQuestionMultipleJoinedInRequest(t *testing.T) {
	e := newEnv(t,
		questionResp("Which databases?", []string{"a", "b", "c"}, true),
		resp{Text: "got a and c"})
	e.startQuestion(t, "pick some")
	e.press(tea.KeySpace, tea.KeyDown, tea.KeyDown, tea.KeySpace, tea.KeyEnter)
	if got := e.followupToolResult(t, 1); got != "a, c" {
		t.Errorf("tool result = %q, want %q", got, "a, c")
	}
	if v := viewText(e.Model); !strings.Contains(v, "got a and c") {
		t.Errorf("final reply missing:\n%s", v)
	}
}

func TestQuestionCancelledResultInRequest(t *testing.T) {
	e := newEnv(t,
		questionResp("Which database?", []string{"postgres", "sqlite"}, false),
		resp{Text: "no answer, moving on"})
	e.startQuestion(t, "pick a database")
	e.press(tea.KeyEsc)
	if got := e.followupToolResult(t, 1); got != "cancelled" {
		t.Errorf("tool result = %q, want exactly cancelled", got)
	}
	if e.Session.Busy() {
		t.Error("turn still busy after the final reply")
	}
	v := viewText(e.Model)
	if !strings.Contains(v, "no answer, moving on") || !strings.Contains(v, "Worked for") {
		t.Errorf("turn did not finish normally:\n%s", v)
	}
}

func TestQuestionOverlayVisibleAndTurnRunning(t *testing.T) {
	for _, mode := range []agent.Mode{agent.ModeAuto, agent.ModeManual} {
		e := newEnv(t,
			questionResp("Which database?", []string{"postgres", "sqlite"}, false),
			resp{Text: "sqlite it is"})
		e.Session.SetMode(mode)
		e.startQuestion(t, "pick a database")

		v := viewText(e.Model)
		for _, want := range []string{"Which database?", "postgres", "sqlite"} {
			if !strings.Contains(v, want) {
				t.Errorf("mode %v: question box lacks %q:\n%s", mode, want, v)
			}
		}
		if !e.Session.Busy() {
			t.Errorf("mode %v: turn not busy while the question waits", mode)
		}
		if strings.Contains(v, "Worked for") {
			t.Errorf("mode %v: turn finished marker while the question waits:\n%s", mode, v)
		}
		if got := len(e.Server.Requests()); got != 1 {
			t.Errorf("mode %v: requests = %d while waiting, want 1", mode, got)
		}
		denied := "denied by" + " user"
		prompt := "y / n / a (" + "always"
		if strings.Contains(v, denied) || strings.Contains(v, prompt) {
			t.Errorf("mode %v: approval prompt or denial for a question call:\n%s", mode, v)
		}

		e.press(tea.KeyDown, tea.KeySpace, tea.KeyEnter)
		if got := e.followupToolResult(t, 1); got != "sqlite" {
			t.Errorf("mode %v: tool result = %q, want sqlite", mode, got)
		}
		if v := viewText(e.Model); strings.Contains(v, denied) {
			t.Errorf("mode %v: denial in the final view:\n%s", mode, v)
		}
	}
}
