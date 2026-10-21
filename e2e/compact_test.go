package e2e

import (
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
	"yipyip/internal/tui"
)

const compactSummary = "SUMMARY-OF-EARLIER-TURNS"

// isSummaryReq reports whether req is the compaction request: its last
// message is a user message asking for a summary.
func isSummaryReq(req providertest.Request) bool {
	n := len(req.Messages)
	return n > 0 && req.Messages[n-1].Role == "user" &&
		strings.HasPrefix(req.Messages[n-1].Content, "Summarize the conversation")
}

func countSummaryReqs(srv *providertest.Server) int {
	n := 0
	for _, r := range srv.Requests() {
		if isSummaryReq(r) {
			n++
		}
	}
	return n
}

// compactServer answers summary requests with compactSummary and every other
// request with "answer N", reporting usage prompt tokens.
func compactServer(prompt int) *providertest.Server {
	return providertest.NewFunc(func(n int, req providertest.Request) providertest.Response {
		if isSummaryReq(req) {
			return resp{Text: compactSummary, Usage: &providertest.Usage{Prompt: 50, Completion: 5}}
		}
		return resp{Text: "answer", Usage: &providertest.Usage{Prompt: prompt, Completion: 10}}
	})
}

type roleContent struct{ Role, Content string }

func roles(msgs []providertest.Message) []roleContent {
	out := make([]roleContent, len(msgs))
	for i, m := range msgs {
		out[i] = roleContent{m.Role, m.Content}
	}
	return out
}

func TestCompactShrinksHistory(t *testing.T) {
	srv := compactServer(10)
	e := newEnvServer(t, srv, envOpts{contextLimit: 8000})
	e.submit(t, "first question")
	e.submit(t, "second question")
	e.submit(t, "third question")
	system := srv.Requests()[0].Messages[0]
	if system.Role != "system" {
		t.Fatalf("first message of the first request is %q, want system", system.Role)
	}

	m, cmd := e.Model.Command("/compact")
	if cmd == nil {
		t.Fatal("/compact returned no command")
	}
	mustContain(t, m.View(), "compacting...")
	next, _ := m.Update(cmd())
	e.Model = next.(tui.Model).Drain()
	if n := countSummaryReqs(srv); n != 1 {
		t.Fatalf("%d summary requests, want 1", n)
	}
	mustContain(t, e.Model.View(), "compacted:")
	mustNotContain(t, e.Model.View(), "error:")

	e.submit(t, "fourth question")
	reqs := srv.Requests()
	last := reqs[len(reqs)-1]
	if isSummaryReq(last) {
		t.Fatal("the last request is a summary request")
	}
	want := []roleContent{
		{"system", system.Content},
		{"user", "[Summary of the earlier conversation]\n" + compactSummary},
		{"user", "second question"},
		{"assistant", "answer"},
		{"user", "third question"},
		{"assistant", "answer"},
		{"user", "fourth question"},
	}
	got := roles(last.Messages)
	if len(got) != len(want) {
		t.Fatalf("request has %d messages, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("message %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if strings.Contains(string(last.Body), "first question") {
		t.Error("the compacted turn is still in the request")
	}
}

// autoCompactRun runs three turns against a fake that reports a prompt of 800
// tokens (80% of the limit 1000), then a fourth message; it returns the
// server and the requests that the fourth message caused.
func autoCompactRun(t *testing.T, projectJSON string) (srv *providertest.Server, second []providertest.Request) {
	t.Helper()
	srv = compactServer(800)
	e := newEnvServer(t, srv, envOpts{contextLimit: 1000, projectJSON: projectJSON})
	// Three turns so that the history is long enough to compact.
	e.submit(t, "one")
	e.submit(t, "two")
	e.submit(t, "three")
	before := len(srv.Requests())
	e.submit(t, "four")
	return srv, srv.Requests()[before:]
}

func TestAutoCompact80(t *testing.T) {
	_, second := autoCompactRun(t, "")
	if len(second) != 2 {
		t.Fatalf("message four sent %d requests, want 2 (summary, normal)", len(second))
	}
	if !isSummaryReq(second[0]) {
		t.Errorf("first request is not the summary request: %+v", roles(second[0].Messages))
	}
	if isSummaryReq(second[1]) {
		t.Error("second request is a summary request")
	}
	if last := second[1].Messages[len(second[1].Messages)-1]; last.Role != "user" || last.Content != "four" {
		t.Errorf("normal request ends with %+v", last)
	}
}

func TestAutoCompactOff(t *testing.T) {
	srv, second := autoCompactRun(t, `{"auto_compact": false}`)
	if n := countSummaryReqs(srv); n != 0 {
		t.Fatalf("%d summary requests with auto_compact false", n)
	}
	if len(second) != 1 {
		t.Fatalf("message four sent %d requests, want 1", len(second))
	}
}
