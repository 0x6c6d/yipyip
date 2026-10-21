package provider_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"yipyip/internal/config"
	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

// clientFor builds a client for baseURL (allowlisted) with the fake key.
func clientFor(t *testing.T, baseURL string, red *config.Redactor) *provider.Client {
	t.Helper()
	hc, err := provider.NewHTTPClient([]string{baseURL})
	if err != nil {
		t.Fatal(err)
	}
	return provider.NewClient(baseURL, "SECRET123", hc, red)
}

// lastErr asserts that evs ends with exactly one final event, an EventError,
// and returns its error.
func lastErr(t *testing.T, evs []provider.Event) error {
	t.Helper()
	if len(evs) == 0 {
		t.Fatal("no events")
	}
	for i, ev := range evs[:len(evs)-1] {
		if ev.Kind == provider.EventDone || ev.Kind == provider.EventError {
			t.Fatalf("final event at %d of %d: %+v", i, len(evs), ev)
		}
	}
	ev := evs[len(evs)-1]
	if ev.Kind != provider.EventError || ev.Err == nil {
		t.Fatalf("last event = %+v, want EventError", ev)
	}
	if strings.Contains(ev.Err.Error(), "SECRET123") {
		t.Fatalf("error leaks the key: %q", ev.Err.Error())
	}
	return ev.Err
}

func TestHTTP500(t *testing.T) {
	body := "internal\n\n   server\t error " + strings.Repeat("x", 600)
	// 5xx is retried: every attempt fails, the last error is reported.
	srv := providertest.NewFunc(func(int, providertest.Request) providertest.Response {
		return providertest.Response{Status: 500, Body: body}
	})
	defer srv.Close()

	err := lastErr(t, collect(t, clientFor(t, srv.URL, nil).Stream(context.Background(), userReq())))
	msg := err.Error()
	if !strings.HasPrefix(msg, "HTTP 500: internal server error xxx") {
		t.Errorf("message = %q", msg)
	}
	if got := strings.TrimPrefix(msg, "HTTP 500: "); len([]rune(got)) != 500 {
		t.Errorf("excerpt has %d characters, want 500", len([]rune(got)))
	}

	// Empty body: just the status.
	srv2 := providertest.NewFunc(func(int, providertest.Request) providertest.Response {
		return providertest.Response{Status: 502}
	})
	defer srv2.Close()
	if msg := lastErr(t, collect(t, clientFor(t, srv2.URL, nil).Stream(context.Background(), userReq()))).Error(); msg != "HTTP 502" {
		t.Errorf("empty body message = %q", msg)
	}
}

func TestHTTP401(t *testing.T) {
	srv := providertest.New(providertest.Response{Status: 401, Body: `{"error":{"message":"No auth credentials found","code":401}}`})
	defer srv.Close()

	evs := collect(t, clientFor(t, srv.URL, config.NewRedactor("SECRET123")).Stream(context.Background(), userReq()))
	if len(evs) != 1 {
		t.Fatalf("events = %+v, want a single EventError", evs)
	}
	msg := lastErr(t, evs).Error()
	if msg != `HTTP 401: {"error":{"message":"No auth credentials found","code":401}}` {
		t.Errorf("message = %q", msg)
	}
}

