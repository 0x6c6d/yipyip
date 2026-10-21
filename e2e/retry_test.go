package e2e

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"yipyip/internal/provider/providertest"
)

// waitLog is a retry wait stub that records the requested waits and returns
// at once (or after gate is closed, when set).
type waitLog struct {
	mu      sync.Mutex
	waits   []time.Duration
	waiting chan struct{} // receives one value per wait that started (when set)
	gate    chan struct{} // the wait blocks until it is closed (when set)
}

func (w *waitLog) wait(ctx context.Context, d time.Duration) error {
	w.mu.Lock()
	w.waits = append(w.waits, d)
	w.mu.Unlock()
	if w.waiting != nil {
		select {
		case w.waiting <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if w.gate != nil {
		select {
		case <-w.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return ctx.Err()
}

func (w *waitLog) seen() []time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]time.Duration(nil), w.waits...)
}

// retryEnv builds an environment around srv with the retry wait stubbed by w.
func retryEnv(t *testing.T, srv *providertest.Server, w *waitLog) *env {
	t.Helper()
	e := newEnvServer(t, srv, envOpts{contextLimit: 8000})
	e.Session.SetRetryWait(w.wait)
	return e
}

func TestRetryTwoFailuresThenStream(t *testing.T) {
	srv := providertest.NewWithFailures(providertest.Failures{Mode: providertest.Fail503, Count: 2},
		resp{Text: "answer after retries"})
	w := &waitLog{}
	e := retryEnv(t, srv, w)
	e.submit(t, "hello there")

	v := viewText(e.Model)
	if !strings.Contains(v, "answer after retries") || !strings.Contains(v, "Worked for") {
		t.Errorf("turn did not finish with the answer:\n%s", v)
	}
	if strings.Contains(v, "retrying") || strings.Contains(v, "error:") {
		t.Errorf("retry status or error left in the view:\n%s", v)
	}
	reqs := e.Server.Requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	for i := 1; i < len(reqs); i++ {
		if !bytes.Equal(reqs[0].Body, reqs[i].Body) {
			t.Errorf("request %d body differs from request 0", i)
		}
	}
	got := w.seen()
	if len(got) != 2 || got[0] != time.Second || got[1] != 2*time.Second {
		t.Errorf("waits = %v, want [1s 2s]", got)
	}
}

func TestRetryStatusVisibleWhileWaiting(t *testing.T) {
	srv := providertest.NewWithFailures(providertest.Failures{Mode: providertest.Fail503, Count: 1},
		resp{Text: "streamed answer"})
	w := &waitLog{waiting: make(chan struct{}, 1), gate: make(chan struct{})}
	e := retryEnv(t, srv, w)
	m, _ := e.Model.Command("hello there")
	e.Model = m

	select {
	case <-w.waiting:
	case <-time.After(10 * time.Second):
		t.Fatal("the retry wait never started")
	}
	const want = "retrying (2/10) in 1s"
	deadline := time.After(10 * time.Second)
	for !strings.Contains(viewText(e.Model), want) {
		select {
		case <-deadline:
			t.Fatalf("%q never shown while waiting:\n%s", want, viewText(e.Model))
		default:
		}
		e.Model = e.Model.Drain()
	}
	if got := len(e.Server.Requests()); got != 1 {
		t.Errorf("requests = %d while waiting, want 1", got)
	}

	close(w.gate)
	waitSession(t, e.Session)
	e.Model = e.Model.Drain()
	v := viewText(e.Model)
	if strings.Contains(v, "retrying") {
		t.Errorf("retry status still shown after the answer streamed:\n%s", v)
	}
	if !strings.Contains(v, "streamed answer") {
		t.Errorf("answer missing:\n%s", v)
	}
	if got := len(e.Server.Requests()); got != 2 {
		t.Errorf("requests = %d, want 2", got)
	}
}

func TestRetryNo400RetryEndToEnd(t *testing.T) {
	srv := providertest.New(resp{Status: 400, Body: "bad request mentioning " + secret})
	w := &waitLog{}
	e := retryEnv(t, srv, w)
	e.submit(t, "hello there")

	if got := len(e.Server.Requests()); got != 1 {
		t.Errorf("requests = %d, want exactly 1 (no retry of a 400)", got)
	}
	v := viewText(e.Model)
	if !strings.Contains(v, "error:") {
		t.Errorf("no error line in the view:\n%s", v)
	}
	if strings.Contains(v, "retrying") {
		t.Errorf("retry text in the view:\n%s", v)
	}
	if strings.Contains(v, secret) {
		t.Errorf("error line is not redacted:\n%s", v)
	}
	if got := w.seen(); len(got) != 0 {
		t.Errorf("waits = %v, want none", got)
	}
}

func TestRetryGivesUpAfterTenAttempts(t *testing.T) {
	srv := providertest.NewWithFailures(providertest.Failures{Mode: providertest.Fail503, Count: 10})
	w := &waitLog{}
	e := retryEnv(t, srv, w)
	e.submit(t, "hello there")

	if got := len(e.Server.Requests()); got != 10 {
		t.Errorf("requests = %d, want exactly 10", got)
	}
	if got := len(w.seen()); got != 9 {
		t.Errorf("waits = %d, want 9", got)
	}
	v := viewText(e.Model)
	if !strings.Contains(v, "error:") {
		t.Errorf("no error line in the view:\n%s", v)
	}
	if strings.Contains(v, "retrying") {
		t.Errorf("retry status left in the view after giving up:\n%s", v)
	}
	if strings.Contains(v, secret) {
		t.Errorf("secret in the view:\n%s", v)
	}
}
