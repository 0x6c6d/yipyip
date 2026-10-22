package agent

import (
	"strings"
	"testing"
	"time"

	"yipyip/internal/provider/providertest"
)

// TestJevQuestionAsksUser checks that a question of the model always reaches
// the user, even while jev is on and would answer every question confidently,
// and that jev is never asked to answer it.
func TestJevQuestionAsksUser(t *testing.T) {
	for _, on := range []bool{true, false} {
		srv := questionServer(`{"question":"Which color?","options":["alpha","beta"]}`)
		t.Cleanup(srv.Close)
		j := providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
			ans := map[string]any{}
			for name, q := range req.Questions {
				if qm, _ := q.(map[string]any); qm["type"] == "score" {
					ans[name] = providertest.JevScoreAnswer(0, 0.99)
				} else {
					ans[name] = map[string]any{"type": "choice", "choice": "beta", "confidence": 0.99}
				}
			}
			return providertest.JevResponse{Answers: ans}
		})
		t.Cleanup(j.Close)
		s := newJevSession(t, srv, `{"baseURL":"`+j.URL+`","apiKey":"{env:TYPESAFE_API_KEY}","route":{"easy":"fake/m2","hard":"fake/m1"}}`)
		s.SetJevTimeout(500 * time.Millisecond)
		if on {
			if err := s.SetJev(true); err != nil {
				t.Fatal(err)
			}
		}

		asked := 0
		evs := runQuestionTurn(t, s, func(q *QuestionRequest) {
			asked++
			if q.Question != "Which color?" || strings.Join(q.Options, "|") != "alpha|beta" {
				t.Errorf("on=%v: question %q %q", on, q.Question, q.Options)
			}
			q.Respond("USER-ANS")
		})
		if asked != 1 {
			t.Fatalf("on=%v: %d questions reached the user, want 1", on, asked)
		}
		if got := questionResults(s); len(got) != 1 || got[0] != "USER-ANS" {
			t.Fatalf("on=%v: question results %q, want [USER-ANS]", on, got)
		}
		assertContinued(t, s, evs)
		for _, ev := range evs {
			if ev.Kind == EvInfo && strings.Contains(ev.Text, "answered") {
				t.Errorf("on=%v: info %q", on, ev.Text)
			}
		}

		reqs := j.Requests()
		if want := map[bool]int{true: 1, false: 0}[on]; len(reqs) != want {
			t.Fatalf("on=%v: %d jev requests, want %d", on, len(reqs), want)
		}
		for _, r := range reqs {
			for name, q := range r.Questions {
				qm, _ := q.(map[string]any)
				if qm["type"] != "score" || name == "answer" {
					t.Errorf("on=%v: jev got question %q %v", on, name, qm)
				}
			}
			if strings.Contains(r.State, "Which color") {
				t.Errorf("on=%v: question text sent to jev: %q", on, r.State)
			}
		}
	}
}
