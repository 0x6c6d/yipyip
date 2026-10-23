package provider

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fetchServer starts an httptest server on 127.0.0.1 that records the
// request headers it receives.
type fetchServer struct {
	*httptest.Server
	hits atomic.Int64
	mu   sync.Mutex
	hdrs []http.Header
}

func newFetchServer(t *testing.T, h http.HandlerFunc) *fetchServer {
	t.Helper()
	s := &fetchServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		s.mu.Lock()
		s.hdrs = append(s.hdrs, r.Header.Clone())
		s.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		io.WriteString(w, "ok")
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *fetchServer) addr() string { return s.Listener.Addr().String() }

func fetchGet(c *http.Client, u string, hdr map[string]string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func TestWebFetchBlockedAddresses(t *testing.T) {
	srv := newFetchServer(t, nil)
	_, port, _ := strings.Cut(srv.addr(), ":")
	c := NewFetchHTTPClient(FetchOptions{Timeout: 2 * time.Second})
	for _, hp := range []string{
		"127.0.0.1:" + port, "127.0.0.1:9", "[::1]:9", "10.0.0.1:9", "192.168.1.1:9",
		"169.254.169.254:9", "0.0.0.0:9", "0.0.0.0:" + port,
	} {
		_, err := fetchGet(c, "http://"+hp+"/", nil)
		if !errors.Is(err, ErrAddressNotAllowed) {
			t.Errorf("GET %s: err = %v, want ErrAddressNotAllowed", hp, err)
		}
	}
	if n := srv.hits.Load(); n != 0 {
		t.Errorf("blocked server got %d requests", n)
	}

	// An allowed server that redirects to a second 127.0.0.1 server.
	target := newFetchServer(t, nil)
	redir := newFetchServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/x", http.StatusFound)
	})
	c = NewFetchHTTPClient(FetchOptions{AllowAddr: redir.addr(), Timeout: 2 * time.Second})
	if _, err := fetchGet(c, redir.URL+"/", nil); !errors.Is(err, ErrAddressNotAllowed) {
		t.Errorf("redirect to loopback: err = %v, want ErrAddressNotAllowed", err)
	}
	if redir.hits.Load() != 1 || target.hits.Load() != 0 {
		t.Errorf("hits: redirector %d (want 1), target %d (want 0)", redir.hits.Load(), target.hits.Load())
	}

	// AllowAddr exempts exactly one ip:port.
	other := newFetchServer(t, nil)
	c = NewFetchHTTPClient(FetchOptions{AllowAddr: "127.0.0.1:1", Timeout: 2 * time.Second})
	if _, err := fetchGet(c, other.URL+"/", nil); !errors.Is(err, ErrAddressNotAllowed) {
		t.Errorf("other port: err = %v, want ErrAddressNotAllowed", err)
	}
	if other.hits.Load() != 0 {
		t.Errorf("other port server got %d requests", other.hits.Load())
	}
}

