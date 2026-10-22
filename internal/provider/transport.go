// Package provider talks to OpenAI-compatible chat APIs. All of yipyip's
// outgoing HTTP goes through the host-allowlist Transport defined here: the
// provider/jev client (NewHTTPClient) and the separate search-only client
// (NewSearchHTTPClient), whose allowlists never share the search host.
package provider

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrHostNotAllowed is wrapped by every error returned for a request whose
// target host:port is not in the allowlist.
var ErrHostNotAllowed = errors.New("host not allowed")

// maxRedirects is the maximum number of redirects a client follows.
const maxRedirects = 10

// Transport is an http.RoundTripper that only lets requests through whose
// canonical host:port (lowercase host, explicit or scheme-default port) is in
// a fixed allowlist. The comparison is a plain string match: no DNS lookups.
type Transport struct {
	allowed map[string]struct{}
	inner   *http.Transport
}

// NewTransport builds a Transport whose allowlist is the set of canonical
// host:port of baseURLs. Every entry must be an absolute http or https URL
// with a host. An empty list is valid and allows nothing.
func NewTransport(baseURLs []string) (*Transport, error) {
	allowed := make(map[string]struct{}, len(baseURLs))
	for _, raw := range baseURLs {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid base URL: %w", err)
		}
		key, err := canonicalHostPort(u)
		if err != nil {
			return nil, fmt.Errorf("invalid base URL: %w", err)
		}
		allowed[key] = struct{}{}
	}
	return &Transport{allowed: allowed, inner: newInnerTransport()}, nil
}

// newInnerTransport builds the transport used for allowed requests from
// scratch: no proxy (HTTP_PROXY/HTTPS_PROXY are ignored), bounded dial and
// TLS-handshake times, but no overall timeout because responses stream.
func newInnerTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   10 * time.Second,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// canonicalHostPort returns "host:port" for an absolute http/https URL:
// lowercase host, numeric port without leading zeros, scheme default (80 or
// 443) when the URL has none. IPv6 literals are bracketed. Hosts that are
// empty or contain non-ASCII, whitespace, '%' (other than an IPv6 zone) or
// other unusual bytes are refused, so that string equality is meaningful.
func canonicalHostPort(u *url.URL) (string, error) {
	if u == nil {
		return "", errors.New("missing URL")
	}
	var defPort int
	switch strings.ToLower(u.Scheme) {
	case "http":
		defPort = 80
	case "https":
		defPort = 443
	default:
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Opaque != "" {
		return "", errors.New("opaque URL")
	}
	host := u.Hostname()
	if host == "" {
		return "", errors.New("missing host")
	}
	if !validHost(host) {
		return "", errors.New("invalid host")
	}
	host = strings.ToLower(host)
	port := defPort
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return "", errors.New("invalid port")
		}
		port = n
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// validHost accepts ASCII host names, IPv4 literals and IPv6 literals
// (optionally with a zone). Anything else is refused.
func validHost(host string) bool {
	if strings.Contains(host, ":") { // IPv6 literal, possibly with a zone
		addr, zone, _ := strings.Cut(host, "%")
		if net.ParseIP(addr) == nil {
			return false
		}
		for i := 0; i < len(zone); i++ {
			if c := zone[i]; c <= ' ' || c >= 0x7f || c == '%' {
				return false
			}
		}
		return true
	}
	for i := 0; i < len(host); i++ {
		c := host[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '.', c == '_':
		default:
			return false
		}
	}
	return true
}

// allows reports whether u's canonical host:port is in the allowlist, and
// returns a description of the target for error messages (never userinfo).
func (t *Transport) allows(u *url.URL) (bool, string) {
	key, err := canonicalHostPort(u)
	if err != nil {
		return false, "invalid request URL"
	}
	_, ok := t.allowed[key]
	return ok, key
}

// RoundTrip forwards req to the inner transport if its target host:port is
// allowed. Otherwise it closes the request body and returns an error wrapping
// ErrHostNotAllowed without dialing.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: nil request", ErrHostNotAllowed)
	}
	if ok, target := t.allows(req.URL); !ok {
		if req.Body != nil {
			req.Body.Close()
		}
		return nil, fmt.Errorf("%w: %s", ErrHostNotAllowed, target)
	}
	return t.inner.RoundTrip(req)
}

// SearchTimeout is the overall timeout of the search client: search
// responses are small and never stream.
const SearchTimeout = 20 * time.Second

// NewHTTPClient returns a client that uses a Transport for baseURLs and
// checks every redirect target against the same allowlist (at most 10
// redirects). It has no overall timeout because responses stream.
func NewHTTPClient(baseURLs []string) (*http.Client, error) {
	t, err := NewTransport(baseURLs)
	if err != nil {
		return nil, err
	}
	return newAllowlistClient(t), nil
}

// NewSearchHTTPClient returns a client for the web search service only: its
// Transport allows exactly the canonical host:port of baseURL, redirects are
// checked against that single host (at most 10), and every request is bounded
// by SearchTimeout. The search host is never added to the provider/jev
// client. An empty or invalid baseURL yields an error that does not contain
// the URL.
func NewSearchHTTPClient(baseURL string) (*http.Client, error) {
	if baseURL == "" {
		return nil, errors.New("search: missing base URL")
	}
	t, err := NewTransport([]string{baseURL})
	if err != nil {
		// NewTransport's error may quote the URL; never pass it on.
		return nil, errors.New("search: invalid base URL")
	}
	c := newAllowlistClient(t)
	c.Timeout = SearchTimeout
	return c, nil
}

// newAllowlistClient wraps t in a client whose CheckRedirect refuses
// redirects to hosts outside t's allowlist and stops after maxRedirects.
func newAllowlistClient(t *Transport) *http.Client {
	return &http.Client{
		Transport: t,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			if ok, target := t.allows(req.URL); !ok {
				return fmt.Errorf("%w: redirect to %s", ErrHostNotAllowed, target)
			}
			return nil
		},
	}
}
