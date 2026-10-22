package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"yipyip/internal/provider/providertest"
)

const jevRouteJSON = `,"route":{"easy":"fake/m2","hard":"fake/m1"}`

// routeRig is a session with a tool-calling fake provider and a jev fake.
type routeRig struct {
	srv *providertest.Server
	j   *providertest.JevServer
	s   *Session
}

// newRouteRig starts the fakes; jev answers resp (its "difficulty" answer is
// sent under every asked question name); extra is appended to the jev block.
func newRouteRig(t *testing.T, resp providertest.JevResponse, extra string, on bool) *routeRig {
	t.Helper()
	r := &routeRig{
		srv: providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
			if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "user" {
				return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "r1", Name: "read_file", Arguments: `{"path":"notes.txt"}`}}}
			}
			return providertest.Response{Text: "done"}
		}),
		j: providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
			out := resp
			if a, ok := resp.Answers["difficulty"]; ok {
				out.Answers = map[string]any{}
				for name := range req.Questions {
					out.Answers[name] = a
				}
			}
			return out
		}),
	}
	t.Cleanup(r.srv.Close)
	t.Cleanup(r.j.Close)
	r.s = newJevSession(t, r.srv, `{"baseURL":"`+r.j.URL+`","apiKey":"{env:TYPESAFE_API_KEY}"`+extra+`}`)
	if err := os.WriteFile(filepath.Join(r.s.StartDir(), "notes.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.s.SetJevTimeout(150 * time.Millisecond)
	if on {
		if err := r.s.SetJev(true); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

// turn runs one turn and returns the models of its requests and its infos.
func (r *routeRig) turn(t *testing.T, text string) (models, infos []string) {
	t.Helper()
	before := len(r.srv.Requests())
	if err := r.s.Submit(text); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for done := false; !done; {
		for _, ev := range r.s.PollEvents() {
			switch ev.Kind {
			case EvApproval:
				ev.Approval.Respond(Allow)
			case EvInfo:
				infos = append(infos, ev.Text)
			case EvTurnDone:
				done = true
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("turn did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, q := range r.srv.Requests()[before:] {
		models = append(models, q.Model)
	}
	return models, infos
}

// allModel reports whether a tool-call turn (>= 2 requests) used only want.
func allModel(models []string, want string) bool {
	if len(models) < 2 {
		return false
	}
	for _, m := range models {
		if m != want {
			return false
		}
	}
	return true
}

func routeScore(score, conf float64) providertest.JevResponse {
	return providertest.JevResponse{Answers: map[string]any{"difficulty": providertest.JevScoreAnswer(score, conf)}}
}

func TestJevRoute(t *testing.T) {
	for _, sc := range []float64{0, 0.99} {
		r := newRouteRig(t, routeScore(sc, 0.9), jevRouteJSON, true)
		models, infos := r.turn(t, "small fix please")
		if !allModel(models, "m2") || r.s.TurnModel() != "fake/m2" || r.s.ModelID() != "fake/m1" {
			t.Fatalf("score %v: models %q, turn model %q, session %q", sc, models, r.s.TurnModel(), r.s.ModelID())
		}
		if !contains(infos, "jev route: fake/m2") {
			t.Fatalf("score %v: infos %q", sc, infos)
		}
		reqs := r.j.Requests()
		if len(reqs) != 1 || reqs[0].State != "request: small fix please" || reqs[0].Questions["difficulty"] == nil {
			t.Fatalf("score %v: jev requests %+v", sc, reqs)
		}
		if st := r.s.JevStats(); st.Answered != 1 || st.Fallbacks != 0 || st.RoutingTokens == 0 {
			t.Fatalf("score %v: stats %+v", sc, st)
		}
	}
	for _, sc := range []float64{1, 2} {
		r := newRouteRig(t, routeScore(sc, 0.9), jevRouteJSON, true)
		if err := r.s.SetModel("fake/m2"); err != nil {
			t.Fatal(err)
		}
		if models, _ := r.turn(t, "big refactor"); !allModel(models, "m1") || r.s.TurnModel() != "fake/m1" || r.s.ModelID() != "fake/m2" {
			t.Fatalf("score %v: models %q, turn model %q, session %q", sc, models, r.s.TurnModel(), r.s.ModelID())
		}
		if models, _ := r.turn(t, "again"); !allModel(models, "m1") || len(r.j.Requests()) != 2 {
			t.Fatalf("score %v: second turn models %q, %d jev requests", sc, models, len(r.j.Requests()))
		}
	}
}

func TestJevRouteFallback(t *testing.T) {
	for name, resp := range map[string]providertest.JevResponse{
		"500":            {Status: 500},
		"timeout":        {Delay: 5 * time.Second},
		"low confidence": routeScore(0, 0.2),
	} {
		r := newRouteRig(t, resp, jevRouteJSON, true)
		models, infos := r.turn(t, "x")
		if !allModel(models, "m1") || r.s.TurnModel() != "" || r.s.ModelID() != "fake/m1" {
			t.Fatalf("%s: models %q, turn model %q", name, models, r.s.TurnModel())
		}
		if st := r.s.JevStats(); st.Fallbacks != 1 || st.Answered != 0 || st.RoutingTokens != 0 {
			t.Fatalf("%s: stats %+v", name, st)
		}
		for _, in := range infos {
			if len(in) >= 10 && in[:10] == "jev route:" {
				t.Fatalf("%s: route info %q", name, in)
			}
		}
	}
}

func TestJevRouteOff(t *testing.T) {
	for name, c := range map[string]struct {
		extra string
		on    bool
	}{"jev off": {jevRouteJSON, false}, "no route": {"", true}} {
		r := newRouteRig(t, routeScore(0, 0.9), c.extra, c.on)
		if models, _ := r.turn(t, "x"); !allModel(models, "m1") || len(r.j.Requests()) != 0 || r.s.TurnModel() != "" {
			t.Fatalf("%s: models %q, %d jev requests", name, models, len(r.j.Requests()))
		}
		if st := r.s.JevStats(); st.Answered != 0 || st.Fallbacks != 0 {
			t.Fatalf("%s: stats %+v", name, st)
		}
	}
}

func TestJevRouteNoBlock(t *testing.T) {
	r := newRouteRig(t, providertest.JevResponse{Delay: 5 * time.Second}, jevRouteJSON, true)
	start := time.Now()
	if err := r.s.Submit("x"); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("Submit blocked %v", d)
	}
	r.s.Cancel()
	r.s.Wait()
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
