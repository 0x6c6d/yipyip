package e2e

import (
	"encoding/json"
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

// headlessQuestion is a fake response asking one question with call id id.
func headlessQuestion(id, q string, options []string) providertest.Response {
	args, _ := json.Marshal(map[string]any{"question": q, "options": options})
	return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: id, Name: "question", Arguments: string(args)}}}
}

// lastToolMessage returns the content of the last tool message of request idx.
func lastToolMessage(t *testing.T, srv *providertest.Server, idx int) string {
	t.Helper()
	reqs := srv.Requests()
	if len(reqs) <= idx {
		t.Fatalf("requests = %d, want more than %d", len(reqs), idx)
	}
	got := ""
	for _, m := range reqs[idx].Messages {
		if m.Role == "tool" {
			got = m.Content
		}
	}
	return got
}

func questionTexts(recs []map[string]any) []string {
	var out []string
	for _, r := range recs {
		if r["type"] == "question" {
			s, _ := r["question"].(string)
			out = append(out, s)
		}
	}
	return out
}

func TestHeadlessAnswerOrder(t *testing.T) {
	srv := providertest.New(
		headlessQuestion("q1", "Pick?", []string{"A", "B"}),
		headlessQuestion("q2", "Why?", nil),
		providertest.Response{Text: "end"},
	)
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--answer", "B", "--answer", "my words", "--json")
	if res.code != 0 {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	if got := lastToolMessage(t, srv, 1); got != "B" {
		t.Errorf("answer 1 = %q, want B", got)
	}
	if got := lastToolMessage(t, srv, 2); got != "my words" {
		t.Errorf("answer 2 = %q, want my words", got)
	}
	recs := jsonLines(t, res.stdout)
	if qs := questionTexts(recs); len(qs) != 2 || qs[0] != "Pick?" || qs[1] != "Why?" {
		t.Errorf("question records = %v", qs)
	}
	if r := lastResult(t, recs); r["reason"] != "done" || r["final_text"] != "end" {
		t.Errorf("result = %v", r)
	}
	checkNoSecret(t, res)
}

func TestHeadlessExit3(t *testing.T) {
	srv := providertest.New(
		headlessQuestion("q1", "Pick?", []string{"A", "B"}),
		providertest.Response{Text: "never"},
	)
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--json")
	if res.code != 3 {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	recs := jsonLines(t, res.stdout)
	r := lastResult(t, recs)
	if r["exit_code"] != float64(3) || r["reason"] != "question" {
		t.Errorf("result = %v", r)
	}
	if qs := questionTexts(recs); len(qs) != 1 || !strings.Contains(qs[0], "Pick?") {
		t.Errorf("question records = %v", qs)
	}
	if n := len(srv.Requests()); n != 1 {
		t.Errorf("requests = %d, want 1", n)
	}

	// Without --json: question line and the unanswered marker on stderr.
	srv2 := providertest.New(headlessQuestion("q1", "Pick?", []string{"A", "B"}), providertest.Response{Text: "never"})
	defer srv2.Close()
	home, dir = headlessDirs(t, srv2)
	res = runYip(t, home, dir, "", headlessEnv, "-p", "go")
	if res.code != 3 {
		t.Fatalf("plain: code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "question: Pick?") || !strings.Contains(res.stderr, "yip: unanswered question") {
		t.Errorf("plain stderr = %q", res.stderr)
	}
	if n := len(srv2.Requests()); n != 1 {
		t.Errorf("plain requests = %d, want 1", n)
	}
	checkNoSecret(t, res)
}

func TestHeadlessExit3Extra(t *testing.T) {
	srv := providertest.New(
		headlessQuestion("q1", "Pick?", []string{"A", "B"}),
		headlessQuestion("q2", "Again?", []string{"C", "D"}),
		providertest.Response{Text: "never"},
	)
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--answer", "A", "--json")
	if res.code != 3 {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	if got := lastToolMessage(t, srv, 1); got != "A" {
		t.Errorf("answer 1 = %q, want A", got)
	}
	if n := len(srv.Requests()); n != 2 {
		t.Errorf("requests = %d, want 2", n)
	}
	recs := jsonLines(t, res.stdout)
	if r := lastResult(t, recs); r["exit_code"] != float64(3) || r["reason"] != "question" {
		t.Errorf("result = %v", r)
	}
	if qs := questionTexts(recs); len(qs) != 2 {
		t.Errorf("question records = %v", qs)
	}
}
