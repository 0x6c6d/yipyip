#!/usr/bin/env bash
# Verification for T304 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named tests: provider" <<'CMD'
go_tests ./internal/provider TestJevHostRefusedUnset TestJevHostAllowedSet TestTransportJevOnlyItsHost TestJevForeignHostRefused
CMD
expect_ok "named tests: config and agent" <<'CMD'
go_tests ./internal/config TestJevAllowedBaseURLs TestJevRouteUnknownModel && go_tests ./internal/agent TestJevSessionClient
CMD
expect_ok "exported API present" <<'CMD'
has_symbols ./internal/config Config.AllowedBaseURLs && has_symbols ./internal/agent Session.JevClient
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"yipyip/internal/config"
	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

func zzSess(t *testing.T, srv *providertest.Server, jevJSON string) (*Session, *config.Config) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	for _, d := range []string{filepath.Join(home, ".config", "yipyip"), dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(config.GlobalPath(home), []byte(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if jevJSON != "" {
		if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(`{"jev":`+jevJSON+`}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(home, dir, func(k string) string {
		switch k {
		case "FAKE_KEY":
			return "SECRET123"
		case "TYPESAFE_API_KEY":
			return "JEVKEY-zz"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(Options{Config: cfg, StartDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Cancel(); s.Wait() })
	return s, cfg
}

var zzQ = []provider.JevQuestion{{Name: "r", Type: provider.JevNoul, Instructions: "risky?"}}

func zzJev() *providertest.JevServer {
	return providertest.NewJev(func(int, providertest.JevRequest) providertest.JevResponse {
		return providertest.JevResponse{Answers: map[string]any{"r": providertest.JevNoulAnswer(0.1)}}
	})
}

func TestZZPosAllowSet(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	jev := zzJev()
	defer jev.Close()
	s, cfg := zzSess(t, srv, `{"baseURL":"`+jev.URL+`","apiKey":"{env:TYPESAFE_API_KEY}"}`)
	got := cfg.AllowedBaseURLs()
	if len(got) != 2 {
		t.Fatalf("allowed: %q", got)
	}
	c := s.JevClient()
	if c == nil {
		t.Fatal("nil JevClient with jev configured")
	}
	if _, _, err := c.Ask(context.Background(), "s", zzQ); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if r := jev.Requests(); len(r) != 1 || r[0].Header.Get("Authorization") != "Bearer JEVKEY-zz" {
		t.Fatalf("requests: %+v", r)
	}
}

func TestZZPosUnset(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	jev := zzJev()
	defer jev.Close()
	s, cfg := zzSess(t, srv, "")
	if s.JevClient() != nil {
		t.Fatal("JevClient without jev")
	}
	a, b := cfg.AllowedBaseURLs(), cfg.BaseURLs()
	if len(a) != len(b) || len(a) != 1 || a[0] != b[0] {
		t.Fatalf("allowed %q vs base %q", a, b)
	}
	_, _, err := provider.NewJevClient(s.hc, jev.URL, "k", "m").Ask(context.Background(), "s", zzQ)
	if !errors.Is(err, provider.ErrHostNotAllowed) || len(jev.Requests()) != 0 {
		t.Fatalf("jev host reachable without jev: %v", err)
	}
	s2, _ := zzSess(t, srv, `{"baseURL":"`+jev.URL+`"}`)
	if s2.JevClient() != nil {
		t.Fatal("JevClient without key")
	}
}

// TestZZEvilReach passes only when the session client reaches the jev host
// without "jev", or a foreign host with "jev" set.
func TestZZEvilReach(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	jev := zzJev()
	defer jev.Close()
	other := zzJev()
	defer other.Close()
	s, _ := zzSess(t, srv, "")
	if _, _, err := provider.NewJevClient(s.hc, jev.URL, "k", "m").Ask(context.Background(), "s", zzQ); err == nil {
		return
	}
	s2, _ := zzSess(t, srv, `{"baseURL":"`+jev.URL+`","apiKey":"{env:TYPESAFE_API_KEY}"}`)
	if _, _, err := provider.NewJevClient(s2.hc, other.URL, "k", "m").Ask(context.Background(), "s", zzQ); err == nil {
		return
	}
	t.Fatal("REJECTED: jev host only while configured, foreign host refused")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/agent/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: allowlist with jev, without jev, without key" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/agent TestZZPosAllowSet TestZZPosUnset
CMD
expect_fail "negative: jev host reachable when unset or foreign host reachable when set" <<'CMD'
go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvilReach$' ./internal/agent
CMD
expect_fail "negative: only transport.go builds transports or clients" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' --exclude='transport.go' -e 'http.Transport{' -e 'http.Client{' -e 'NewTransport(' internal cmd
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 4fabda9 -- go.mod go.sum
CMD
expect_fail "negative: no protected or security-critical path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v4/spec.md versions/v4/plan.md versions/v4/tasks internal/provider/transport.go internal/tools internal/agent/approval.go internal/config/redact.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
