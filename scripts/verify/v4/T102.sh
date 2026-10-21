#!/usr/bin/env bash
# Verification for T102 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named tui skill tests" <<'CMD'
go_tests ./internal/tui TestSkillRun TestSkillRunNoArgs TestSkillUnknown
CMD
expect_ok "phase 1 exit criterion" <<'CMD'
go test -count=1 -v ./internal/skills/ ./internal/tui/ -run 'TestSkill' 2>&1 | grep -q -- '--- PASS: TestSkillRun'
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/provider/providertest"
)

func zzSkillModel(t *testing.T, srv *providertest.Server) (Model, func()) {
	t.Helper()
	m, sess := newSessionModelOpts(t, srv, "start", func(o *Options) {
		p := filepath.Join(o.StartDir, ".yipyip", "skills", "greet", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\nname: greet\ndescription: Say hi\n---\n\nHello there.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		o.Home = ""
	})
	return m, func() { waitSession(t, sess) }
}

func zzLastUser(t *testing.T, srv *providertest.Server) string {
	t.Helper()
	reqs := srv.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request sent")
	}
	last := ""
	for _, msg := range reqs[len(reqs)-1].Messages {
		if msg.Role == "user" {
			last = msg.Content
		}
	}
	return last
}

func TestZZPosSkillArgs(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	m, wait := zzSkillModel(t, srv)
	m, _ = m.Command("/greet Bob and Alice")
	wait()
	m = m.Drain()
	if got := zzLastUser(t, srv); got != "Hello there.\n\nBob and Alice" {
		t.Fatalf("user message = %q", got)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "/greet Bob and Alice") {
		t.Fatalf("typed line not in view:\n%s", v)
	}
}

func TestZZPosSkillNoArgs(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	m, wait := zzSkillModel(t, srv)
	m, _ = m.Command("/greet")
	wait()
	_ = m.Drain()
	if got := zzLastUser(t, srv); got != "Hello there." {
		t.Fatalf("user message = %q", got)
	}
}

func TestZZPosSkillUnknown(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	m, wait := zzSkillModel(t, srv)
	m, _ = m.Command("/greetx Bob")
	wait()
	m = m.Drain()
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("%d requests sent for an unknown command", n)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "unknown command: /greetx") {
		t.Fatalf("no unknown-command line:\n%s", v)
	}
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/tui/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: skill with and without args, unknown name sends nothing" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/tui TestZZPosSkillArgs TestZZPosSkillNoArgs TestZZPosSkillUnknown
CMD
expect_ok "cmd/yip passes the home directory to the TUI" <<'CMD'
grep -n -e 'Home' cmd/yip/main.go | grep -q -e 'Home *[:=]'
CMD
expect_fail "negative: tui never writes into a skills folder" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'skills.*os.WriteFile' -e 'os.WriteFile.*skills' internal/tui
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
