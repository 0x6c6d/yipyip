#!/usr/bin/env bash
# Verification for T201 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named tests of internal/skills (new and T101)" <<'CMD'
go_tests ./internal/skills TestSkillGlobalOnly TestSkillProjectOnly TestSkillProjectOverrides TestSkillBadSkipped TestSkillMissingDirs TestSkillLoadProject TestSkillSymlinkEscape TestSkillTooLarge
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func zzWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// zzDirs returns base, home and start (both created, no skills yet).
func zzDirs(t *testing.T) (string, string, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, start := filepath.Join(base, "home"), filepath.Join(base, "start")
	for _, d := range []string{home, start} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return base, home, start
}

func zzFind(list []Skill, name string) *Skill {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

func zzOverride(t *testing.T) []Skill {
	_, home, start := zzDirs(t)
	g := filepath.Join(home, ".config", "yipyip", "skills")
	p := filepath.Join(start, ".yipyip", "skills")
	zzWrite(t, filepath.Join(g, "review", "SKILL.md"), "---\ndescription: global review\n---\nGLOBAL BODY\n")
	zzWrite(t, filepath.Join(g, "only-global", "SKILL.md"), "---\n---\nG\n")
	zzWrite(t, filepath.Join(p, "review", "SKILL.md"), "---\ndescription: project review\n---\nPROJECT BODY\n")
	list, warns := Load(home, start)
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %q", warns)
	}
	return list
}

func TestZZPosGlobalOnly(t *testing.T) {
	_, home, start := zzDirs(t)
	zzWrite(t, filepath.Join(home, ".config", "yipyip", "skills", "lint", "SKILL.md"), "---\ndescription: Lint it\n---\nRun lint.\n")
	list, warns := Load(home, start)
	if len(warns) != 0 || len(list) != 1 || list[0].Name != "lint" || string(list[0].Source) != "global" || list[0].Body != "Run lint." {
		t.Fatalf("global only: %+v %q", list, warns)
	}
}

func TestZZPosOverride(t *testing.T) {
	list := zzOverride(t)
	r := zzFind(list, "review")
	if r == nil || r.Body != "PROJECT BODY" || string(r.Source) != "project" || r.Description != "project review" {
		t.Fatalf("review: %+v", r)
	}
	if zzFind(list, "only-global") == nil || len(list) != 2 {
		t.Fatalf("list: %+v", list)
	}
}

func TestZZPosBadSkipped(t *testing.T) {
	_, home, start := zzDirs(t)
	g := filepath.Join(home, ".config", "yipyip", "skills")
	p := filepath.Join(start, ".yipyip", "skills")
	zzWrite(t, filepath.Join(g, "Bad_Global", "SKILL.md"), "---\n---\nx\n")
	zzWrite(t, filepath.Join(g, "nofm", "SKILL.md"), "just text\n")
	zzWrite(t, filepath.Join(p, "unclosed", "SKILL.md"), "---\nname: unclosed\n")
	zzWrite(t, filepath.Join(p, "badname", "SKILL.md"), "---\nname: Has Space\n---\nx\n")
	zzWrite(t, filepath.Join(p, "fine", "SKILL.md"), "---\n---\nok\n")
	list, warns := Load(home, start)
	if len(list) != 1 || list[0].Name != "fine" {
		t.Fatalf("list: %+v", list)
	}
	if len(warns) != 4 {
		t.Fatalf("want 4 warnings, got %q", warns)
	}
	for _, f := range []string{"Bad_Global", "nofm", "unclosed", "badname"} {
		n := 0
		for _, w := range warns {
			if strings.Contains(w, "\n") {
				t.Fatalf("multi-line warning %q", w)
			}
			if strings.Contains(w, f) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("folder %q named by %d warnings: %q", f, n, warns)
		}
	}
}

func TestZZPosMissing(t *testing.T) {
	_, home, start := zzDirs(t)
	if list, warns := Load(home, start); len(list) != 0 || len(warns) != 0 {
		t.Fatalf("missing dirs: %+v %q", list, warns)
	}
	if list, warns := Load("", start); len(list) != 0 || len(warns) != 0 {
		t.Fatalf("empty home: %+v %q", list, warns)
	}
}

func zzEscape(t *testing.T) []Skill {
	base, home, start := zzDirs(t)
	zzWrite(t, filepath.Join(home, ".config", "yipyip", "skills", "g", "SKILL.md"), "---\n---\nG\n")
	out := filepath.Join(base, "outside", "evil")
	zzWrite(t, filepath.Join(out, "SKILL.md"), "---\nname: evil\n---\nleak\n")
	p := filepath.Join(start, ".yipyip", "skills")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, filepath.Join(p, "evil")); err != nil {
		t.Fatal(err)
	}
	list, _ := Load(home, start)
	return list
}

func TestZZPosEscapeWithGlobal(t *testing.T) {
	list := zzEscape(t)
	if zzFind(list, "evil") != nil || zzFind(list, "g") == nil {
		t.Fatalf("list: %+v", list)
	}
}

// TestZZEvilGlobalWins passes only when the global skill beats the project one
// or a project symlink escape is loaded.
func TestZZEvilGlobalWins(t *testing.T) {
	r := zzFind(zzOverride(t), "review")
	if r != nil && r.Body == "GLOBAL BODY" {
		return
	}
	if zzFind(zzEscape(t), "evil") != nil {
		return
	}
	t.Fatal("REJECTED: project wins and the escape is refused")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/skills/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: global only, override, bad entries, missing dirs, escape with global dir" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/skills TestZZPosGlobalOnly TestZZPosOverride TestZZPosBadSkipped TestZZPosMissing TestZZPosEscapeWithGlobal
CMD
expect_fail "negative: global never beats project, project escape never loaded" <<'CMD'
go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvilGlobalWins$' ./internal/skills
CMD
expect_fail "negative: internal/skills never writes, execs or talks http" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'os.WriteFile' -e 'os.Create' -e 'os.OpenFile' -e 'os.Mkdir' -e 'os.Remove' -e 'os.Rename' -e '"os/exec"' -e '"net/http"' internal/skills
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
