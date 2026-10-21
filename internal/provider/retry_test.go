package provider_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"yipyip/internal/config"
	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

// wantWaits is the specified wait schedule between the ten attempts.
var wantWaits = []time.Duration{
	time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
	16 * time.Second, 32 * time.Second, 60 * time.Second, 60 * time.Second, 60 * time.Second,
}

// waitRecorder replaces the retry wait for one test: it records each wait
// and returns at once (no test ever sleeps).
type waitRecorder struct {
	mu    sync.Mutex
	waits []time.Duration
	hook  func(n int) // called with the 1-based wait number, may be nil
}

func recordWaits(t *testing.T) *waitRecorder {
	t.Helper()
	w := &waitRecorder{}
	restore := provider.SetRetryWait(func(ctx context.Context, d time.Duration) error {
		w.mu.Lock()
		w.waits = append(w.waits, d)
		n, hook := len(w.waits), w.hook
		w.mu.Unlock()
		if hook != nil {
			hook(n)
		}
		return ctx.Err()
	})
	t.Cleanup(restore)
	return w
}

func (w *waitRecorder) recorded() []time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]time.Duration(nil), w.waits...)
}

func retryEvents(evs []provider.Event) []provider.Retry {
	var rs []provider.Retry
	for _, ev := range evs {
		if ev.Kind == provider.EventRetry {
			rs = append(rs, ev.Retry)
		}
	}
	return rs
}

func sameBodies(t *testing.T, reqs []providertest.Request) {
	t.Helper()
	for i, r := range reqs {
		if len(r.Body) == 0 || !bytes.Equal(r.Body, reqs[0].Body) {
			t.Fatalf("request %d body differs from request 0", i)
		}
	}
}

func equalWaits(got, want []time.Duration) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestRetryNineFailuresThenSuccess(t *testing.T) {
	w := recordWaits(t)
	srv := providertest.NewWithFailures(providertest.Failures{Mode: providertest.FailReset, Count: 9}, providertest.Response{Text: "the answer"})
	defer srv.Close()

	evs := collect(t, newTestClient(t, srv, "SECRET123").Stream(context.Background(), userReq()))
	if last := evs[len(evs)-1]; last.Kind != provider.EventDone {
		t.Fatalf("last event = %+v, want EventDone", last)
	}
	if got := streamedText(evs); got != "the answer" {
		t.Fatalf("streamed %q", got)
	}
	reqs := srv.Requests()
	if len(reqs) != 10 {
		t.Fatalf("recorded %d requests, want 10", len(reqs))
	}
	sameBodies(t, reqs)
	if got := w.recorded(); !equalWaits(got, wantWaits) {
		t.Fatalf("waits = %v, want %v", got, wantWaits)
	}
	if n := len(retryEvents(evs)); n != 9 {
		t.Fatalf("%d retry events, want 9", n)
	}
	for n := 1; n <= 9; n++ {
		if got := provider.RetryDelay(n); got != wantWaits[n-1] {
			t.Errorf("RetryDelay(%d) = %v, want %v", n, got, wantWaits[n-1])
		}
	}

	// Ten failures: the tenth is final, no eleventh attempt and no tenth wait.
	w2 := recordWaits(t)
	srv2 := providertest.NewWithFailures(providertest.Failures{Mode: providertest.FailReset, Count: 10})
	defer srv2.Close()
	lastErr(t, collect(t, newTestClient(t, srv2, "SECRET123").Stream(context.Background(), userReq())))
	if n := len(srv2.Requests()); n != 10 {
		t.Fatalf("recorded %d requests, want 10", n)
	}
	if got := w2.recorded(); !equalWaits(got, wantWaits) {
		t.Fatalf("waits = %v, want %v", got, wantWaits)
	}
}

