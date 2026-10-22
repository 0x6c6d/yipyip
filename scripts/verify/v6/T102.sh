#!/usr/bin/env bash
# Verification for T102 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named search transport tests" <<'CMD'
go_tests ./internal/provider TestSearchTransportReachesSearchHost TestSearchTransportRefusesProviderHost TestSearchHostRefusedForProviderClient TestSearchHostRefusedForJevClient TestSearchRedirectRefused TestSearchTransportInvalidBaseURL
CMD
expect_ok "whole provider package (old transport and jev allowlist tests green)" <<'CMD'
go test -count=1 ./internal/provider/...
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package provider_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"yipyip/internal/config"
	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

func zzCounting(n *atomic.Int64, h http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if h != nil {
			h(w, r)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
}

func zzGet(c *http.Client, u string) error {
	resp, err := c.Get(u)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func TestZZSearchTransportPos(t *testing.T) {
	var sh atomic.Int64
	s := zzCounting(&sh, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hop" {
			http.Redirect(w, r, "/res/v1/web/search?q=y", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	defer s.Close()
	c, err := provider.NewSearchHTTPClient(s.URL)
	if err != nil || c == nil {
		t.Fatalf("NewSearchHTTPClient: %v", err)
	}
	if provider.SearchTimeout != 20*time.Second || c.Timeout != 20*time.Second {
		t.Fatalf("timeout %v", c.Timeout)
	}
	if _, ok := c.Transport.(*provider.Transport); !ok {
		t.Fatalf("transport %T", c.Transport)
	}
	for _, p := range []string{"/res/v1/web/search?q=x", "/hop"} {
		if err := zzGet(c, s.URL+p); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	if sh.Load() != 3 {
		t.Fatalf("search server requests = %d", sh.Load())
	}
}

// TestZZSearchTransportEvil passes (exit 0) only when a request that must be
// refused gets through or is refused for the wrong reason.
func TestZZSearchTransportEvil(t *testing.T) {
	var ph, jh, oh, sh atomic.Int64
	other := zzCounting(&oh, nil)
	defer other.Close()
	prov := zzCounting(&ph, nil)
	defer prov.Close()
	jev := zzCounting(&jh, nil)
	defer jev.Close()
	s := zzCounting(&sh, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/away" {
			http.Redirect(w, r, other.URL+"/x", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	defer s.Close()

	home, dir := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Dir(config.GlobalPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.GlobalPath(home), []byte(providertest.ConfigJSON(prov.URL+"/v1", "k", 8000)), 0o644); err != nil {
		t.Fatal(err)
	}
	project := `{"jev":{"baseURL":"` + jev.URL + `","apiKey":"k3"},"search":{"baseURL":"` + s.URL + `","apiKey":"k2"}}`
	if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(home, dir, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	hc, err := provider.NewHTTPClient(cfg.AllowedBaseURLs())
	if err != nil {
		t.Fatal(err)
	}
	sc, err := provider.NewSearchHTTPClient(cfg.Search.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	su, _ := url.Parse(s.URL)
	cases := []struct {
		name string
		do   func() error
	}{
		{"provider/jev client to the search host", func() error { return zzGet(hc, s.URL+"/res/v1/web/search?q=x") }},
		{"search client to the provider host", func() error { return zzGet(sc, prov.URL+"/v1/chat/completions") }},
		{"search client to the jev host", func() error { return zzGet(sc, jev.URL+"/v1/systemone") }},
		{"search client to a foreign host", func() error { return zzGet(sc, other.URL+"/") }},
		{"search client redirected to a foreign host", func() error { return zzGet(sc, s.URL+"/away") }},
		{"search client to localhost spelling of the search port", func() error { return zzGet(sc, "http://localhost:"+su.Port()+"/") }},
	}
	for _, c := range cases {
		err := c.do()
		if err == nil || !errors.Is(err, provider.ErrHostNotAllowed) {
			t.Logf("not refused with ErrHostNotAllowed: %s: %v", c.name, err)
			return
		}
	}
	if n := ph.Load() + jh.Load() + oh.Load(); n != 0 {
		t.Logf("refused hosts were contacted %d times", n)
		return
	}
	for _, bad := range []string{"", "ftp://127.0.0.1", "http://", "127.0.0.1:80"} {
		if c, err := provider.NewSearchHTTPClient(bad); err == nil || c != nil {
			t.Logf("bad base URL %q built a client", bad)
			return
		}
	}
	t.Fatal("REJECTED: every foreign host, cross-host redirect and bad base URL refused")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/provider/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: search client reaches its host, same-host redirect, 20 s timeout" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/provider TestZZSearchTransportPos
CMD
expect_fail "negative: search host reachable by provider/jev client, foreign host or redirect reachable by search client, bad base URL accepted" <<'CMD'
out="$(go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZSearchTransportEvil$' ./internal/provider 2>&1)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q -e 'REJECTED:' && exit 1
printf '%s\n' "$out" | grep -q -e '^ok' && exit 0
exit 125
CMD
expect_ok "transport.go changed against ede3886 (this task owns it)" <<'CMD'
! git diff --quiet ede3886 -- internal/provider/transport.go && grep -q -e 'func NewSearchHTTPClient' internal/provider/transport.go
CMD
expect_fail "negative: another security-critical file differs from HEAD (diff)" <<'CMD'
out="$(git diff --name-only HEAD -- internal/tools/pathguard.go internal/agent/approval.go internal/tools/bash.go internal/config/redact.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: an http.Client is built outside transport.go" <<'CMD'
grep -rln --include='*.go' --exclude='*_test.go' -e 'http\.Client{' cmd internal | grep -v -x -e 'internal/provider/transport.go' | no_match -e .
CMD
expect_fail "negative: a file outside transport.go and provider tests changed (or a protected path)" <<'CMD'
out="$(git status --porcelain --untracked-files=all)" || exit 125
printf '%s\n' "$out" | sed -e 's/^...//' -e 's/^.* -> //' | grep -v -x -e '' -e 'versions/v6/Tasks.md' | grep -v -E -x -e 'internal/provider/transport\.go' -e 'internal/provider/[a-z0-9_]+_test\.go' | no_match -e .
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet ede3886 -- go.mod go.sum
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
