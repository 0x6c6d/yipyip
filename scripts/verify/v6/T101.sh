#!/usr/bin/env bash
# Verification for T101 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named search config tests" <<'CMD'
go_tests ./internal/config TestSearchConfigLoad TestSearchConfigDefaults TestSearchConfigAbsent TestSearchConfigMissingKey TestSearchConfigNonHTTPS TestSearchConfigNoEcho TestSearchNotInAllowedBaseURLs TestSearchConfigProject
CMD
expect_ok "whole config package (jev, env, redact, settings tests unchanged and green)" <<'CMD'
go test -count=1 ./internal/config
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const zzProv = `"provider":{"p":{"options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"k"},"models":{"a":{}}}}`

func zzLoad(t *testing.T, global, project string, env map[string]string) (*Config, error) {
	t.Helper()
	home, dir := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Dir(GlobalPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GlobalPath(home), []byte(global), 0o644); err != nil {
		t.Fatal(err)
	}
	if project != "" {
		if err := os.WriteFile(filepath.Join(dir, ProjectFile), []byte(project), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Load(home, dir, func(k string) string { return env[k] })
}

func TestZZSearchPos(t *testing.T) {
	env := map[string]string{"BRAVE_API_KEY": "BRAVEVAL77"}
	cfg, err := zzLoad(t, `{`+zzProv+`}`, "", env)
	if err != nil || cfg.Search != nil {
		t.Fatalf("absent search: err=%v search=%+v", err, cfg)
	}
	cfg, err = zzLoad(t, `{`+zzProv+`,"jev":{"apiKey":"j"},"search":{"apiKey":"{env:BRAVE_API_KEY}"}}`, "", env)
	if err != nil {
		t.Fatal(err)
	}
	if DefaultSearchBaseURL != "https://api.search.brave.com" || cfg.Search == nil || cfg.Search.BaseURL != DefaultSearchBaseURL || cfg.Search.APIKey != "BRAVEVAL77" {
		t.Fatalf("defaults: %+v", cfg.Search)
	}
	if strings.Contains(cfg.Redactor.Redact("x BRAVEVAL77 y"), "BRAVEVAL77") {
		t.Fatal("env key not redacted")
	}
	for _, u := range append(cfg.AllowedBaseURLs(), cfg.BaseURLs()...) {
		if strings.Contains(u, "search.brave") {
			t.Fatalf("search URL in the provider allowlist: %v", cfg.AllowedBaseURLs())
		}
	}
	for _, c := range []struct{ in, want string }{
		{"http://127.0.0.1:8080/", "http://127.0.0.1:8080"},
		{"http://localhost:9/", "http://localhost:9"},
		{"http://[::1]:7", "http://[::1]:7"},
		{"https://search.example.org/", "https://search.example.org"},
	} {
		cfg, err := zzLoad(t, `{`+zzProv+`}`, `{"search":{"baseURL":"`+c.in+`","apiKey":"LITERALKEY99"}}`, env)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if cfg.Search == nil || cfg.Search.BaseURL != c.want || cfg.Search.APIKey != "LITERALKEY99" {
			t.Fatalf("%s: %+v", c.in, cfg.Search)
		}
		if strings.Contains(cfg.Redactor.Redact("k=LITERALKEY99"), "LITERALKEY99") {
			t.Fatal("literal key not redacted")
		}
		for _, u := range cfg.AllowedBaseURLs() {
			if u == c.want {
				t.Fatalf("search URL %s in AllowedBaseURLs", u)
			}
		}
	}
}

// TestZZSearchEvil passes (exit 0) only when an invalid search block is
// accepted, or its error does not name the key or echoes a value.
func TestZZSearchEvil(t *testing.T) {
	env := map[string]string{"BRAVE_API_KEY": "BRAVEVAL77"}
	for _, c := range []struct {
		search, key, value string
		env                map[string]string
	}{
		{`{}`, "search.apiKey", "", env},
		{`{"apiKey":""}`, "search.apiKey", "", env},
		{`{"apiKey":7}`, "search.apiKey", "", env},
		{`"on"`, "search", "", env},
		{`{"apiKey":"{env:BRAVE_API_KEY}"}`, "BRAVE_API_KEY", "", map[string]string{}},
		{`{"apiKey":"{env:BRAVE_API_KEY}","baseURL":"http://evil-host.example.com"}`, "search.baseURL", "evil-host", env},
		{`{"apiKey":"{env:BRAVE_API_KEY}","baseURL":"http://127.0.0.2:9"}`, "search.baseURL", "127.0.0.2", env},
		{`{"apiKey":"{env:BRAVE_API_KEY}","baseURL":"http://localhost.evil-host.com"}`, "search.baseURL", "evil-host", env},
		{`{"apiKey":"{env:BRAVE_API_KEY}","baseURL":"ftp://127.0.0.1"}`, "search.baseURL", "ftp:", env},
		{`{"apiKey":"{env:BRAVE_API_KEY}","baseURL":"not a url"}`, "search.baseURL", "not a url", env},
		{`{"apiKey":"{env:BRAVE_API_KEY}","baseURL":"https://"}`, "search.baseURL", "", env},
		{`{"apiKey":"{env:BRAVE_API_KEY}","baseURL":7}`, "search.baseURL", "", env},
		{`{"apiKey":"LITERALKEY99","baseURL":"http://evil-host.example.com/LITERALKEY99"}`, "search.baseURL", "LITERALKEY99", env},
	} {
		_, err := zzLoad(t, `{`+zzProv+`,"search":`+c.search+`}`, "", c.env)
		if err == nil {
			t.Logf("accepted: %s", c.search)
			return
		}
		msg := err.Error()
		if !strings.Contains(msg, c.key) || (c.value != "" && strings.Contains(msg, c.value)) || strings.Contains(msg, "BRAVEVAL77") {
			t.Logf("bad error for %s: %v", c.search, err)
			return
		}
	}
	t.Fatal("REJECTED: every invalid search block refused naming its key, no value echoed")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/config/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: defaults, loopback http, trailing slash, key redaction, not in AllowedBaseURLs" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/config TestZZSearchPos
CMD
expect_fail "negative: an invalid search block is accepted or its error lacks the key or echoes a value" <<'CMD'
out="$(go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZSearchEvil$' ./internal/config 2>&1)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q -e 'REJECTED:' && exit 1
printf '%s\n' "$out" | grep -q -e '^ok' && exit 0
exit 125
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet ede3886 -- go.mod go.sum
CMD
expect_fail "negative: a security-critical file differs from HEAD (diff)" <<'CMD'
out="$(git diff --name-only HEAD -- internal/provider/transport.go internal/tools/pathguard.go internal/agent/approval.go internal/tools/bash.go internal/config/redact.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: a file outside internal/config changed (or a protected path)" <<'CMD'
out="$(git status --porcelain --untracked-files=all)" || exit 125
printf '%s\n' "$out" | sed -e 's/^...//' -e 's/^.* -> //' | grep -v -x -e '' -e 'versions/v6/Tasks.md' | grep -v -E -x -e 'internal/config/[a-z0-9_]+\.go' | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
