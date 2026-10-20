package provider

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// countingServer starts an httptest server on 127.0.0.1 that counts requests.
func countingServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if h != nil {
			h(w, r)
			return
		}
		io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func newClient(t *testing.T, baseURLs ...string) *http.Client {
	t.Helper()
	c, err := NewHTTPClient(baseURLs)
	if err != nil {
		t.Fatalf("NewHTTPClient(%q): %v", baseURLs, err)
	}
	return c
}

func TestTransportAllowsConfiguredHost(t *testing.T) {
	srv, hits := countingServer(t, nil)
	c := newClient(t, srv.URL+"/v1")
	resp, err := c.Get(srv.URL + "/v1/chat/completions")
	if err != nil {
		t.Fatalf("request to configured host: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "ok" || hits.Load() != 1 {
		t.Fatalf("body %q hits %d", b, hits.Load())
	}

	// Host case and an explicit default port do not matter.
	tr, err := NewTransport([]string{"HTTPS://API.Example.COM/v1", "http://Other.example:80"})
	if err != nil {
		t.Fatalf("NewTransport: %v", err)
	}
	for _, raw := range []string{"https://api.example.com:443/x", "https://api.example.com/x", "http://other.example/y", "http://OTHER.example:080/y"} {
		u, _ := url.Parse(raw)
		if ok, _ := tr.allows(u); !ok {
			t.Errorf("%s should be allowed", raw)
		}
	}
}

func TestTransportRejectsForeignHost(t *testing.T) {
	allowed, allowedHits := countingServer(t, nil)
	foreign, foreignHits := countingServer(t, nil)
	c := newClient(t, allowed.URL)

	_, err := c.Get(foreign.URL + "/v1/chat/completions")
	if !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("foreign host: err = %v, want ErrHostNotAllowed", err)
	}
	// A different name for the allowed server is a different host (no DNS).
	_, err = c.Get(strings.Replace(allowed.URL, "127.0.0.1", "localhost", 1) + "/v1")
	if !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("localhost alias: err = %v, want ErrHostNotAllowed", err)
	}
	// Userinfo naming the allowed host must not fool the check.
	au, _ := url.Parse(allowed.URL)
	fu, _ := url.Parse(foreign.URL)
	_, err = c.Get("http://" + au.Host + "@" + fu.Host + "/x")
	if !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("userinfo trick: err = %v, want ErrHostNotAllowed", err)
	}
	if foreignHits.Load() != 0 || allowedHits.Load() != 0 {
		t.Fatalf("servers reached: foreign=%d allowed=%d", foreignHits.Load(), allowedHits.Load())
	}

	// The request body is closed on refusal, and RoundTrip itself refuses.
	tr, err := NewTransport([]string{allowed.URL})
	if err != nil {
		t.Fatal(err)
	}
	body := &closeTracker{Reader: strings.NewReader("payload")}
	req, _ := http.NewRequest(http.MethodPost, foreign.URL+"/v1", body)
	if _, err := tr.RoundTrip(req); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("RoundTrip foreign: err = %v", err)
	}
	if !body.closed {
		t.Fatal("request body not closed on refusal")
	}
	if foreignHits.Load() != 0 {
		t.Fatalf("foreign server reached %d times", foreignHits.Load())
	}

	// Non-http schemes and look-alike hosts are refused without dialing.
	tr2, _ := NewTransport([]string{"https://api.example.com"})
	for _, raw := range []string{
		"ftp://api.example.com/x",
		"https://api.example.com.evil.test/x",
		"https://api.example.com./x",
		"https://xn--pi-7ba.example.com/x",
	} {
		u, _ := url.Parse(raw)
		if ok, _ := tr2.allows(u); ok {
			t.Errorf("%s should be refused", raw)
		}
	}
	unicodeReq := &http.Request{Method: http.MethodGet, URL: &url.URL{Scheme: "https", Host: "api.Kxample.com"}}
	if _, err := tr2.RoundTrip(unicodeReq); !errors.Is(err, ErrHostNotAllowed) {
		t.Errorf("non-ASCII host: err = %v", err)
	}
}

type closeTracker struct {
	io.Reader
	closed bool
}

func (c *closeTracker) Close() error { c.closed = true; return nil }

