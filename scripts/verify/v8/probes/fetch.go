//go:build ignore

// Supervisor probe for v8 T301 (provider.NewFetchHTTPClient). Workers never edit it.
// All servers are httptest servers on 127.0.0.1; blocked addresses must be
// refused at dial time, before any connection is made.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"yipyip/internal/provider"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

// page is an httptest server that records requests.
type page struct {
	*httptest.Server
	mu   sync.Mutex
	hits []*http.Request
}

func newPage(h func(w http.ResponseWriter, r *http.Request)) *page {
	p := &page{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.hits = append(p.hits, r.Clone(r.Context()))
		p.mu.Unlock()
		h(w, r)
	}))
	return p
}

func (p *page) addr() string { return p.Listener.Addr().String() }

func (p *page) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.hits)
}

func okHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	io.WriteString(w, "ok")
}

func get(c *http.Client, u string, hdr map[string]string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		setup("NewRequest: %v", err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), nil
}

// blocked reports a refusal by the dial-time address check.
func blocked(err error) bool {
	return err != nil && (errors.Is(err, provider.ErrAddressNotAllowed) || strings.Contains(err.Error(), "address not allowed"))
}

func main() {
	defer func() {
		if p := recover(); p != nil {
			fmt.Println("PANIC:", p)
			os.Exit(125)
		}
	}()
	if len(os.Args) != 2 {
		setup("usage: probe <case>")
	}
	c := os.Args[1]
	switch {
	case c == "defaults":
		cl := provider.NewFetchHTTPClient(provider.FetchOptions{})
		if cl == nil || cl.Jar != nil || cl.Timeout != 20*time.Second || cl.Transport == nil || cl.Transport == http.DefaultTransport {
			fail("client %+v: want no jar, Timeout 20s, own transport", cl)
		}
		if provider.ErrAddressNotAllowed == nil || provider.ErrAddressNotAllowed.Error() != "address not allowed" {
			fail("ErrAddressNotAllowed = %v", provider.ErrAddressNotAllowed)
		}
		pass("defaults")
	case c == "headers": // fixed User-Agent, no Authorization, no Cookie (even when set), no jar
		p := newPage(func(w http.ResponseWriter, r *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "s1"})
			okHandler(w, r)
		})
		defer p.Close()
		cl := provider.NewFetchHTTPClient(provider.FetchOptions{AllowAddr: p.addr()})
		for i := 0; i < 2; i++ {
			body, err := get(cl, p.URL+"/h", map[string]string{"Authorization": "Bearer SECRET123", "Cookie": "a=b", "User-Agent": "other"})
			if err != nil || body != "ok" {
				fail("GET: %q %v", body, err)
			}
		}
		for _, r := range p.hits {
			if ua := r.Header.Get("User-Agent"); ua != "yipyip" {
				fail("User-Agent = %q", ua)
			}
			if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				fail("Authorization %q / Cookie %q sent", r.Header.Get("Authorization"), r.Header.Get("Cookie"))
			}
		}
		pass("headers")
	case c == "redirects5" || c == "redirects6":
		var p *page
		p = newPage(func(w http.ResponseWriter, r *http.Request) {
			n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/r/"))
			if n > 0 {
				http.Redirect(w, r, p.URL+"/r/"+strconv.Itoa(n-1), http.StatusFound)
				return
			}
			okHandler(w, r)
		})
		defer p.Close()
		cl := provider.NewFetchHTTPClient(provider.FetchOptions{AllowAddr: p.addr(), Timeout: 5 * time.Second})
		if c == "redirects5" {
			if body, err := get(cl, p.URL+"/r/5", nil); err != nil || body != "ok" {
				fail("5 redirects: %q %v", body, err)
			}
			pass("5 redirects followed")
		}
		if _, err := get(cl, p.URL+"/r/6", nil); err != nil {
			rejected("6 redirects refused: %v", err)
		}
		accepted("6 redirects followed")
	case c == "timeout": // the Timeout hook bounds a slow server
		p := newPage(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(5 * time.Second):
			case <-r.Context().Done():
			}
			okHandler(w, r)
		})
		defer p.Close()
		cl := provider.NewFetchHTTPClient(provider.FetchOptions{AllowAddr: p.addr(), Timeout: 300 * time.Millisecond})
		start := time.Now()
		_, err := get(cl, p.URL, nil)
		if err == nil || time.Since(start) > 3*time.Second {
			fail("slow server: err %v after %v", err, time.Since(start))
		}
		pass("timeout after %v", time.Since(start))
	case c == "loopback": // a 127.0.0.1 server without AllowAddr is refused, never reached
		p := newPage(okHandler)
		defer p.Close()
		_, err := get(provider.NewFetchHTTPClient(provider.FetchOptions{Timeout: 2 * time.Second}), p.URL, nil)
		if blocked(err) && p.count() == 0 {
			rejected("loopback refused: %v", err)
		}
		accepted("loopback: err %v, hits %d", err, p.count())
	case c == "allowaddr-port": // AllowAddr exempts exactly one ip:port
		p := newPage(okHandler)
		defer p.Close()
		_, err := get(provider.NewFetchHTTPClient(provider.FetchOptions{AllowAddr: "127.0.0.1:1", Timeout: 2 * time.Second}), p.URL, nil)
		if blocked(err) && p.count() == 0 {
			rejected("other port refused: %v", err)
		}
		accepted("other port: err %v, hits %d", err, p.count())
	case c == "redirect-loopback": // allowed server redirects to another 127.0.0.1 server
		b := newPage(okHandler)
		defer b.Close()
		a := newPage(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, b.URL+"/x", http.StatusFound) })
		defer a.Close()
		_, err := get(provider.NewFetchHTTPClient(provider.FetchOptions{AllowAddr: a.addr(), Timeout: 2 * time.Second}), a.URL, nil)
		if blocked(err) && b.count() == 0 {
			rejected("redirect to loopback refused: %v", err)
		}
		accepted("redirect to loopback: err %v, target hits %d", err, b.count())
	case c == "redirect-userinfo": // redirect targets are re-checked: no userinfo
		var a *page
		a = newPage(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/x" {
				okHandler(w, r)
				return
			}
			http.Redirect(w, r, "http://u:pw@"+a.addr()+"/x", http.StatusFound)
		})
		defer a.Close()
		_, err := get(provider.NewFetchHTTPClient(provider.FetchOptions{AllowAddr: a.addr(), Timeout: 2 * time.Second}), a.URL+"/start", nil)
		if err != nil && a.count() == 1 {
			rejected("redirect with userinfo refused: %v", err)
		}
		accepted("redirect with userinfo: err %v, hits %d", err, a.count())
	case strings.HasPrefix(c, "addr="): // e.g. addr=10.0.0.1:9, addr=[::1]:9, addr=localhost:9
		hostport := strings.TrimPrefix(c, "addr=")
		_, err := get(provider.NewFetchHTTPClient(provider.FetchOptions{Timeout: time.Second}), "http://"+hostport+"/", nil)
		if blocked(err) {
			rejected("%s refused: %v", hostport, err)
		}
		accepted("%s not refused by the address check: %v", hostport, err)
	default:
		setup("unknown case %q", c)
	}
}
