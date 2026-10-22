package provider_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

const jevKey = "jev-test-key-77"

func jevQuestions() []provider.JevQuestion {
	return []provider.JevQuestion{
		{Name: "next", Type: provider.JevChoice, Instructions: "pick the next step", Choices: map[string]string{"edit": "Edit a file", "ask": "Ask the user"}},
		{Name: "done", Type: provider.JevScore, Instructions: "how complete", Levels: []string{"none", "some", "all"}},
		{Name: "stuck", Type: provider.JevNoul, Instructions: "is the agent stuck?"},
	}
}

func jevAnswers() map[string]any {
	return map[string]any{
		"next":  providertest.JevChoiceAnswer("ask", 0.9),
		"done":  providertest.JevScoreAnswer(2.5, 0.75),
		"stuck": providertest.JevNoulAnswer(0.2),
	}
}

func jevServe(resp providertest.JevResponse) *providertest.JevServer {
	return providertest.NewJev(func(int, providertest.JevRequest) providertest.JevResponse { return resp })
}

func jevClient(t *testing.T, srv *providertest.JevServer, allow []string) *provider.JevClient {
	t.Helper()
	hc, err := provider.NewHTTPClient(allow)
	if err != nil {
		t.Fatal(err)
	}
	return provider.NewJevClient(hc, srv.URL+"/", jevKey, "jev-1")
}

// jevMustFail asks once and checks that the error wraps ErrJev, contains
// want and never the key.
func jevMustFail(t *testing.T, srv *providertest.JevServer, want string) error {
	t.Helper()
	ans, _, err := jevClient(t, srv, []string{srv.URL}).Ask(context.Background(), "s", jevQuestions())
	if err == nil || !errors.Is(err, provider.ErrJev) || ans != nil {
		t.Fatalf("want ErrJev, got %v, %+v", err, ans)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q lacks %q", err, want)
	}
	if strings.Contains(err.Error(), jevKey) {
		t.Fatalf("key in error %q", err)
	}
	return err
}

func TestJevRequestShape(t *testing.T) {
	srv := jevServe(providertest.JevResponse{Answers: jevAnswers(), Usage: &providertest.JevUsage{Input: 1, Output: 1}})
	defer srv.Close()
	if _, _, err := jevClient(t, srv, []string{srv.URL}).Ask(context.Background(), "STATE", jevQuestions()); err != nil {
		t.Fatal(err)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	r := reqs[0]
	if r.Method != "POST" || r.Path != "/v1/systemone" {
		t.Fatalf("method/path: %s %s", r.Method, r.Path)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer "+jevKey {
		t.Fatalf("auth header %q", got)
	}
	if got := r.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content type %q", got)
	}
	if r.Model != "jev-1" || r.State != "STATE" || len(r.Questions) != 3 {
		t.Fatalf("body: %s", r.Body)
	}
	next, _ := r.Questions["next"].(map[string]any)
	crit, _ := next["criteria"].(map[string]any)
	if next["type"] != "choice" || next["instructions"] != "pick the next step" || crit["edit"] != "Edit a file" || crit["ask"] != "Ask the user" {
		t.Fatalf("choice question: %v", next)
	}
	done, _ := r.Questions["done"].(map[string]any)
	lv, _ := done["criteria"].([]any)
	if done["type"] != "score" || len(lv) != 3 || lv[0] != "none" || lv[1] != "some" || lv[2] != "all" {
		t.Fatalf("score question: %v", done)
	}
	stuck, _ := r.Questions["stuck"].(map[string]any)
	if stuck["type"] != "noul" || stuck["instructions"] != "is the agent stuck?" {
		t.Fatalf("noul question: %v", stuck)
	}
	if _, ok := stuck["criteria"]; ok {
		t.Fatalf("noul question has criteria: %v", stuck)
	}
}

