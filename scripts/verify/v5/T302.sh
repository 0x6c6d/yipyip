#!/usr/bin/env bash
# Verification for T302 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named and existing /jev tests" <<'CMD'
go_tests ./internal/tui TestJevCmdRoute TestJevCmdNoRoute TestJevToggle TestJevUnconfigured TestJevHelp
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

func zzModel(t *testing.T, project string) (Model, *agent.Session) {
	t.Helper()
	srv := providertest.New()
	t.Cleanup(srv.Close)
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
	if project != "" {
		if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(project), 0o644); err != nil {
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
	sess, err := agent.NewSession(agent.Options{Config: cfg, StartDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Cancel(); sess.Wait() })
	m := New(Options{StartDir: dir, Session: sess})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return next.(Model), sess
}

const zzModels = `"provider":{"fake":{"models":{"m3":{"name":"m3"},"m4":{"name":"m4"}}}}`

func zzJev(route string) string {
	j := `{` + zzModels + `,"jev":{"baseURL":"http://127.0.0.1:1","apiKey":"{env:TYPESAFE_API_KEY}"`
	if route != "" {
		j += `,"route":` + route
	}
	return j + `}}`
}

func zzLast(m Model) string {
	lines := strings.Split(strings.TrimRight(ansi.Strip(m.View()), "\n"), "\n")
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return strings.Join(out, "\n")
}

func TestZZPosRouteLines(t *testing.T) {
	for _, c := range []struct{ route, want string }{
		{`{"easy":"fake/m2","medium":"fake/m3","hard":"fake/m4"}`, "jev on · easy=fake/m2 medium=fake/m3 hard=fake/m4"},
		{`{"easy":"fake/m2","hard":"fake/m4"}`, "jev on · easy=fake/m2 medium=fake/m4 hard=fake/m4"},
		{`{"medium":"fake/m3","hard":"fake/m2"}`, "jev on · easy=fake/m2 medium=fake/m3 hard=fake/m2"},
		{"", "jev on · no route configured"},
	} {
		m, sess := zzModel(t, zzJev(c.route))
		m, _ = m.Command("/jev")
		if v := zzLast(m); !strings.Contains(v, c.want) || !sess.JevOn() {
			t.Fatalf("route %s: want %q in:\n%s", c.route, c.want, v)
		}
		m, _ = m.Command("/jev")
		if v := zzLast(m); !strings.Contains(v, "jev off") || sess.JevOn() {
			t.Fatalf("toggle off:\n%s", v)
		}
	}
}

// TestZZEvil passes only when /jev without a configured jev prints "jev on"
// or switches jev on.
func TestZZEvil(t *testing.T) {
	for _, proj := range []string{"", `{"jev":{"baseURL":"http://127.0.0.1:1"}}`} {
		m, sess := zzModel(t, proj)
		m, _ = m.Command("/jev")
		v := zzLast(m)
		if strings.Contains(v, "jev on") || sess.JevOn() || !strings.Contains(v, "jev is not configured") {
			t.Logf("unconfigured /jev: %s", v)
			return
		}
	}
	t.Fatal("REJECTED: unconfigured jev stays off")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/tui/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: three-tier, two-key, no-easy and no-route lines; toggle off" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/tui TestZZPosRouteLines
CMD
expect_fail "negative: /jev without a configured jev prints jev on or switches it on" <<'CMD'
out="$(go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvil$' ./internal/tui 2>&1)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q -e 'REJECTED:' && exit 1
printf '%s\n' "$out" | grep -q -e '^ok' && exit 0
exit 125
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 0c3a7c5 -- go.mod go.sum
CMD
expect_fail "negative: no protected or security-critical path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md CLAUDE.md prompts .claude scripts/verify versions/v5/spec.md versions/v5/plan.md versions/v5/tasks internal/provider internal/tools internal/config internal/agent/approval.go internal/agent/question.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
