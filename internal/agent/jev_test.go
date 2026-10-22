package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

var jevTestQs = []provider.JevQuestion{{Name: "r", Type: provider.JevNoul, Instructions: "risky?"}}

// jevFake returns a jev fake that always replies resp.
func jevFake(resp providertest.JevResponse) *providertest.JevServer {
	return providertest.NewJev(func(int, providertest.JevRequest) providertest.JevResponse { return resp })
}

func jevConfigJSON(j *providertest.JevServer) string {
	return `{"baseURL":"` + j.URL + `","apiKey":"{env:TYPESAFE_API_KEY}"}`
}

func TestJevSessionToggle(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	j := jevFake(providertest.JevResponse{})
	defer j.Close()

	s := newJevSession(t, srv, jevConfigJSON(j))
	if !s.JevConfigured() {
		t.Fatal("not configured")
	}
	if s.JevOn() {
		t.Fatal("jev on at start")
	}
	if err := s.SetJev(true); err != nil || !s.JevOn() {
		t.Fatalf("SetJev(true): %v, on=%v", err, s.JevOn())
	}
	s.Reset()
	if !s.JevOn() {
		t.Fatal("Reset switched jev off")
	}
	if err := s.SetJev(false); err != nil || s.JevOn() {
		t.Fatalf("SetJev(false): %v, on=%v", err, s.JevOn())
	}

	for name, cfg := range map[string]string{"unconfigured": "", "no key": `{"baseURL":"` + j.URL + `"}`} {
		u := newJevSession(t, srv, cfg)
		if u.JevConfigured() {
			t.Fatalf("%s: configured", name)
		}
		if err := u.SetJev(true); !errors.Is(err, ErrJevNotConfigured) || u.JevOn() {
			t.Fatalf("%s: SetJev(true) = %v, on=%v", name, err, u.JevOn())
		}
		if err := u.SetJev(false); err != nil {
			t.Fatalf("%s: SetJev(false) = %v", name, err)
		}
	}
}

func TestJevAskRedacts(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	j := jevFake(providertest.JevResponse{
		Answers: map[string]any{"r": providertest.JevNoulAnswer(0.4)},
		Usage:   &providertest.JevUsage{Input: 21, Output: 2},
	})
	defer j.Close()
	s := newJevSession(t, srv, jevConfigJSON(j))
	if err := s.SetJev(true); err != nil {
		t.Fatal(err)
	}

	state := "token SECRET123 key jev-session-key " + strings.Repeat("ä", 3000)
	ans, err := s.askJev(context.Background(), state, jevTestQs)
	if err != nil || len(ans) != 1 || ans[0].Noul != 0.4 {
		t.Fatalf("askJev: %+v, %v", ans, err)
	}
	reqs := j.Requests()
	if len(reqs) != 1 {
		t.Fatalf("%d jev requests", len(reqs))
	}
	got := reqs[0].State
	if !strings.HasPrefix(got, "token [redacted]") || strings.Contains(got, "jev-session-key") {
		t.Fatalf("state not redacted: %.60q", got)
	}
	if n := utf8.RuneCountInString(got); n > JevStateLimit {
		t.Fatalf("state has %d runes", n)
	}
	if strings.Contains(string(reqs[0].Body), "SECRET123") {
		t.Fatal("secret in jev request body")
	}
	if st := s.JevStats(); st.InputTokens != 21 {
		t.Fatalf("InputTokens = %d", st.InputTokens)
	}

	// The cut happens after redaction: a secret straddling the limit is not
	// left half visible.
	if _, err := s.askJev(context.Background(), strings.Repeat("y", JevStateLimit-4)+"SECRET123", jevTestQs); err != nil {
		t.Fatal(err)
	}
	if r := j.Requests(); len(r) != 2 || strings.Contains(string(r[1].Body), "SECR") {
		t.Fatalf("partial secret sent: %d requests", len(r))
	}

	s.noteJev(true)
	s.noteJev(false)
	s.noteJev(false)
	if st := s.JevStats(); st.Answered != 1 || st.Fallbacks != 2 || st.InputTokens != 42 {
		t.Fatalf("stats: %+v", st)
	}
}

func TestJevAskOff(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	j := jevFake(providertest.JevResponse{Answers: map[string]any{"r": providertest.JevNoulAnswer(0.4)}})
	defer j.Close()

	s := newJevSession(t, srv, jevConfigJSON(j))
	if _, err := s.askJev(context.Background(), "state", jevTestQs); !errors.Is(err, ErrJevOff) {
		t.Fatalf("off: %v", err)
	}
	if err := s.SetJev(true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetJev(false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.askJev(context.Background(), "state", jevTestQs); !errors.Is(err, ErrJevOff) {
		t.Fatalf("switched off: %v", err)
	}
	u := newJevSession(t, srv, "")
	if _, err := u.askJev(context.Background(), "state", jevTestQs); !errors.Is(err, ErrJevOff) {
		t.Fatalf("unconfigured: %v", err)
	}
	if n := len(j.Requests()); n != 0 {
		t.Fatalf("%d jev requests while off", n)
	}
}

func TestJevAskTimeout(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	j := jevFake(providertest.JevResponse{Delay: 5 * time.Second, Answers: map[string]any{"r": providertest.JevNoulAnswer(0.4)}})
	defer j.Close()
	s := newJevSession(t, srv, jevConfigJSON(j))
	s.SetJevTimeout(150 * time.Millisecond)
	if err := s.SetJev(true); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := s.askJev(context.Background(), "state", jevTestQs)
	if err == nil {
		t.Fatal("no error after timeout")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("askJev took %v", d)
	}
}

func TestJevStatsTokens(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "hi", Usage: &providertest.Usage{Prompt: 12, Completion: 5}})
	defer srv.Close()
	s := newJevSession(t, srv, "")
	evs := submitAndWait(t, s, "hello")
	if !findEvent(evs, EvTurnDone, "") {
		t.Fatalf("turn did not finish: %+v", evs)
	}
	st := s.JevStats()
	if st.OtherTokens != 17 || st.QuestionTokens != 0 || st.RoutingTokens != 0 {
		t.Fatalf("stats: %+v", st)
	}
	s.Reset()
	if got := s.JevStats(); got != st {
		t.Fatalf("Reset changed stats: %+v", got)
	}
}
