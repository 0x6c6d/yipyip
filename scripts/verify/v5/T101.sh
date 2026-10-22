#!/usr/bin/env bash
# Verification for T101 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named and existing jev config tests" <<'CMD'
go_tests ./internal/config TestJevRouteThreeTiers TestJevRouteTwoKeys TestJevRouteMediumUnknown TestJevRouteUnknownModel TestJevConfigInvalid TestJevConfigValues TestJevConfigDefaults
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

const zzProv = `"provider":{"p":{"options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"k"},"models":{"a":{},"b":{},"c":{}}}}`

func zzLoad(t *testing.T, jev string) (*Config, error) {
	t.Helper()
	home, dir := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Dir(GlobalPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GlobalPath(home), []byte(`{`+zzProv+`,"jev":`+jev+`}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(home, dir, func(string) string { return "" })
}

func TestZZPosRoutes(t *testing.T) {
	for _, c := range []struct {
		jev        string
		e, m, h    string
	}{
		{`{"route":{"easy":"p/a","medium":"p/b","hard":"p/c"}}`, "p/a", "p/b", "p/c"},
		{`{"route":{"easy":"p/a","hard":"p/c"}}`, "p/a", "", "p/c"},
		{`{"route":{"hard":"p/c"}}`, "", "", "p/c"},
		{`{"route":{"medium":"p/b","hard":"p/c"}}`, "", "p/b", "p/c"},
	} {
		cfg, err := zzLoad(t, c.jev)
		if err != nil {
			t.Fatalf("%s: %v", c.jev, err)
		}
		r := cfg.Jev.Route
		if r == nil || r.Easy != c.e || r.Medium != c.m || r.Hard != c.h {
			t.Fatalf("%s: route %+v", c.jev, r)
		}
	}
	cfg, err := zzLoad(t, `{}`)
	if err != nil || cfg.Jev.Route != nil {
		t.Fatalf("no route: %v %+v", err, cfg)
	}
}

// TestZZEvil passes only when an invalid route is accepted or its error does
// not name the key or echoes the value.
func TestZZEvil(t *testing.T) {
	for _, c := range []struct{ jev, key, value string }{
		{`{"route":{"easy":"p/a","medium":"p/ghost","hard":"p/c"}}`, "jev.route.medium", "ghost"},
		{`{"route":{"easy":"p/a","medium":3,"hard":"p/c"}}`, "jev.route.medium", ""},
		{`{"route":{"easy":"p/a","medium":"p/b"}}`, "jev.route.hard", ""},
		{`{"route":{"easy":"zz/phantom","hard":"p/c"}}`, "jev.route.easy", "phantom"},
		{`{"route":{"medium":"p/b","hard":"q/spectre"}}`, "jev.route.hard", "spectre"},
	} {
		_, err := zzLoad(t, c.jev)
		if err == nil {
			t.Logf("accepted: %s", c.jev)
			return
		}
		if !strings.Contains(err.Error(), c.key) || (c.value != "" && strings.Contains(err.Error(), c.value)) {
			t.Logf("bad error for %s: %v", c.jev, err)
			return
		}
	}
	t.Fatal("REJECTED: every invalid route refused naming its key")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/config/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: three-key, two-key, hard-only and medium+hard routes" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/config TestZZPosRoutes
CMD
expect_fail "negative: invalid medium/easy/hard accepted or error lacks the key or echoes the value" <<'CMD'
out="$(go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvil$' ./internal/config 2>&1)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q -e 'REJECTED:' && exit 1
printf '%s\n' "$out" | grep -q -e '^ok' && exit 0
exit 125
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 0c3a7c5 -- go.mod go.sum
CMD
expect_fail "negative: no protected or security-critical path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md CLAUDE.md prompts .claude scripts/verify versions/v5/spec.md versions/v5/plan.md versions/v5/tasks internal/provider internal/tools internal/agent/approval.go internal/config/redact.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
