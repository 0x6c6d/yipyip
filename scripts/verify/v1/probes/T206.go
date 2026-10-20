//go:build ignore

// Supervisor probe for T206 (provider errors, dropped stream, redacted error text). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"yipyip/internal/config"
	"yipyip/internal/provider"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

func sse(w http.ResponseWriter, payloads ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, p := range payloads {
		io.WriteString(w, "data: "+p+"\n\n")
		w.(http.Flusher).Flush()
	}
}

// run streams one request and returns the events; it fails when the channel does not close in time.
func run(ctx context.Context, c *provider.Client) []provider.Event {
	ch := c.Stream(ctx, provider.Request{Model: "m1", Messages: []provider.Message{{Role: "user", Content: "hi"}}})
	var evs []provider.Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return evs
			}
			evs = append(evs, ev)
		case <-deadline:
			fmt.Println("FAIL: event channel not closed within 5s")
			os.Exit(1)
		}
	}
}

func last(evs []provider.Event) provider.Event {
	if len(evs) == 0 {
		return provider.Event{}
	}
	return evs[len(evs)-1]
}

func client(url string, allow []string, red *config.Redactor) *provider.Client {
	hc, err := provider.NewHTTPClient(allow)
	if err != nil {
		setup("NewHTTPClient: %v", err)
	}
	return provider.NewClient(url+"/v1", "SECRET123", hc, red)
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("PANIC:", r)
			os.Exit(125)
		}
	}()
	if len(os.Args) != 2 {
		setup("usage: probe <case>")
	}
	var hits atomic.Int64
	var handler http.HandlerFunc
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler(w, r)
	}))
	defer srv.Close()
	ctx := context.Background()

	switch os.Args[1] {
	case "ok":
		handler = func(w http.ResponseWriter, r *http.Request) {
			sse(w, `{"choices":[{"index":0,"delta":{"content":"hel"}}]}`, `{"choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":"stop"}]}`, "[DONE]")
		}
		evs := run(ctx, client(srv.URL, []string{srv.URL}, nil))
		text := ""
		for _, ev := range evs {
			if ev.Kind == provider.EventText {
				text += ev.Text
			}
		}
		if text != "hello" || last(evs).Kind != provider.EventDone {
			fail("text=%q last=%+v", text, last(evs))
		}
		pass("normal stream ends with EventDone")
	case "leak401":
		handler = func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":"invalid key: `+r.Header.Get("Authorization")+` rejected"}`, http.StatusUnauthorized)
		}
		// Redactor nil: the client must still redact its own API key.
		ev := last(run(ctx, client(srv.URL, []string{srv.URL}, nil)))
		if ev.Kind != provider.EventError || ev.Err == nil {
			accepted("401 did not end in EventError: %+v", ev)
		}
		msg := ev.Err.Error()
		if strings.Contains(msg, "SECRET123") || !strings.Contains(msg, "401") {
			accepted("error text must contain 401 and no key: %q", msg)
		}
		rejected("%s", msg)
	case "drop":
		handler = func(w http.ResponseWriter, r *http.Request) {
			sse(w, `{"choices":[{"index":0,"delta":{"content":"par"}}]}`)
			panic(http.ErrAbortHandler)
		}
		evs := run(ctx, client(srv.URL, []string{srv.URL}, nil))
		if len(evs) < 2 || evs[0].Kind != provider.EventText || last(evs).Kind != provider.EventError {
			accepted("dropped stream not reported as error after the text: %+v", evs)
		}
		rejected("%v", last(evs).Err)
	case "malformed":
		handler = func(w http.ResponseWriter, r *http.Request) { sse(w, `{not json`, "[DONE]") }
		ev := last(run(ctx, client(srv.URL, []string{srv.URL}, nil)))
		if ev.Kind != provider.EventError {
			accepted("malformed chunk accepted: %+v", ev)
		}
		rejected("%v", ev.Err)
	case "foreign":
		handler = func(w http.ResponseWriter, r *http.Request) { sse(w, "[DONE]") }
		ev := last(run(ctx, client(srv.URL, []string{"http://127.0.0.1:1"}, nil)))
		if ev.Kind != provider.EventError || hits.Load() != 0 || strings.Contains(fmt.Sprint(ev.Err), "SECRET123") {
			accepted("foreign host: kind=%v hits=%d err=%v", ev.Kind, hits.Load(), ev.Err)
		}
		rejected("%v", ev.Err)
	case "cancel":
		handler = func(w http.ResponseWriter, r *http.Request) {
			sse(w, `{"choices":[{"index":0,"delta":{"content":"x"}}]}`)
			<-r.Context().Done()
		}
		cctx, cancel := context.WithCancel(ctx)
		time.AfterFunc(300*time.Millisecond, cancel)
		start := time.Now()
		ev := last(run(cctx, client(srv.URL, []string{srv.URL}, config.NewRedactor("SECRET123"))))
		if ev.Kind != provider.EventError || !errors.Is(ev.Err, context.Canceled) || time.Since(start) > 2*time.Second {
			accepted("cancel: kind=%v err=%v after %v", ev.Kind, ev.Err, time.Since(start))
		}
		rejected("%v", ev.Err)
	default:
		setup("unknown case %q", os.Args[1])
	}
}