func TestTransportRejectsOtherPort(t *testing.T) {
	allowed, _ := countingServer(t, nil)
	other, otherHits := countingServer(t, nil)
	c := newClient(t, allowed.URL)
	_, err := c.Get(other.URL + "/v1")
	if !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("other port: err = %v, want ErrHostNotAllowed", err)
	}
	if otherHits.Load() != 0 {
		t.Fatalf("other-port server reached %d times", otherHits.Load())
	}

	// The scheme default port differs from an explicit other port.
	tr, _ := NewTransport([]string{"https://api.example.com"})
	for _, raw := range []string{"https://api.example.com:8443/x", "http://api.example.com/x", "https://api.example.com:0/x", "https://api.example.com:65979/x"} {
		u, _ := url.Parse(raw)
		if ok, _ := tr.allows(u); ok {
			t.Errorf("%s should be refused", raw)
		}
	}
}

func TestTransportRejectsRedirectToForeignHost(t *testing.T) {
	foreign, foreignHits := countingServer(t, nil)
	allowed, allowedHits := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redir":
			http.Redirect(w, r, foreign.URL+"/x", http.StatusFound)
		case "/local":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		default:
			io.WriteString(w, "final")
		}
	})
	c := newClient(t, allowed.URL)

	resp, err := c.Get(allowed.URL + "/redir")
	if resp != nil {
		resp.Body.Close()
	}
	if !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("redirect to foreign: err = %v, want ErrHostNotAllowed", err)
	}
	if foreignHits.Load() != 0 {
		t.Fatalf("foreign server reached %d times", foreignHits.Load())
	}

	// Redirects within the allowed host are followed.
	resp, err = c.Get(allowed.URL + "/local")
	if err != nil {
		t.Fatalf("same-host redirect: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "final" {
		t.Fatalf("same-host redirect body %q", b)
	}

	// At most 10 redirects.
	before := allowedHits.Load()
	resp, err = c.Get(allowed.URL + "/loop")
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("redirect loop: want error")
	}
	if n := allowedHits.Load() - before; n != maxRedirects {
		t.Fatalf("redirect loop: %d requests, want %d", n, maxRedirects)
	}
	if c.Timeout != 0 {
		t.Fatalf("client has a Timeout %v", c.Timeout)
	}
}

func TestTransportEmptyAllowlist(t *testing.T) {
	srv, hits := countingServer(t, nil)
	for _, list := range [][]string{nil, {}} {
		c, err := NewHTTPClient(list)
		if err != nil {
			t.Fatalf("NewHTTPClient(%v): %v", list, err)
		}
		_, err = c.Get(srv.URL + "/v1")
		if !errors.Is(err, ErrHostNotAllowed) {
			t.Fatalf("empty allowlist: err = %v, want ErrHostNotAllowed", err)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("server reached %d times", hits.Load())
	}
}

func TestTransportInvalidBaseURL(t *testing.T) {
	for _, raw := range []string{
		"::not a url",
		"ftp://example.org",
		"http://",
		"https://",
		"example.org",
		"/v1",
		"",
		"http://:8080",
		"http://example.org:0",
		"http://example.org:70000",
		"mailto:x@example.org",
		"http:example.org",
	} {
		if _, err := NewTransport([]string{raw}); err == nil {
			t.Errorf("NewTransport(%q) accepted an invalid URL", raw)
		}
		if _, err := NewHTTPClient([]string{"http://127.0.0.1:1", raw}); err == nil {
			t.Errorf("NewHTTPClient with %q accepted an invalid URL", raw)
		}
	}
	for _, raw := range []string{"http://127.0.0.1:8080/v1", "https://api.example.com/v1", "http://[::1]:9000"} {
		if _, err := NewTransport([]string{raw}); err != nil {
			t.Errorf("NewTransport(%q): %v", raw, err)
		}
	}
}

// The inner transport must ignore proxy environment variables.
func TestTransportIgnoresProxyEnv(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	tr, err := NewTransport(nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.inner.Proxy != nil {
		t.Fatal("inner transport has a Proxy func")
	}
	if !tr.inner.ForceAttemptHTTP2 || tr.inner.TLSHandshakeTimeout == 0 {
		t.Fatal("inner transport settings missing")
	}
}
