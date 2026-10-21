#!/usr/bin/env bash
# Verification for T301 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named jev config tests" <<'CMD'
go_tests ./internal/config TestJevConfigDefaults TestJevConfigUnset TestJevConfigValues TestJevRouteUnknownModel TestJevConfigInvalid
CMD
expect_ok "exported API present" <<'CMD'
has_symbols ./internal/config Jev JevRoute DefaultJevBaseURL DefaultJevModel DefaultJevMinConfidence Config.Jev
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

func zzLoad(t *testing.T, global string) (*Config, error) {
	t.Helper()
	base := t.TempDir()
	home, start := filepath.Join(base, "home"), filepath.Join(base, "start")
	for _, d := range []string{filepath.Join(home, ".config", "yipyip"), start} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(GlobalPath(home), []byte(global), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(home, start, func(k string) string {
		if k == "TYPESAFE_API_KEY" {
			return "SECRET123"
		}
		return ""
	})
}

const zzProv = `"default_model":"fake/m1","provider":{"fake":{"options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"k"},"models":{"m1":{},"m2":{}}}}`

func TestZZPosDefaults(t *testing.T) {
	c, err := zzLoad(t, `{`+zzProv+`,"jev":{"apiKey":"{env:TYPESAFE_API_KEY}"}}`)
	if err != nil {
		t.Fatal(err)
	}
	j := c.Jev
	if j == nil || j.BaseURL != "https://api.typesafe.ai" || DefaultJevBaseURL != "https://api.typesafe.ai" || j.Model != "jev-latest" || j.MinConfidence != 0.7 || j.APIKey != "SECRET123" || j.Route != nil {
		t.Fatalf("defaults: %+v", j)
	}
	if got := c.Redactor.Redact("x SECRET123 y"); strings.Contains(got, "SECRET123") {
		t.Fatalf("jev key not redacted: %q", got)
	}
	c, err = zzLoad(t, `{`+zzProv+`,"jev":{}}`)
	if err != nil || c.Jev == nil || c.Jev.APIKey != "" || c.Jev.Model != "jev-latest" {
		t.Fatalf("empty block: %v %+v", err, c)
	}
}

func TestZZPosUnset(t *testing.T) {
	c, err := zzLoad(t, `{`+zzProv+`}`)
	if err != nil || c.Jev != nil {
		t.Fatalf("unset: %v %+v", err, c)
	}
}

func TestZZPosValues(t *testing.T) {
	c, err := zzLoad(t, `{`+zzProv+`,"jev":{"baseURL":"http://127.0.0.1:7/","model":"jx","min_confidence":0.9,"route":{"easy":"fake/m2","hard":"fake/m1"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	j := c.Jev
	if j == nil || j.BaseURL != "http://127.0.0.1:7" || j.Model != "jx" || j.MinConfidence != 0.9 || j.APIKey != "" || j.Route == nil || j.Route.Easy != "fake/m2" || j.Route.Hard != "fake/m1" {
		t.Fatalf("values: %+v", j)
	}
}

func zzBad(t *testing.T, jev, key, val string) {
	t.Helper()
	_, err := zzLoad(t, `{`+zzProv+`,"jev":`+jev+`}`)
	if err == nil {
		t.Fatalf("accepted %s", jev)
	}
	if !strings.Contains(err.Error(), key) {
		t.Fatalf("error %q does not name %s", err, key)
	}
	if val != "" && strings.Contains(err.Error(), val) {
		t.Fatalf("error %q quotes the value %q", err, val)
	}
}

func TestZZPosRejects(t *testing.T) {
	zzBad(t, `{"route":{"easy":"fake/nope-zz","hard":"fake/m1"}}`, "jev.route.easy", "nope-zz")
	zzBad(t, `{"route":{"easy":"fake/m1","hard":"other/zzz"}}`, "jev.route.hard", "zzz")
	zzBad(t, `{"route":{"easy":"fake/m1"}}`, "jev.route.hard", "")
	zzBad(t, `{"min_confidence":1.5}`, "jev.min_confidence", "")
	zzBad(t, `{"min_confidence":-0.1}`, "jev.min_confidence", "")
	zzBad(t, `{"min_confidence":"high"}`, "jev.min_confidence", "")
	zzBad(t, `{"baseURL":"ftp://h.example/x"}`, "jev.baseURL", "h.example")
	zzBad(t, `{"model":5}`, "jev.model", "")
	zzBad(t, `"on"`, "jev", "")
}

// TestZZEvilAccepts passes only when an unknown route model is accepted or a
// resolved secret appears in a load error.
func TestZZEvilAccepts(t *testing.T) {
	if _, err := zzLoad(t, `{`+zzProv+`,"jev":{"route":{"easy":"fake/ghost","hard":"fake/m1"}}}`); err == nil {
		return
	}
	_, err := zzLoad(t, `{`+zzProv+`,"jev":{"apiKey":"{env:TYPESAFE_API_KEY}","route":{"easy":"{env:TYPESAFE_API_KEY}","hard":"fake/m1"}}}`)
	if err == nil || strings.Contains(err.Error(), "SECRET123") {
		return
	}
	t.Fatal("REJECTED: unknown route models refused without quoting values")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/config/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: defaults, unset, values, rejections naming the key" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/config TestZZPosDefaults TestZZPosUnset TestZZPosValues TestZZPosRejects
CMD
expect_fail "negative: unknown route model accepted or secret quoted in an error" <<'CMD'
go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvilAccepts$' ./internal/config
CMD
expect_fail "negative: config does not do network or exec" <<'CMD'
no_match -n -e '"net/http"' -e '"os/exec"' internal/config/jev.go
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 4fabda9 -- go.mod go.sum
CMD
expect_fail "negative: no protected or security-critical path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v4/spec.md versions/v4/plan.md versions/v4/tasks internal/provider/transport.go internal/tools internal/agent/approval.go internal/config/redact.go internal/config/env.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
