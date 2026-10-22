package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"yipyip/internal/provider/providertest"
)

// noRiskTurn runs one turn calling bash, write_file and edit_file in mode with
// jev on and a route; the fake jev answers every noul question with 0.99 and
// the routing score confidently. Every approval prompt is answered with d.
// It returns the approved tools in prompt order, the start dir and the jev
// requests.
func noRiskTurn(t *testing.T, mode Mode, d Decision) ([]string, string, []providertest.JevRequest) {
	t.Helper()
	srv := providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		steps := 0
		for _, m := range req.Messages {
			switch m.Role {
			case "user":
				steps = 0
			case "tool":
				steps++
			}
		}
		switch steps {
		case 0:
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "b1", Name: "bash", Arguments: `{"command":"echo nr > bash.txt"}`}}}
		case 1:
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "w1", Name: "write_file", Arguments: `{"path":"w.txt","content":"hello\n"}`}}}
		case 2:
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "e1", Name: "edit_file", Arguments: `{"path":"w.txt","old_string":"hello","new_string":"bye"}`}}}
		}
		return providertest.Response{Text: "done"}
	})
	t.Cleanup(srv.Close)
	j := providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
		ans := map[string]any{}
		for name, q := range req.Questions {
			if qm, _ := q.(map[string]any); qm["type"] == "score" {
				ans[name] = providertest.JevScoreAnswer(0, 0.95)
			} else {
				ans[name] = providertest.JevNoulAnswer(0.99)
			}
		}
		return providertest.JevResponse{Answers: ans}
	})
	t.Cleanup(j.Close)

	s := newJevSession(t, srv, `{"baseURL":"`+j.URL+`","apiKey":"{env:TYPESAFE_API_KEY}"`+jevRouteJSON+`}`)
	s.SetJevTimeout(500 * time.Millisecond)
	if err := s.SetJev(true); err != nil {
		t.Fatal(err)
	}
	s.SetMode(mode)
	if err := s.Submit("do the three things"); err != nil {
		t.Fatal(err)
	}
	var prompted []string
	deadline := time.Now().Add(8 * time.Second)
	for done := false; !done; {
		for _, ev := range s.PollEvents() {
			switch ev.Kind {
			case EvApproval:
				prompted = append(prompted, ev.Tool)
				ev.Approval.Respond(d)
			case EvQuestion:
				ev.Question.Cancel()
			case EvTurnDone:
				done = true
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("turn did not finish")
		}
		time.Sleep(3 * time.Millisecond)
	}
	return prompted, s.StartDir(), j.Requests()
}

// TestJevNoRiskQuestion checks that jev no longer screens tool calls: in auto
// mode all gated calls run unprompted although jev would call them risky, in
// manual mode bash is still prompted and a denied call does not run, and jev
// never gets a noul or "risk" question.
func TestJevNoRiskQuestion(t *testing.T) {
	noRisk := func(t *testing.T, reqs []providertest.JevRequest) {
		t.Helper()
		if len(reqs) == 0 {
			t.Fatal("jev got no request (routing expected)")
		}
		for i, r := range reqs {
			for name, q := range r.Questions {
				if qm, _ := q.(map[string]any); qm["type"] == "noul" || name == "risk" {
					t.Errorf("jev request %d asks %q %v", i, name, qm)
				}
			}
		}
	}

	t.Run("auto", func(t *testing.T) {
		prompted, dir, reqs := noRiskTurn(t, ModeAuto, Deny)
		if len(prompted) != 0 {
			t.Fatalf("auto mode prompted for %v", prompted)
		}
		if b, err := os.ReadFile(filepath.Join(dir, "bash.txt")); err != nil || string(b) != "nr\n" {
			t.Errorf("bash did not run: %q %v", b, err)
		}
		if b, err := os.ReadFile(filepath.Join(dir, "w.txt")); err != nil || string(b) != "bye\n" {
			t.Errorf("write_file/edit_file did not run: %q %v", b, err)
		}
		noRisk(t, reqs)
	})

	t.Run("manual deny", func(t *testing.T) {
		prompted, dir, reqs := noRiskTurn(t, ModeManual, Deny)
		if len(prompted) == 0 || prompted[0] != "bash" {
			t.Fatalf("manual mode prompts = %v, want bash first", prompted)
		}
		if _, err := os.Stat(filepath.Join(dir, "bash.txt")); err == nil {
			t.Error("denied bash ran")
		}
		if _, err := os.Stat(filepath.Join(dir, "w.txt")); err == nil {
			t.Error("denied write_file ran")
		}
		noRisk(t, reqs)
	})
}
