package e2e

import (
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/provider/providertest"
	"yipyip/internal/tui"
)

const (
	jevPhaseA int32 = iota
	jevPhaseB
	jevPhaseC
)

// waitView drains the model until its view contains want.
func (e *env) waitView(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.Model = e.Model.Drain()
		if strings.Contains(viewText(e.Model), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("view never showed %q:\n%s", want, viewText(e.Model))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestJevE2E(t *testing.T) {
	var phase atomic.Int32
	phase.Store(jevPhaseA)

	jev := providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
		if phase.Load() == jevPhaseC {
			return providertest.JevResponse{Status: http.StatusInternalServerError}
		}
		answers := map[string]any{}
		for name, q := range req.Questions {
			qm, _ := q.(map[string]any)
			switch qm["type"] {
			case "score":
				answers[name] = providertest.JevScoreAnswer(0, 0.9)
			case "choice":
				answers[name] = providertest.JevChoiceAnswer("beta", 0.95)
			case "noul":
				if phase.Load() == jevPhaseB {
					answers[name] = providertest.JevNoulAnswer(0.9)
				} else {
					answers[name] = providertest.JevNoulAnswer(0.1)
				}
			}
		}
		return providertest.JevResponse{Answers: answers}
	})
	t.Cleanup(jev.Close)

	// One provider script per user message; the step is the number of tool
	// results since the last user message.
	srv := providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		user, steps := "", 0
		for _, m := range req.Messages {
			switch m.Role {
			case "user":
				user, steps = m.Content, 0
			case "tool":
				steps++
			}
		}
		qargs := `{"question":"Which one?","options":["alpha","beta"],"multiple":false}`
		bash := `{"command":"echo e2e"}`
		switch {
		case strings.Contains(user, "turn A"):
			if steps == 0 {
				return resp{ToolCalls: []providertest.ToolCall{call("qa", "question", qargs)}}
			}
			return resp{Text: "A done"}
		case strings.Contains(user, "turn B"):
			if steps == 0 {
				return resp{ToolCalls: []providertest.ToolCall{call("ba", "bash", bash)}}
			}
			return resp{Text: "B done"}
		case strings.Contains(user, "turn C"):
			switch steps {
			case 0:
				return resp{ToolCalls: []providertest.ToolCall{call("qc", "question", qargs)}}
			case 1:
				return resp{ToolCalls: []providertest.ToolCall{call("bc", "bash", bash)}}
			}
			return resp{Text: "C done"}
		}
		return resp{Text: "ok"}
	})

	e := newEnvServer(t, srv, envOpts{
		contextLimit: 8000,
		projectJSON:  `{"jev":{"baseURL":"` + jev.URL + `","apiKey":"{env:TYPESAFE_API_KEY}","route":{"easy":"fake/m2","hard":"fake/m1"}}}`,
	})
	status := func() string {
		r1, r2 := e.Model.StatusRows()
		return r1 + "\n" + r2
	}
	// toolResults returns the tool results of the request's current turn.
	toolResults := func(req providertest.Request) []string {
		var out []string
		for _, m := range req.Messages {
			switch m.Role {
			case "user":
				out = nil
			case "tool":
				out = append(out, m.Content)
			}
		}
		return out
	}

	e.submit(t, "/jev")
	if v := viewText(e.Model); !strings.Contains(v, "jev on") {
		t.Fatalf("/jev: no 'jev on':\n%s", v)
	}
	if !strings.Contains(status(), "jev") {
		t.Errorf("status lacks jev: %q", status())
	}

	// Turn A: easy route, jev answers the question.
	e.submit(t, "turn A please SECRET123")
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("turn A: %d provider requests, want 2", len(reqs))
	}
	for i, r := range reqs {
		if r.Model != "m2" {
			t.Errorf("turn A request %d model = %q, want m2", i, r.Model)
		}
	}
	if !strings.Contains(status(), "route: fake/m2") {
		t.Errorf("turn A: status lacks route: fake/m2: %q", status())
	}
	if v := viewText(e.Model); strings.Contains(v, questionRow) || !strings.Contains(v, "A done") {
		t.Errorf("turn A: question prompt shown or no final reply:\n%s", v)
	}
	if got := toolResults(reqs[1]); len(got) != 1 || got[0] != "beta" {
		t.Errorf("turn A: model got %q, want [beta]", got)
	}

	// Turn B: risky bash in auto mode turns into the approval prompt.
	phase.Store(jevPhaseB)
	n := len(srv.Requests())
	m, _ := e.Model.Command("turn B run it")
	e.Model = m
	e.waitView(t, "y / n / a (always")
	next, _ := e.Model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	e.Model = next.(tui.Model)
	waitSession(t, e.Session)
	e.Model = e.Model.Drain()
	reqsB := srv.Requests()[n:]
	if len(reqsB) != 2 {
		t.Fatalf("turn B: %d provider requests, want 2", len(reqsB))
	}
	if got := toolResults(reqsB[1]); len(got) != 1 || !strings.Contains(got[0], "denied by user") {
		t.Errorf("turn B: bash result = %q, want denied by user", got)
	}
	if v := viewText(e.Model); !strings.Contains(v, "B done") {
		t.Errorf("turn B: no final reply:\n%s", v)
	}

	// Turn C: jev fails with 500; fallback = session model, real question, bash as is.
	phase.Store(jevPhaseC)
	n = len(srv.Requests())
	e.startQuestion(t, "turn C go")
	e.press(tea.KeyDown, tea.KeySpace, tea.KeyEnter)
	waitSession(t, e.Session)
	e.Model = e.Model.Drain()
	reqsC := srv.Requests()[n:]
	if len(reqsC) != 3 {
		t.Fatalf("turn C: %d provider requests, want 3", len(reqsC))
	}
	for i, r := range reqsC {
		if r.Model != "m1" {
			t.Errorf("turn C request %d model = %q, want m1", i, r.Model)
		}
	}
	if got := toolResults(reqsC[1]); len(got) != 1 || got[0] != "beta" {
		t.Errorf("turn C: question result = %q, want [beta]", got)
	}
	if got := toolResults(reqsC[2]); len(got) != 2 || strings.Contains(got[1], "denied by user") {
		t.Errorf("turn C: bash result = %q, want a run without denial", got)
	}
	if v := viewText(e.Model); strings.Contains(v, "y / n / a (always") || !strings.Contains(v, "C done") {
		t.Errorf("turn C: approval prompt shown or no final reply:\n%s", v)
	}

	e.submit(t, "/jev stats")
	v := viewText(e.Model)
	if a := statNum(t, v, "jev answered:"); a < 2 {
		t.Errorf("jev answered = %d, want >= 2:\n%s", a, v)
	}
	if f := statNum(t, v, "jev fallbacks:"); f < 1 {
		t.Errorf("jev fallbacks = %d, want >= 1:\n%s", f, v)
	}

	for i, r := range jev.Requests() {
		if strings.Contains(string(r.Body), "SECRET123") || strings.Contains(r.State, "JEVKEY-E2E") {
			t.Errorf("jev request %d leaks a secret: %s", i, r.Body)
		}
	}
	for i, r := range srv.Requests() {
		if strings.Contains(string(r.Body), "JEVKEY-E2E") {
			t.Errorf("provider request %d contains the jev key", i)
		}
	}

	e.submit(t, "/jev")
	if v := viewText(e.Model); !strings.Contains(v, "jev off") {
		t.Fatalf("/jev again: no 'jev off':\n%s", v)
	}
	before := len(jev.Requests())
	e.submit(t, "one more turn")
	if got := len(jev.Requests()); got != before {
		t.Errorf("jev requests after off = %d, want %d", got, before)
	}
}

// statNum returns the number after label in the first line containing it.
func statNum(t *testing.T, view, label string) int {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if i := strings.Index(line, label); i >= 0 {
			if n, err := strconv.Atoi(strings.Fields(line[i+len(label):] + " x")[0]); err == nil {
				return n
			}
		}
	}
	t.Fatalf("%q not in view:\n%s", label, view)
	return 0
}
