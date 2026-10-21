package provider_test

import (
	"bytes"
	"context"
	"testing"

	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

// streamedText concatenates the text events of evs.
func streamedText(evs []provider.Event) string {
	var s string
	for _, ev := range evs {
		if ev.Kind == provider.EventText {
			s += ev.Text
		}
	}
	return s
}

// scriptedAttempts drives n attempts (one Stream call each, same request) at a
// fake that fails the first fails attempts in mode, then streams "the answer".
// It asserts that every failed attempt ends in an error event, that the
// attempt after the failures streams the answer, and that every attempt was
// recorded with an identical body.
func scriptedAttempts(t *testing.T, mode providertest.FailMode, fails int) {
	t.Helper()
	srv := providertest.NewWithFailures(providertest.Failures{Mode: mode, Count: fails}, providertest.Response{Text: "the answer"})
	defer srv.Close()
	c := newTestClient(t, srv, "SECRET123")

	for i := 0; i < fails; i++ {
		lastErr(t, collect(t, c.Stream(context.Background(), userReq())))
	}
	evs := collect(t, c.Stream(context.Background(), userReq()))
	if last := evs[len(evs)-1]; last.Kind != provider.EventDone {
		t.Fatalf("last event = %+v, want EventDone", last)
	}
	if got := streamedText(evs); got != "the answer" {
		t.Fatalf("streamed %q", got)
	}

	reqs := srv.Requests()
	if len(reqs) != fails+1 {
		t.Fatalf("recorded %d requests, want %d", len(reqs), fails+1)
	}
	for i, r := range reqs {
		if !bytes.Equal(r.Body, reqs[0].Body) {
			t.Fatalf("request %d body differs from request 0", i)
		}
	}
}

func TestFakeConnectionResetScripted(t *testing.T) {
	scriptedAttempts(t, providertest.FailReset, 9)

	// The current client makes a single attempt: it ends in an error event.
	srv := providertest.NewWithFailures(providertest.Failures{Mode: providertest.FailReset, Count: 9})
	defer srv.Close()
	lastErr(t, collect(t, newTestClient(t, srv, "SECRET123").Stream(context.Background(), userReq())))
	if n := len(srv.Requests()); n < 1 || n > 9 {
		t.Fatalf("recorded %d requests", n)
	}
}

func TestFake429Scripted(t *testing.T) {
	scriptedAttempts(t, providertest.Fail429, 9)

	srv := providertest.NewWithFailures(providertest.Failures{Mode: providertest.Fail429, Count: 9, RetryAfter: "1"})
	defer srv.Close()
	err := lastErr(t, collect(t, newTestClient(t, srv, "SECRET123").Stream(context.Background(), userReq())))
	if got := err.Error(); len(got) < 8 || got[:8] != "HTTP 429" {
		t.Fatalf("error = %q", got)
	}
	if n := len(srv.Requests()); n != 1 {
		t.Fatalf("recorded %d requests, want 1 (client does not retry yet)", n)
	}
}

func TestFake503Scripted(t *testing.T) {
	scriptedAttempts(t, providertest.Fail503, 9)

	srv := providertest.NewWithFailures(providertest.Failures{Mode: providertest.Fail503, Count: 9})
	defer srv.Close()
	err := lastErr(t, collect(t, newTestClient(t, srv, "SECRET123").Stream(context.Background(), userReq())))
	if got := err.Error(); len(got) < 8 || got[:8] != "HTTP 503" {
		t.Fatalf("error = %q", got)
	}
	if n := len(srv.Requests()); n != 1 {
		t.Fatalf("recorded %d requests, want 1 (client does not retry yet)", n)
	}
}

func TestFakeRecordsEachAttempt(t *testing.T) {
	srv := providertest.NewWithFailures(providertest.Failures{Mode: providertest.Fail503, Count: 2})
	defer srv.Close()
	c := newTestClient(t, srv, "SECRET123")
	for i := 0; i < 3; i++ {
		evs := collect(t, c.Stream(context.Background(), userReq()))
		if ok := evs[len(evs)-1].Kind == provider.EventDone; ok != (i == 2) {
			t.Fatalf("attempt %d: done=%v", i, ok)
		}
	}
	reqs := srv.Requests()
	if len(reqs) != 3 {
		t.Fatalf("recorded %d requests, want 3", len(reqs))
	}
	if len(reqs[0].Body) == 0 {
		t.Fatal("empty body recorded")
	}
	for i, r := range reqs {
		if !bytes.Equal(r.Body, reqs[0].Body) {
			t.Fatalf("request %d body differs", i)
		}
	}
}