func TestRetry429And5xxAreRetryable(t *testing.T) {
	w := recordWaits(t)
	statuses := []int{429, 503, 429, 500, 503, 502, 429, 504, 503}
	srv := providertest.NewFunc(func(n int, _ providertest.Request) providertest.Response {
		if n < len(statuses) {
			return providertest.Response{Status: statuses[n], Body: "try later SECRET123", RetryAfter: "1"}
		}
		return providertest.Response{Text: "the answer"}
	})
	defer srv.Close()

	evs := collect(t, newTestClient(t, srv, "SECRET123").Stream(context.Background(), userReq()))
	if last := evs[len(evs)-1]; last.Kind != provider.EventDone {
		t.Fatalf("last event = %+v, want EventDone", last)
	}
	if got := streamedText(evs); got != "the answer" {
		t.Fatalf("streamed %q", got)
	}
	reqs := srv.Requests()
	if len(reqs) != 10 {
		t.Fatalf("recorded %d requests, want 10", len(reqs))
	}
	sameBodies(t, reqs)
	if got := w.recorded(); !equalWaits(got, wantWaits) {
		t.Fatalf("waits = %v, want %v", got, wantWaits)
	}
	for _, r := range retryEvents(evs) {
		if strings.Contains(r.Reason, "SECRET123") {
			t.Fatalf("retry reason leaks the key: %q", r.Reason)
		}
	}
}

func TestNoRetryOn400(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404} {
		w := recordWaits(t)
		srv := providertest.New(providertest.Response{Status: status, Body: "bad request SECRET123"})
		evs := collect(t, newTestClient(t, srv, "SECRET123").Stream(context.Background(), userReq()))
		srv.Close()
		err := lastErr(t, evs) // also asserts the key is redacted
		if want := fmt.Sprintf("HTTP %d: bad request", status); !strings.HasPrefix(err.Error(), want) {
			t.Fatalf("error = %q", err)
		}
		if !strings.Contains(err.Error(), "[redacted]") {
			t.Fatalf("error = %q, want the redacted body", err)
		}
		if n := len(srv.Requests()); n != 1 {
			t.Fatalf("HTTP %d: recorded %d requests, want 1", status, n)
		}
		if rs := retryEvents(evs); len(rs) != 0 {
			t.Fatalf("HTTP %d: retry events %+v", status, rs)
		}
		if len(evs) != 1 {
			t.Fatalf("HTTP %d: events %+v, want a single EventError", status, evs)
		}
		if got := w.recorded(); len(got) != 0 {
			t.Fatalf("HTTP %d: waits %v", status, got)
		}
	}
}

func TestNoRetryAfterFirstByte(t *testing.T) {
	w := recordWaits(t)
	// The first SSE bytes arrive, then the connection breaks.
	srv := providertest.New(
		providertest.Response{Text: "partial", Drop: true},
		providertest.Response{Text: "must not be fetched"},
	)
	defer srv.Close()

	evs := collect(t, newTestClient(t, srv, "SECRET123").Stream(context.Background(), userReq()))
	err := lastErr(t, evs)
	if !strings.HasPrefix(err.Error(), "stream interrupted") {
		t.Fatalf("error = %q", err)
	}
	if got := streamedText(evs); got != "partial" {
		t.Fatalf("streamed %q", got)
	}
	if n := len(srv.Requests()); n != 1 {
		t.Fatalf("recorded %d requests, want 1", n)
	}
	if rs := retryEvents(evs); len(rs) != 0 {
		t.Fatalf("retry events %+v", rs)
	}
	if got := w.recorded(); len(got) != 0 {
		t.Fatalf("waits %v", got)
	}
}

func TestRetryCancellation(t *testing.T) {
	for _, returnErr := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		restore := provider.SetRetryWait(func(c context.Context, d time.Duration) error {
			if d == 2*time.Second { // the 2nd wait
				cancel()
				if returnErr {
					return c.Err()
				}
			}
			return nil
		})
		srv := providertest.NewWithFailures(providertest.Failures{Mode: providertest.Fail503, Count: 10})
		evs := collect(t, newTestClient(t, srv, "SECRET123").Stream(ctx, userReq()))
		n := len(srv.Requests())
		srv.Close()
		restore()
		cancel()

		if err := lastErr(t, evs); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if n > 2 {
			t.Fatalf("recorded %d requests after a cancel in the 2nd wait, want <= 2", n)
		}
	}

	// The real wait returns at once for a cancelled ctx (no sleeping).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := provider.RealRetryWait(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("real wait = %v, want context.Canceled", err)
	}
}