func TestJevAnswerTypes(t *testing.T) {
	srv := jevServe(providertest.JevResponse{Answers: jevAnswers(), Usage: &providertest.JevUsage{Input: 120, Output: 7}})
	defer srv.Close()
	ans, u, err := jevClient(t, srv, []string{srv.URL}).Ask(context.Background(), "s", jevQuestions())
	if err != nil {
		t.Fatal(err)
	}
	want := []provider.JevAnswer{
		{Name: "next", Type: provider.JevChoice, Choice: "ask", Confidence: 0.9},
		{Name: "done", Type: provider.JevScore, Score: 2.5, Confidence: 0.75},
		{Name: "stuck", Type: provider.JevNoul, Noul: 0.2},
	}
	if len(ans) != len(want) {
		t.Fatalf("answers: %+v", ans)
	}
	for i := range want {
		if ans[i] != want[i] {
			t.Fatalf("answer %d: %+v, want %+v", i, ans[i], want[i])
		}
	}
	if u != (provider.JevUsage{Input: 120, Output: 7}) {
		t.Fatalf("usage %+v", u)
	}

	// Without usage both counts are estimated from the bodies.
	srv2 := jevServe(providertest.JevResponse{Answers: jevAnswers()})
	defer srv2.Close()
	_, u, err = jevClient(t, srv2, []string{srv2.URL}).Ask(context.Background(), "s", jevQuestions())
	if err != nil {
		t.Fatal(err)
	}
	reqLen := len(srv2.Requests()[0].Body)
	if u.Input != max(1, reqLen/4) || u.Output < 1 {
		t.Fatalf("estimated usage %+v (request %d bytes)", u, reqLen)
	}
}

func TestJev422(t *testing.T) {
	srv := jevServe(providertest.JevResponse{Status: 422})
	defer srv.Close()
	err := jevMustFail(t, srv, "422")
	if !strings.Contains(err.Error(), "invalid question") {
		t.Fatalf("422 error lacks detail msg: %q", err)
	}
}

func TestJevForeignHostRefused(t *testing.T) {
	srv := jevServe(providertest.JevResponse{Answers: jevAnswers()})
	defer srv.Close()
	_, _, err := jevClient(t, srv, []string{"http://127.0.0.1:1"}).Ask(context.Background(), "s", jevQuestions())
	if err == nil || !errors.Is(err, provider.ErrHostNotAllowed) || !errors.Is(err, provider.ErrJev) {
		t.Fatalf("want ErrHostNotAllowed and ErrJev, got %v", err)
	}
	if strings.Contains(err.Error(), jevKey) {
		t.Fatalf("key in error %q", err)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("%d requests reached the foreign host", n)
	}
}

func TestJevBadResponse(t *testing.T) {
	cases := map[string]providertest.JevResponse{
		"500":      {Status: 500},
		"non-JSON": {Body: "not json"},
		"missing": {Answers: func() map[string]any {
			a := jevAnswers()
			delete(a, "stuck")
			return a
		}()},
		"wrong type": {Answers: func() map[string]any {
			a := jevAnswers()
			a["next"] = providertest.JevNoulAnswer(0.5)
			return a
		}()},
		"too large": {Body: `{"pad":"` + strings.Repeat("x", 1<<20) + `"}`},
	}
	for name, resp := range cases {
		t.Run(name, func(t *testing.T) {
			srv := jevServe(resp)
			defer srv.Close()
			jevMustFail(t, srv, "")
		})
	}
}

func TestJevInvalidQuestions(t *testing.T) {
	srv := jevServe(providertest.JevResponse{Answers: jevAnswers()})
	defer srv.Close()
	c := jevClient(t, srv, []string{srv.URL})
	many := make(map[string]string, 256)
	for i := range 256 {
		many[strings.Repeat("c", i+1)] = "d"
	}
	levels := make([]string, 11)
	for i := range levels {
		levels[i] = "l"
	}
	for name, qs := range map[string][]provider.JevQuestion{
		"none":         nil,
		"empty name":   {{Name: "", Type: provider.JevNoul}},
		"duplicate":    {{Name: "d", Type: provider.JevNoul}, {Name: "d", Type: provider.JevNoul}},
		"unknown type": {{Name: "x", Type: "box"}},
		"no choices":   {{Name: "x", Type: provider.JevChoice}},
		"256 choices":  {{Name: "x", Type: provider.JevChoice, Choices: many}},
		"no levels":    {{Name: "x", Type: provider.JevScore}},
		"11 levels":    {{Name: "x", Type: provider.JevScore, Levels: levels}},
	} {
		if _, _, err := c.Ask(context.Background(), "s", qs); err == nil || !errors.Is(err, provider.ErrJev) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("%d requests for invalid questions", n)
	}
}

func TestJevContextTimeout(t *testing.T) {
	srv := jevServe(providertest.JevResponse{Delay: 5 * time.Second, Answers: jevAnswers()})
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := jevClient(t, srv, []string{srv.URL}).Ask(ctx, "s", jevQuestions())
	if err == nil || !errors.Is(err, provider.ErrJev) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want ErrJev and DeadlineExceeded, got %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %v", d)
	}
}
