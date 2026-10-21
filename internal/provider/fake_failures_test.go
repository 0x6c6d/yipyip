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

// scriptedAttempts drives one Stream call at a fake that fails the first
// fails attempts in mode, then streams "the answer". The client retries the
// failures itself: the call streams the answer, and every attempt was
// recorded with an identical body.
func scriptedAttempts(t *testing.T, mode providertest.FailMode, fails int) {
	t.Helper()
	srv := providertest.NewWithFailures(providertest.Failures{Mode: mode, Count: fails}, providertest.Response{Text: "the answer"})
	defer srv.Close()
	c := newTestClient(t, srv, "SECRET123")

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

// exhaustedAttempts streams once at a fake that always fails in mode: the
// client gives up after provider.MaxAttempts attempts with an error event.
func exhaustedAttempts(t *testing.T, f providertest.Failures) error {
	t.Helper()
	f.Count = 100
	srv := providertest.NewWithFailures(f)
	defer srv.Close()
	err := lastErr(t, collect(t, newTestClient(t, srv, "SECRET123").Stream(context.Background(), userReq())))
	if n := len(srv.Requests()); n != provider.MaxAttempts {
		t.Fatalf("recorded %d requests, want %d", n, provider.MaxAttempts)
	}
	return err
}

func TestFakeConnectionResetScripted(t *testing.T) {
	scriptedAttempts(t, providertest.FailReset, 9)
	exhaustedAttempts(t, providertest.Failures{Mode: providertest.FailReset})
}

func TestFake429Scripted(t *testing.T) {
	scriptedAttempts(t, providertest.Fail429, 9)

	err := exhaustedAttempts(t, providertest.Failures{Mode: providertest.Fail429, RetryAfter: "1"})
	if got := err.Error(); len(got) < 8 || got[:8] != "HTTP 429" {
		t.Fatalf("error = %q", got)
	}
}

func TestFake503Scripted(t *testing.T) {
	scriptedAttempts(t, providertest.Fail503, 9)

	err := exhaustedAttempts(t, providertest.Failures{Mode: providertest.Fail503})
	if got := err.Error(); len(got) < 8 || got[:8] != "HTTP 503" {
		t.Fatalf("error = %q", got)
	}
}

func TestFakeRecordsEachAttempt(t *testing.T) {
	srv := providertest.NewWithFailures(providertest.Failures{Mode: providertest.Fail503, Count: 2})
	defer srv.Close()
	c := newTestClient(t, srv, "SECRET123")
	evs := collect(t, c.Stream(context.Background(), userReq()))
	if last := evs[len(evs)-1]; last.Kind != provider.EventDone {
		t.Fatalf("last event = %+v, want EventDone", last)
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