// hostRecorder records the URL host of every request the client sends and
// passes it on to the client's own (host-restricted) transport.
type hostRecorder struct {
	next  http.RoundTripper
	mu    sync.Mutex
	hosts []string
}

func (h *hostRecorder) RoundTrip(r *http.Request) (*http.Response, error) {
	h.mu.Lock()
	h.hosts = append(h.hosts, r.URL.Host)
	h.mu.Unlock()
	return h.next.RoundTrip(r)
}

func TestRetryNeverOtherHost(t *testing.T) {
	recordWaits(t)
	srv := providertest.NewWithFailures(providertest.Failures{Mode: providertest.FailReset, Count: 4}, providertest.Response{Text: "the answer"})
	defer srv.Close()
	u, _ := url.Parse(srv.URL)

	hc, err := provider.NewHTTPClient([]string{srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	rec := &hostRecorder{next: hc.Transport}
	wrapped := *hc
	wrapped.Transport = rec
	c := provider.NewClient(srv.URL, "SECRET123", &wrapped, config.NewRedactor("SECRET123"))

	evs := collect(t, c.Stream(context.Background(), userReq()))
	if last := evs[len(evs)-1]; last.Kind != provider.EventDone {
		t.Fatalf("last event = %+v", last)
	}
	reqs := srv.Requests()
	if len(reqs) != 5 || len(rec.hosts) != 5 {
		t.Fatalf("recorded %d requests, client sent %d, want 5", len(reqs), len(rec.hosts))
	}
	for i := range reqs {
		if reqs[i].Host != u.Host {
			t.Errorf("request %d hit host %q, want %q", i, reqs[i].Host, u.Host)
		}
		if rec.hosts[i] != u.Host {
			t.Errorf("attempt %d addressed host %q, want %q", i, rec.hosts[i], u.Host)
		}
	}

	// Regression guard: a client pointed at a foreign host is refused by the
	// transport once, the refusal is final (never retried) and the foreign
	// server is never reached.
	foreign := providertest.NewWithFailures(providertest.Failures{Mode: providertest.Fail503, Count: 10})
	defer foreign.Close()
	fc := provider.NewClient(foreign.URL, "SECRET123", hc, nil)
	fevs := collect(t, fc.Stream(context.Background(), userReq()))
	if err := lastErr(t, fevs); !errors.Is(err, provider.ErrHostNotAllowed) {
		t.Fatalf("error = %v, want ErrHostNotAllowed", err)
	}
	if rs := retryEvents(fevs); len(rs) != 0 {
		t.Fatalf("refused host retried: %+v", rs)
	}
	if n := len(foreign.Requests()); n != 0 {
		t.Fatalf("foreign host reached %d times", n)
	}
}

func TestRetryEventFields(t *testing.T) {
	recordWaits(t)
	srv := providertest.NewWithFailures(providertest.Failures{Mode: providertest.Fail503, Count: 2}, providertest.Response{Text: "the answer"})
	defer srv.Close()

	evs := collect(t, newTestClient(t, srv, "SECRET123").Stream(context.Background(), userReq()))
	rs := retryEvents(evs)
	if len(rs) != 2 {
		t.Fatalf("retry events = %+v, want 2", rs)
	}
	for i, r := range rs {
		if r.Attempt != i+2 || r.Max != 10 {
			t.Errorf("event %d: attempt %d/%d, want %d/10", i, r.Attempt, r.Max, i+2)
		}
		if !strings.HasPrefix(r.Reason, "HTTP 503") {
			t.Errorf("event %d: reason %q", i, r.Reason)
		}
	}
	if rs[0].Wait != time.Second || rs[1].Wait != 2*time.Second || rs[1].Wait <= rs[0].Wait {
		t.Errorf("waits %v, %v: want 1s then 2s", rs[0].Wait, rs[1].Wait)
	}
	// Status events are non-final and precede the streamed answer.
	if evs[0].Kind != provider.EventRetry || evs[1].Kind != provider.EventRetry {
		t.Errorf("events = %+v", evs)
	}
	if last := evs[len(evs)-1]; last.Kind != provider.EventDone {
		t.Fatalf("last event = %+v", last)
	}
}
