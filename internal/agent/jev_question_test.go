package agent

import (
	"strings"
	"testing"
	"time"

	"yipyip/internal/provider/providertest"
)

const jevQArgs = `{"question":"Which color?","options":["alpha","beta"]}`

// jevQOutcome is what one question turn with jev looked like.
type jevQOutcome struct {
	asked  bool   // an EvQuestion reached the user
	result string // the question tool result
	info   []string
}

// jevQTurn submits user, answers every EvQuestion with "USER-ANS" by polling
// PollEvents and returns the outcome once the turn is done.
func jevQTurn(t *testing.T, s *Session, user string) jevQOutcome {
	t.Helper()
	if err := s.Submit(user); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	var o jevQOutcome
	deadline := time.Now().Add(8 * time.Second)
	for {
		for _, ev := range s.PollEvents() {
			switch ev.Kind {
			case EvQuestion:
				o.asked = true
				ev.Question.Respond("USER-ANS")
			case EvInfo:
				o.info = append(o.info, ev.Text)
			case EvToolResult:
				if ev.Tool == toolQuestion {
					o.result = ev.Text
				}
			case EvTurnDone:
				return o
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("turn did not finish: %+v", o)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// jevQCase runs one question turn with question args against a jev fake that
// replies resp (its choice answer is sent under every asked question name).
func jevQCase(t *testing.T, args string, resp providertest.JevResponse, on bool) (jevQOutcome, []providertest.JevRequest, *Session) {
	t.Helper()
	srv := questionServer(args)
	t.Cleanup(srv.Close)
	j := providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
		r := resp
		if a, ok := resp.Answers["*"]; ok {
			r.Answers = map[string]any{}
			for name := range req.Questions {
				r.Answers[name] = a
			}
		}
		return r
	})
	t.Cleanup(j.Close)
	s := newJevSession(t, srv, jevConfigJSON(j))
	s.SetJevTimeout(150 * time.Millisecond)
	if on {
		if err := s.SetJev(true); err != nil {
			t.Fatal(err)
		}
	}
	o := jevQTurn(t, s, "please pick MSG-user SECRET123")
	return o, j.Requests(), s
}

func jevPick(choice string, conf float64) providertest.JevResponse {
	return providertest.JevResponse{Answers: map[string]any{"*": providertest.JevChoiceAnswer(choice, conf)}}
}

func TestJevQuestion(t *testing.T) {
	o, reqs, s := jevQCase(t, jevQArgs, jevPick("beta", 0.9), true)
	if o.asked || o.result != "beta" || len(reqs) != 1 {
		t.Fatalf("confident: %+v, %d jev requests", o, len(reqs))
	}
	if len(o.info) != 1 || o.info[0] != "jev answered: Which color? → beta" {
		t.Fatalf("info: %q", o.info)
	}
	if st := s.JevStats(); st.Answered != 1 || st.Fallbacks != 0 {
		t.Fatalf("stats: %+v", st)
	}
	for _, q := range reqs[0].Questions {
		qm, _ := q.(map[string]any)
		if qm["type"] != "choice" {
			t.Fatalf("question: %v", qm)
		}
	}

	// Jev consulted, the user asked.
	for name, resp := range map[string]providertest.JevResponse{
		"low confidence": jevPick("beta", 0.3),
		"unknown choice": jevPick("gamma", 0.99),
	} {
		o, reqs, _ := jevQCase(t, jevQArgs, resp, true)
		if !o.asked || o.result != "USER-ANS" || len(reqs) != 1 {
			t.Fatalf("%s: %+v, %d jev requests", name, o, len(reqs))
		}
	}

	// Jev never consulted.
	for name, c := range map[string]struct {
		args string
		on   bool
	}{
		"multiple":  {`{"question":"Q?","options":["a","b"],"multiple":true}`, true},
		"1 option":  {`{"question":"Q?","options":["only"]}`, true},
		"0 options": {`{"question":"Free?"}`, true},
		"jev off":   {jevQArgs, false},
	} {
		o, reqs, s := jevQCase(t, c.args, jevPick("a", 0.99), c.on)
		if !o.asked || len(reqs) != 0 {
			t.Fatalf("%s: %+v, %d jev requests", name, o, len(reqs))
		}
		if st := s.JevStats(); st.Answered != 0 || st.Fallbacks != 0 {
			t.Fatalf("%s: stats %+v", name, st)
		}
	}
}

func TestJevQuestionState(t *testing.T) {
	_, reqs, _ := jevQCase(t, `{"question":"Which color SECRET123?","options":["alpha","beta"]}`, jevPick("beta", 0.9), true)
	if len(reqs) != 1 {
		t.Fatalf("%d jev requests", len(reqs))
	}
	st := reqs[0].State
	iq, io, ic, iu := strings.Index(st, "question: Which color"), strings.Index(st, "options: alpha, beta"), strings.Index(st, "conversation:"), strings.Index(st, "MSG-user")
	if iq < 0 || io < iq || ic < io || iu < ic {
		t.Fatalf("state layout: %q", st)
	}
	if strings.Contains(string(reqs[0].Body), "SECRET123") || strings.Contains(string(reqs[0].Body), "jev-session-key") {
		t.Fatal("secret in jev request")
	}
}

func TestJevFallback(t *testing.T) {
	for name, resp := range map[string]providertest.JevResponse{
		"500":            {Status: 500},
		"422":            {Status: 422},
		"timeout":        {Delay: 5 * time.Second},
		"low confidence": jevPick("alpha", 0.1),
	} {
		o, reqs, s := jevQCase(t, jevQArgs, resp, true)
		if !o.asked || o.result != "USER-ANS" || len(reqs) != 1 {
			t.Fatalf("%s: %+v, %d jev requests", name, o, len(reqs))
		}
		if st := s.JevStats(); st.Fallbacks != 1 || st.Answered != 0 {
			t.Fatalf("%s: stats %+v", name, st)
		}
		if h := s.History(); h[len(h)-1].Content != "follow-up" {
			t.Fatalf("%s: turn not finished: %+v", name, h[len(h)-1])
		}
	}
}
