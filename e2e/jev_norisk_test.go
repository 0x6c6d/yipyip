package e2e

import (
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

// TestJevNoRiskScreen checks through the TUI that in auto mode with jev on
// the gated calls bash, write_file and edit_file run without an approval
// prompt, although the fake jev would call every noul question risky, and
// that jev never gets a "risk" question.
func TestJevNoRiskScreen(t *testing.T) {
	jev := providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
		answers := map[string]any{}
		for name, q := range req.Questions {
			if qm, _ := q.(map[string]any); qm["type"] == "score" {
				answers[name] = providertest.JevScoreAnswer(0, 0.95)
			} else {
				answers[name] = providertest.JevNoulAnswer(0.99)
			}
		}
		return providertest.JevResponse{Answers: answers}
	})
	t.Cleanup(jev.Close)
	srv := providertest.New(
		resp{ToolCalls: []providertest.ToolCall{call("b1", "bash", `{"command":"echo risky > bash.txt"}`)}},
		resp{ToolCalls: []providertest.ToolCall{call("w1", "write_file", `{"path":"w.txt","content":"hello\n"}`)}},
		resp{ToolCalls: []providertest.ToolCall{call("e1", "edit_file", `{"path":"w.txt","old_string":"hello","new_string":"bye"}`)}},
		resp{Text: "all three done"})
	e := newEnvServer(t, srv, envOpts{
		contextLimit: 8000,
		projectJSON:  `{"jev":{"baseURL":"` + jev.URL + `","apiKey":"{env:TYPESAFE_API_KEY}","route":{"easy":"fake/m2","hard":"fake/m1"}}}`,
	})

	e.submit(t, "/jev")
	if v := viewText(e.Model); !strings.Contains(v, "jev on") {
		t.Fatalf("/jev: no 'jev on':\n%s", v)
	}
	e.submit(t, "do the three things")
	v := viewText(e.Model)
	if strings.Contains(v, "y / n / a (always") || !strings.Contains(v, "all three done") {
		t.Errorf("approval prompt shown or no final reply:\n%s", v)
	}
	if got := e.read("bash.txt"); got != "risky\n" {
		t.Errorf("bash.txt = %q, want the bash effect", got)
	}
	if got := e.read("w.txt"); got != "bye\n" {
		t.Errorf("w.txt = %q, want the write_file and edit_file effects", got)
	}
	reqs := jev.Requests()
	if len(reqs) == 0 {
		t.Fatal("jev got no request (routing expected)")
	}
	for i, r := range reqs {
		for name, q := range r.Questions {
			if qm, _ := q.(map[string]any); name == "risk" || qm["type"] == "noul" {
				t.Errorf("jev request %d asks %q %v", i, name, qm)
			}
		}
	}
}
