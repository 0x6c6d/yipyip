// Package provider talks to OpenAI-compatible chat APIs. All of yipyip's
// outgoing HTTP goes through the host-allowlist Transport defined here: the
// provider client (NewHTTPClient) and the separate search-only client
// (NewSearchHTTPClient), whose allowlists never share the search host.
package provider

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
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
// by SearchTimeout. The search host is never added to the provider
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

// The web_fetch client (NewFetchHTTPClient) is the only client that reaches
// arbitrary hosts. Instead of a host allowlist it checks the resolved address
// of every connection in the dialer's Control function, so DNS names, DNS
// rebinding and redirects are all covered by the same check.

// FetchTimeout is the default overall timeout of one web_fetch call,
// redirects included.
const FetchTimeout = 20 * time.Second

// MaxFetchRedirects is the maximum number of redirects the fetch client
// follows.
const MaxFetchRedirects = 5

// MaxFetchURLLen is the maximum length of a web_fetch URL in characters
// (runes).
const MaxFetchURLLen = 2000

// ErrAddressNotAllowed is returned by the fetch client's dialer for a
// connection to a loopback, private, link-local, multicast, unspecified or
// otherwise non-public address.
var ErrAddressNotAllowed = errors.New("address not allowed")

// ErrTooManyRedirects is returned when a fetch follows more than
// MaxFetchRedirects redirects.
var ErrTooManyRedirects = errors.New("too many redirects")

// ErrRedirectNotAllowed is returned when a redirect target fails
// ValidateFetchURL.
var ErrRedirectNotAllowed = errors.New("redirect not allowed")

// FetchOptions configures NewFetchHTTPClient.
type FetchOptions struct {
	Timeout   time.Duration // 0 = FetchTimeout
	AllowAddr string        // test hook: one exact resolved "ip:port" exempt from the address check; "" in production
}

// fetchUserAgent is the fixed User-Agent of every fetch request.
const fetchUserAgent = "yipyip"

// ValidateFetchURL checks raw against the web_fetch URL rule and returns the parsed URL.
// The error text is a short reason without the URL, e.g. "url is empty", "url is longer than 2000 characters",
// "scheme must be http or https", "url has no host", "url must not contain user info", "cannot parse url".
func ValidateFetchURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("url is empty")
	}
	if utf8.RuneCountInString(raw) > MaxFetchURLLen {
		return nil, fmt.Errorf("url is longer than %d characters", MaxFetchURLLen)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("cannot parse url")
	}
	if s := strings.ToLower(u.Scheme); s != "http" && s != "https" {
		return nil, errors.New("scheme must be http or https")
	}
	if u.User != nil {
		return nil, errors.New("url must not contain user info")
	}
	if u.Hostname() == "" {
		return nil, errors.New("url has no host")
	}
	return u, nil
}

// fetchAddrAllowed reports whether the fetch client may connect to a. It
// refuses loopback, private (incl. fc00::/7), link-local unicast, link-local
// multicast, interface-local multicast, multicast, unspecified, 0.0.0.0/8
// and invalid addresses; IPv4-mapped IPv6 addresses are judged as IPv4.
// CGNAT (100.64.0.0/10) and other public addresses are allowed.
func fetchAddrAllowed(a netip.Addr) bool {
	if !a.IsValid() {
		return false
	}
	a = a.Unmap().WithZone("")
	switch {
	case a.IsLoopback(), a.IsPrivate(), a.IsLinkLocalUnicast(),
		a.IsLinkLocalMulticast(), a.IsInterfaceLocalMulticast(),
		a.IsMulticast(), a.IsUnspecified():
		return false
	}
	if a.Is4() && a.As4()[0] == 0 { // 0.0.0.0/8
		return false
	}
	return true
}

// fetchDialControl returns the dialer Control function of the fetch client:
// it checks the resolved address of every connection before it is made.
// allowAddr, when not empty, exempts exactly that "ip:port" (tests only).
func fetchDialControl(allowAddr string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		if allowAddr != "" && address == allowAddr {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return ErrAddressNotAllowed
		}
		a, err := netip.ParseAddr(host)
		if err != nil || !fetchAddrAllowed(a) {
			return ErrAddressNotAllowed
		}
		return nil
	}
}

// fetchTransport clones every request (redirects included), sets the fixed
// User-Agent and removes credentials and cookies before passing it on.
type fetchTransport struct {
	inner *http.Transport
}

// RoundTrip implements http.RoundTripper.
func (t *fetchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("User-Agent", fetchUserAgent)
	r.Header.Del("Authorization")
	r.Header.Del("Proxy-Authorization")
	r.Header.Del("Cookie")
	return t.inner.RoundTrip(r)
}

// NewFetchHTTPClient returns a new client for web_fetch: its own transport
// (no proxy, TLS >= 1.2, bounded dial and handshake times, the resolved
// address of every connection checked by fetchAddrAllowed), no cookie jar,
// at most MaxFetchRedirects redirects whose targets must pass
// ValidateFetchURL, and an overall timeout of opts.Timeout (FetchTimeout
// when 0).
func NewFetchHTTPClient(opts FetchOptions) *http.Client {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = FetchTimeout
	}
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   fetchDialControl(opts.AllowAddr),
	}
	inner := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   10 * time.Second,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Transport: &fetchTransport{inner: inner},
		Jar:       nil,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > MaxFetchRedirects {
				return ErrTooManyRedirects
			}
			if _, err := ValidateFetchURL(req.URL.String()); err != nil {
				return ErrRedirectNotAllowed
			}
			return nil
		},
	}
}
