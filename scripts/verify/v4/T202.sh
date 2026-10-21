#!/usr/bin/env bash
# Verification for T202 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named tui skill tests (new and T102)" <<'CMD'
go_tests ./internal/tui TestSkillBuiltinWins TestSkillSecretRedacted TestSkillWarnings TestSkillArgsAppended TestSkillRun TestSkillRunNoArgs TestSkillUnknown
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/provider/providertest"
	"yipyip/internal/session"
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

type zzRun struct {
	m     Model
	srv   *providertest.Server
	saved []*session.SessionData
	wait  func()
}

func zzSetup(t *testing.T, skills map[string]string) *zzRun {
	t.Helper()
	r := &zzRun{srv: providertest.New(providertest.Response{Text: "ok"})}
	t.Cleanup(r.srv.Close)
	m, sess := newSessionModelOpts(t, r.srv, "start", func(o *Options) {
		home := filepath.Join(filepath.Dir(o.StartDir), "probehome")
		for rel, body := range skills {
			root := filepath.Join(o.StartDir, ".yipyip", "skills")
			if strings.HasPrefix(rel, "global:") {
				rel, root = strings.TrimPrefix(rel, "global:"), filepath.Join(home, ".config", "yipyip", "skills")
			}
			zzPut(t, filepath.Join(root, rel, "SKILL.md"), body)
		}
		o.Home = home
		o.SaveOnExit = func(d *session.SessionData) error { r.saved = append(r.saved, d); return nil }
	})
	r.m = m
	r.wait = func() { waitSession(t, sess) }
	return r
}

func (r *zzRun) run(line string) {
	r.m, _ = r.m.Command(line)
	r.wait()
	r.m = r.m.Drain()
}

func (r *zzRun) bodies() string {
	var b strings.Builder
	for _, q := range r.srv.Requests() {
		for _, msg := range q.Messages {
			b.WriteString(msg.Content + "\n")
		}
	}
	return b.String()
}

func zzSecretRun(t *testing.T) *zzRun {
	r := zzSetup(t, map[string]string{"leak": "---\ndescription: d\n---\nUse key SECRET123 now.\n"})
	r.run("/leak with SECRET123 too")
	return r
}

func TestZZPosRedacted(t *testing.T) {
	r := zzSecretRun(t)
	sent := r.bodies()
	if strings.Contains(sent, "SECRET123") || !strings.Contains(sent, "[redacted]") || !strings.Contains(sent, "Use key") {
		t.Fatalf("model got %q", sent)
	}
	if v := ansi.Strip(r.m.View()); strings.Contains(v, "SECRET123") {
		t.Fatalf("secret in view:\n%s", v)
	}
	r.m, _ = r.m.Command("/exit")
	if len(r.saved) == 0 {
		t.Fatal("nothing saved on /exit")
	}
	for _, d := range r.saved {
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "SECRET123") {
			t.Fatalf("secret in saved session: %s", b)
		}
	}
}

func zzShadowRun(t *testing.T) *zzRun {
	r := zzSetup(t, map[string]string{"help": "---\n---\nSKILLBODY\n", "global:model": "---\n---\nSKILLMODEL\n"})
	r.run("/help")
	return r
}

func TestZZPosBuiltinWins(t *testing.T) {
	r := zzShadowRun(t)
	if n := len(r.srv.Requests()); n != 0 {
		t.Fatalf("%d requests for /help", n)
	}
	v := ansi.Strip(r.m.View())
	if strings.Contains(v, "SKILLBODY") || !strings.Contains(v, "/sessions") {
		t.Fatalf("help not shown or skill body shown:\n%s", v)
	}
}

func TestZZPosWarningAndGlobal(t *testing.T) {
	r := zzSetup(t, map[string]string{"Bad_One": "---\n---\nx\n", "global:hello": "---\n---\nHi from global.\n"})
	v := ansi.Strip(r.m.View())
	if strings.Count(v, "warning:") != 1 || !strings.Contains(v, "Bad_One") {
		t.Fatalf("want one warning naming Bad_One:\n%s", v)
	}
	r.run("/hello x y")
	if !strings.Contains(r.bodies(), "Hi from global.\n\nx y") {
		t.Fatalf("global skill not sent: %q", r.bodies())
	}
}

// TestZZEvilLeak passes only when a secret or a shadowing skill reached the model.
func TestZZEvilLeak(t *testing.T) {
	if strings.Contains(zzSecretRun(t).bodies(), "SECRET123") {
		return
	}
	if strings.Contains(zzShadowRun(t).bodies(), "SKILLBODY") {
		return
	}
	t.Fatal("REJECTED: secret redacted and built-in won")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/tui/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: redaction (request, view, saved session), built-in wins, warning, global skill" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/tui TestZZPosRedacted TestZZPosBuiltinWins TestZZPosWarningAndGlobal
CMD
expect_fail "negative: SECRET123 or a shadowing skill never reaches the model" <<'CMD'
go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvilLeak$' ./internal/tui
CMD
expect_fail "negative: tui has no redaction logic of its own (no Replace of secrets)" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e '"\[redacted\]"' internal/tui
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