func TestSecretProviderError(t *testing.T) {
	echo := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"invalid key: `+r.Header.Get("Authorization")+`", "key":"SECRET123", "other":"OTHERSECRET"}`)
	}
	srv := httptest.NewServer(http.HandlerFunc(echo))
	defer srv.Close()

	for _, tc := range []struct {
		name      string
		red       *config.Redactor
		maskOther bool
	}{
		{"nil redactor", nil, false},
		{"empty redactor", config.NewRedactor(), false},
		{"redactor", config.NewRedactor("SECRET123", "OTHERSECRET"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := lastErr(t, collect(t, clientFor(t, srv.URL, tc.red).Stream(context.Background(), userReq()))).Error()
			if !strings.Contains(msg, "401") || !strings.Contains(msg, "Bearer [redacted]") {
				t.Errorf("message = %q", msg)
			}
			if tc.maskOther && strings.Contains(msg, "OTHERSECRET") {
				t.Errorf("configured secret leaked: %q", msg)
			}
		})
	}

	// The key straddling the 500-character cut leaves no fragment.
	cut := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, strings.Repeat("a", 495)+"SECRET123 tail")
	}))
	defer cut.Close()
	msg := lastErr(t, collect(t, clientFor(t, cut.URL, nil).Stream(context.Background(), userReq()))).Error()
	if strings.Contains(msg, "SECRE") {
		t.Errorf("key fragment in excerpt: %q", msg)
	}
}

func TestDroppedStream(t *testing.T) {
	// Abort after content: no finish_reason, no [DONE].
	srv := providertest.New(
		providertest.Response{Text: "partial text", Drop: true},
		providertest.Response{Text: "x", ToolCalls: []providertest.ToolCall{{ID: "c1", Name: "bash", Arguments: `{"command":"ls -la"}`}}, Drop: true},
		providertest.Response{Raw: []string{`{"choices":[{"index":0,"delta":{"content":"eof"}}]}`}},
		providertest.Response{Raw: []string{`{"choices":[{"index":0,"delta":{"content":"fin"},"finish_reason":"stop"}]}`}},
	)
	defer srv.Close()
	c := clientFor(t, srv.URL, nil)

	evs := collect(t, c.Stream(context.Background(), userReq()))
	err := lastErr(t, evs)
	if !strings.HasPrefix(err.Error(), "stream interrupted") {
		t.Errorf("error = %q", err)
	}
	var text string
	for _, ev := range evs {
		if ev.Kind == provider.EventText {
			text += ev.Text
		}
	}
	if text != "partial text" {
		t.Errorf("text before the drop = %q", text)
	}

	// Partial tool calls of a dropped stream are never emitted.
	evs = collect(t, c.Stream(context.Background(), userReq()))
	lastErr(t, evs)
	for _, ev := range evs {
		if ev.Kind == provider.EventToolCall {
			t.Errorf("tool call from a dropped stream: %+v", ev)
		}
	}

	// Clean EOF without finish_reason or [DONE] is also an interruption.
	if err := lastErr(t, collect(t, c.Stream(context.Background(), userReq()))); !strings.HasPrefix(err.Error(), "stream interrupted") {
		t.Errorf("EOF error = %q", err)
	}

	// EOF after a finish_reason counts as done.
	evs = collect(t, c.Stream(context.Background(), userReq()))
	if last := evs[len(evs)-1]; last.Kind != provider.EventDone {
		t.Errorf("EOF after finish_reason: last = %+v", last)
	}
}

func TestMalformedChunk(t *testing.T) {
	srv := providertest.New(
		providertest.Response{Raw: []string{`{"choices":[{"index":0,"delta":{"content":"ok"}}]}`, `{not json`, `{"choices":[{"index":0,"delta":{"content":"never"}}]}`, "[DONE]"}},
		providertest.Response{Raw: []string{`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"bash","arguments":"{\"co"}}]}}]}`, `[1,2`, "[DONE]"}},
	)
	defer srv.Close()
	c := clientFor(t, srv.URL, nil)

	evs := collect(t, c.Stream(context.Background(), userReq()))
	err := lastErr(t, evs)
	if !strings.HasPrefix(err.Error(), "invalid stream chunk") {
		t.Errorf("error = %q", err)
	}
	if len(evs) != 2 || evs[0].Kind != provider.EventText || evs[0].Text != "ok" {
		t.Errorf("events = %+v", evs)
	}

	evs = collect(t, c.Stream(context.Background(), userReq()))
	if len(evs) != 1 {
		t.Errorf("partial tool call emitted: %+v", evs)
	}
	lastErr(t, evs)
}

func TestCancelStream(t *testing.T) {
	// The server sends one delta and then keeps the connection open.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	defer srv.Close()
	c := clientFor(t, srv.URL, config.NewRedactor("SECRET123"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := c.Stream(ctx, userReq())
	if ev := <-ch; ev.Kind != provider.EventText {
		t.Fatalf("first event = %+v", ev)
	}
	cancel()
	start := time.Now()
	evs := collect(t, ch)
	if d := time.Since(start); d > time.Second {
		t.Errorf("channel closed %v after cancel", d)
	}
	if err := lastErr(t, evs); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}

	// Cancelled while waiting for the response headers.
	slow := providertest.New(providertest.Response{Delay: 10 * time.Second, Text: "late"})
	defer slow.Close()
	ctx2, cancel2 := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel2)
	start = time.Now()
	evs = collect(t, clientFor(t, slow.URL, nil).Stream(ctx2, userReq()))
	if d := time.Since(start); d > time.Second+50*time.Millisecond {
		t.Errorf("channel closed %v after start", d)
	}
	if err := lastErr(t, evs); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}

	// Already cancelled: still exactly one final EventError.
	ctx3, cancel3 := context.WithCancel(context.Background())
	cancel3()
	if err := lastErr(t, collect(t, clientFor(t, slow.URL, nil).Stream(ctx3, userReq()))); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestRefusedHostError(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()

	hc, err := provider.NewHTTPClient([]string{"http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	// Credentials in the URL must not appear in the error text either.
	u := strings.Replace(srv.URL, "http://", "http://user:SECRET123@", 1) + "/v1"
	c := provider.NewClient(u, "SECRET123", hc, nil)
	err = lastErr(t, collect(t, c.Stream(context.Background(), userReq())))
	if !errors.Is(err, provider.ErrHostNotAllowed) {
		t.Errorf("error = %v, want ErrHostNotAllowed", err)
	}
	if hits.Load() != 0 {
		t.Errorf("refused host was reached %d times", hits.Load())
	}

	// A connection error (nothing listens) is an EventError too, without the key.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	hc2, err := provider.NewHTTPClient([]string{deadURL})
	if err != nil {
		t.Fatal(err)
	}
	lastErr(t, collect(t, provider.NewClient(deadURL, "SECRET123", hc2, nil).Stream(context.Background(), userReq())))
}
