//go:build ignore

// Supervisor probe for T203 (host-allowlist transport). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"

	"yipyip/internal/provider"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

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
	var allowedHits, foreignHits atomic.Int64
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignHits.Add(1)
		io.WriteString(w, "foreign")
	}))
	defer foreign.Close()
	allowed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowedHits.Add(1)
		if r.URL.Path == "/redir" {
			http.Redirect(w, r, foreign.URL+"/x", http.StatusFound)
			return
		}
		io.WriteString(w, "allowed")
	}))
	defer allowed.Close()
	client, err := provider.NewHTTPClient([]string{allowed.URL + "/v1"})
	if err != nil {
		setup("NewHTTPClient: %v", err)
	}
	refused := func(err error) bool { return err != nil && errors.Is(err, provider.ErrHostNotAllowed) }

	switch os.Args[1] {
	case "allowed":
		resp, err := client.Get(allowed.URL + "/v1/chat/completions")
		if err != nil {
			fail("request to the configured host failed: %v", err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(b) != "allowed" || allowedHits.Load() != 1 {
			fail("unexpected response %q hits=%d", b, allowedHits.Load())
		}
		if _, err := provider.NewTransport([]string{"https://openai.example/v1", allowed.URL}); err != nil {
			fail("NewTransport rejected valid URLs: %v", err)
		}
		pass("configured host allowed")
	case "foreign":
		_, err := client.Get(foreign.URL + "/v1/chat/completions")
		if !refused(err) || foreignHits.Load() != 0 {
			accepted("foreign host: err=%v hits=%d", err, foreignHits.Load())
		}
		rejected("%v", err)
	case "hostname":
		// Same server, but addressed as localhost: the allowlist compares host:port strings.
		u := strings.Replace(allowed.URL, "127.0.0.1", "localhost", 1)
		_, err := client.Get(u + "/v1")
		if !refused(err) || allowedHits.Load() != 0 {
			accepted("different host name: err=%v hits=%d", err, allowedHits.Load())
		}
		rejected("%v", err)
	case "redirect":
		resp, err := client.Get(allowed.URL + "/redir")
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil || foreignHits.Load() != 0 {
			accepted("redirect to a foreign host followed: err=%v hits=%d", err, foreignHits.Load())
		}
		rejected("%v", err)
	case "empty":
		c, err := provider.NewHTTPClient(nil)
		if err != nil {
			rejected("empty allowlist refused at construction: %v", err)
		}
		_, err = c.Get(allowed.URL + "/v1")
		if !refused(err) || allowedHits.Load() != 0 {
			accepted("empty allowlist let a request through: err=%v", err)
		}
		rejected("%v", err)
	case "badurl":
		for _, u := range []string{"::not a url", "ftp://example.org", "http://"} {
			if _, err := provider.NewTransport([]string{u}); err == nil {
				accepted("NewTransport accepted %q", u)
			}
		}
		rejected("invalid base URLs refused")
	default:
		setup("unknown case %q", os.Args[1])
	}
}
