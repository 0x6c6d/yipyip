#!/usr/bin/env bash
# Verification for T203 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named tui tests (new and earlier skill tests)" <<'CMD'
go_tests ./internal/tui TestSkillsCommand TestSkillsCommandEmpty TestSkillSuggest TestSkillBuiltinWins TestSkillSecretRedacted TestSkillRun
CMD
expect_ok "phase 2 exit criterion: skills package names present" <<'CMD'
go_tests ./internal/skills TestSkillProjectOverrides TestSkillBadSkipped
CMD
expect_ok "phase 2 exit criterion: whole -run set passes" <<'CMD'
go test -count=1 ./internal/skills/ ./internal/tui/ -run 'TestSkill|TestSkillsCommand'
CMD
expect_ok "help lists /skills" <<'CMD'
go test -count=1 ./internal/tui -run '^TestHelp' && grep -q -e '"/skills"' internal/tui/help.go
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
	"yipyip/internal/provider/providertest"
)

func zzPut(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func zzModel(t *testing.T, skills map[string]string) (Model, *agent.Session) {
	t.Helper()
	srv := providertest.New(providertest.Response{Text: "ok"})
	t.Cleanup(srv.Close)
	return newSessionModelOpts(t, srv, "start", func(o *Options) {
		home := filepath.Join(filepath.Dir(o.StartDir), "probehome")
		for rel, body := range skills {
			root := filepath.Join(o.StartDir, ".yipyip", "skills")
			if strings.HasPrefix(rel, "global:") {
				rel, root = strings.TrimPrefix(rel, "global:"), filepath.Join(home, ".config", "yipyip", "skills")
			}
			zzPut(t, filepath.Join(root, rel, "SKILL.md"), body)
		}
		o.Home = home
	})
}

func zzType(m Model, s string) Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return next.(Model)
}

func zzView(m Model) string { return ansi.Strip(m.View()) }

var zzSet = map[string]string{
	"greet":         "---\ndescription: Say hi SECRET123\n---\nHello.\n",
	"global:lint":   "---\ndescription: Lint all\n---\nLint.\n",
	"global:review": "---\ndescription: GLOBALDESC\n---\nG.\n",
	"review":        "---\ndescription: PROJECTDESC\n---\nP.\n",
	"help":          "---\ndescription: SHADOWDESC\n---\nS.\n",
}

func TestZZPosList(t *testing.T) {
	m, sess := zzModel(t, zzSet)
	m, _ = m.Command("/skills")
	v := zzView(m)
	for _, want := range []string{"/greet", "Say hi [redacted]", "(project)", "/lint", "Lint all", "(global)", "PROJECTDESC", "shadowed by built-in"} {
		if !strings.Contains(v, want) {
			t.Errorf("/skills lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "GLOBALDESC") || strings.Contains(v, "SECRET123") {
		t.Errorf("overridden global skill or secret shown:\n%s", v)
	}
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "SHADOWDESC") && !strings.Contains(l, "shadowed by built-in") {
			t.Errorf("shadowed skill without note: %q", l)
		}
	}
	zzPut(t, filepath.Join(sess.StartDir(), ".yipyip", "skills", "added", "SKILL.md"), "---\ndescription: ADDEDDESC\n---\nA.\n")
	m, _ = m.Command("/skills")
	if !strings.Contains(zzView(m), "ADDEDDESC") {
		t.Errorf("/skills did not re-read:\n%s", zzView(m))
	}
}

func TestZZPosEmpty(t *testing.T) {
	m, _ := zzModel(t, nil)
	m, _ = m.Command("/skills")
	if !strings.Contains(zzView(m), "no skills found") {
		t.Fatalf("empty case:\n%s", zzView(m))
	}
}

func TestZZPosSuggest(t *testing.T) {
	m, _ := zzModel(t, zzSet)
	m = zzType(m, "/gr")
	v := zzView(m)
	if !strings.Contains(v, "/greet") || !strings.Contains(v, "Say hi") {
		t.Fatalf("no skill suggestion:\n%s", v)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := next.(Model).input.Value(); got != "/greet" {
		t.Fatalf("Tab gave %q", got)
	}
	m2, _ := zzModel(t, zzSet)
	if v := zzView(zzType(m2, "/he")); strings.Contains(v, "SHADOWDESC") {
		t.Fatalf("shadowed skill suggested:\n%s", v)
	}
}

// TestZZEvilShown passes only when a secret or a shadowed skill is shown where it must not be.
func TestZZEvilShown(t *testing.T) {
	m, _ := zzModel(t, zzSet)
	for _, v := range []string{zzView(zzType(m, "/gr")), zzView(zzType(m, "/he"))} {
		if strings.Contains(v, "SECRET123") || strings.Contains(v, "SHADOWDESC") {
			return
		}
	}
	m, _ = m.Command("/skills")
	if strings.Contains(zzView(m), "SECRET123") {
		return
	}
	t.Fatal("REJECTED: secret redacted and shadowed skill not suggested")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/tui/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: /skills listing, override, shadow note, re-read, empty, suggestions" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/tui TestZZPosList TestZZPosEmpty TestZZPosSuggest
CMD
expect_fail "negative: SECRET123 never shown, shadowed skill never suggested" <<'CMD'
go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvilShown$' ./internal/tui
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