func TestWebFetchRequest(t *testing.T) {
	t.Run("headers", func(t *testing.T) {
		srv := newFetchServer(t, func(w http.ResponseWriter, r *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "s1"})
			io.WriteString(w, "ok")
		})
		c := NewFetchHTTPClient(FetchOptions{AllowAddr: srv.addr(), Timeout: 5 * time.Second})
		for i := 0; i < 2; i++ {
			body, err := fetchGet(c, srv.URL+"/h", map[string]string{
				"Authorization": "Bearer SECRET123", "Proxy-Authorization": "Basic x",
				"Cookie": "a=b", "User-Agent": "other",
			})
			if err != nil || body != "ok" {
				t.Fatalf("GET %d: %q %v", i, body, err)
			}
		}
		srv.mu.Lock()
		defer srv.mu.Unlock()
		if len(srv.hdrs) != 2 {
			t.Fatalf("server saw %d requests, want 2", len(srv.hdrs))
		}
		for i, h := range srv.hdrs {
			if ua := h.Get("User-Agent"); ua != "yipyip" {
				t.Errorf("request %d: User-Agent = %q", i, ua)
			}
			for _, k := range []string{"Authorization", "Proxy-Authorization", "Cookie"} {
				if v := h.Get(k); v != "" {
					t.Errorf("request %d: %s = %q sent", i, k, v)
				}
			}
		}
	})

	t.Run("redirects", func(t *testing.T) {
		var srv *fetchServer
		srv = newFetchServer(t, func(w http.ResponseWriter, r *http.Request) {
			n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/r/"))
			if n > 0 {
				http.Redirect(w, r, srv.URL+"/r/"+strconv.Itoa(n-1), http.StatusFound)
				return
			}
			io.WriteString(w, "ok")
		})
		c := NewFetchHTTPClient(FetchOptions{AllowAddr: srv.addr(), Timeout: 5 * time.Second})
		if body, err := fetchGet(c, srv.URL+"/r/5", nil); err != nil || body != "ok" {
			t.Errorf("5 redirects: %q %v", body, err)
		}
		if _, err := fetchGet(c, srv.URL+"/r/6", nil); !errors.Is(err, ErrTooManyRedirects) {
			t.Errorf("6 redirects: err = %v, want ErrTooManyRedirects", err)
		}
	})

	t.Run("redirect userinfo", func(t *testing.T) {
		var srv *fetchServer
		srv = newFetchServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/x" {
				io.WriteString(w, "ok")
				return
			}
			http.Redirect(w, r, "http://u:pw@"+srv.addr()+"/x", http.StatusFound)
		})
		c := NewFetchHTTPClient(FetchOptions{AllowAddr: srv.addr(), Timeout: 5 * time.Second})
		if _, err := fetchGet(c, srv.URL+"/start", nil); !errors.Is(err, ErrRedirectNotAllowed) {
			t.Errorf("redirect with userinfo: err = %v, want ErrRedirectNotAllowed", err)
		}
		if n := srv.hits.Load(); n != 1 {
			t.Errorf("server got %d requests, want 1", n)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		srv := newFetchServer(t, func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(5 * time.Second):
			case <-r.Context().Done():
			}
			io.WriteString(w, "ok")
		})
		c := NewFetchHTTPClient(FetchOptions{AllowAddr: srv.addr(), Timeout: 300 * time.Millisecond})
		start := time.Now()
		_, err := fetchGet(c, srv.URL, nil)
		if d := time.Since(start); err == nil || d > 3*time.Second {
			t.Errorf("slow server: err %v after %v", err, d)
		}
	})

	t.Run("defaults", func(t *testing.T) {
		c := NewFetchHTTPClient(FetchOptions{})
		if c.Timeout != 20*time.Second || c.Jar != nil {
			t.Errorf("Timeout %v Jar %v, want 20s and nil", c.Timeout, c.Jar)
		}
		ft, ok := c.Transport.(*fetchTransport)
		if !ok || ft.inner == nil || ft.inner.Proxy != nil {
			t.Fatalf("transport %T: want *fetchTransport with its own inner transport and no proxy", c.Transport)
		}
		if c.Transport == http.DefaultTransport || ft.inner == http.DefaultTransport {
			t.Error("fetch client uses http.DefaultTransport")
		}
		c2 := NewFetchHTTPClient(FetchOptions{})
		if c2.Transport.(*fetchTransport).inner == ft.inner {
			t.Error("two fetch clients share one transport")
		}
		if ErrAddressNotAllowed.Error() != "address not allowed" {
			t.Errorf("ErrAddressNotAllowed = %q", ErrAddressNotAllowed.Error())
		}
	})
}

func TestFetchAddressPolicy(t *testing.T) {
	for _, s := range []string{
		"127.0.0.1", "127.1.2.3", "::1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254",
		"0.0.0.0", "0.1.2.3", "::", "fe80::1", "fc00::1", "fd12::1", "224.0.0.1", "ff02::1", "ff01::1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1",
	} {
		if fetchAddrAllowed(netip.MustParseAddr(s)) {
			t.Errorf("fetchAddrAllowed(%s) = true, want false", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "100.64.0.1", "93.184.216.34", "2606:4700::1"} {
		if !fetchAddrAllowed(netip.MustParseAddr(s)) {
			t.Errorf("fetchAddrAllowed(%s) = false, want true", s)
		}
	}
	if fetchAddrAllowed(netip.Addr{}) {
		t.Error("fetchAddrAllowed(invalid) = true, want false")
	}
}

func TestValidateFetchURL(t *testing.T) {
	for _, s := range []string{"http://example.org/x", "HTTPS://example.org"} {
		if _, err := ValidateFetchURL(s); err != nil {
			t.Errorf("ValidateFetchURL(%q): %v", s, err)
		}
	}
	long := "http://example.org/" + strings.Repeat("a", 2001-len("http://example.org/"))
	for _, s := range []string{
		"", long, "ftp://h/x", "file:///etc/passwd", "http:///x", "http://u:pw@h/x", "http://u@h/x", "javascript:alert(1)",
	} {
		u, err := ValidateFetchURL(s)
		if err == nil {
			t.Errorf("ValidateFetchURL(%.40q) = %v, want an error", s, u)
			continue
		}
		if s != "" && strings.Contains(err.Error(), s) {
			t.Errorf("error %q contains the URL", err)
		}
	}
	if _, err := ValidateFetchURL(""); err == nil || err.Error() != "url is empty" {
		t.Errorf("empty: %v", err)
	}
	if _, err := ValidateFetchURL(long); err == nil || err.Error() != "url is longer than 2000 characters" {
		t.Errorf("long: %v", err)
	}
	ok2000 := "http://example.org/" + strings.Repeat("ä", 2000-len("http://example.org/"))
	if _, err := ValidateFetchURL(ok2000); err != nil {
		t.Errorf("2000 runes: %v", err)
	}
}
