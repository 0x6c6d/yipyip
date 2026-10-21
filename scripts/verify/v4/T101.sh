#!/usr/bin/env bash
# Verification for T101 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named tests of internal/skills" <<'CMD'
go_tests ./internal/skills TestSkillLoadProject TestSkillFrontmatter TestSkillMissingDir TestSkillSymlinkEscape TestSkillTooLarge
CMD
expect_ok "exported API present" <<'CMD'
has_symbols ./internal/skills Skill Source Global Project GlobalDir ProjectDir Parse Load
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

func zzStart(t *testing.T) (string, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	start := filepath.Join(base, "start")
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatal(err)
	}
	return base, start
}

func zzFind(list []Skill, name string) *Skill {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

func TestZZPosProject(t *testing.T) {
	_, start := zzStart(t)
	sk := filepath.Join(start, ".yipyip", "skills")
	zzWrite(t, filepath.Join(sk, "greet", "SKILL.md"), "---\nname: greet\ndescription: Say hi\n---\n\nHello there.\n")
	zzWrite(t, filepath.Join(sk, "deploy", "SKILL.md"), "---\r\ndescription: \"Ship it\"\r\nfoo: bar\r\n---\r\nRun the deploy.\r\n")
	if err := os.MkdirAll(filepath.Join(sk, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	list, warns := Load("", start)
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %q", warns)
	}
	if len(list) != 2 || list[0].Name != "deploy" || list[1].Name != "greet" {
		t.Fatalf("want [deploy greet] sorted, got %+v", list)
	}
	g := zzFind(list, "greet")
	if g.Description != "Say hi" || g.Body != "Hello there." || string(g.Source) != "project" {
		t.Fatalf("greet: %+v", *g)
	}
	d := zzFind(list, "deploy")
	if d.Description != "Ship it" || d.Body != "Run the deploy." {
		t.Fatalf("deploy: %+v", *d)
	}
}

func TestZZPosMissingDir(t *testing.T) {
	_, start := zzStart(t)
	list, warns := Load("", start)
	if len(list) != 0 || len(warns) != 0 {
		t.Fatalf("missing dir: %+v %q", list, warns)
	}
}

func TestZZPosParseErrors(t *testing.T) {
	for _, in := range []string{"no frontmatter\n", "---\nname: x\nbody without end\n", "---\nname: Bad_Name\n---\nb\n", "---\njust words\n---\nb\n"} {
		if _, err := Parse("ok", []byte(in)); err == nil {
			t.Errorf("Parse accepted %q", in)
		}
	}
	s, err := Parse("my-skill", []byte("---\n# c\n\ndescription: 'd'\n---\nB\n"))
	if err != nil || s.Name != "my-skill" || s.Description != "d" || s.Body != "B" {
		t.Fatalf("Parse: %+v %v", s, err)
	}
}

// zzEvil builds escapes and oversized/bad entries; returns what Load gave.
func zzEvil(t *testing.T) ([]Skill, []string) {
	base, start := zzStart(t)
	sk := filepath.Join(start, ".yipyip", "skills")
	out := filepath.Join(base, "outside")
	zzWrite(t, filepath.Join(out, "evil", "SKILL.md"), "---\nname: evil\n---\nleak\n")
	zzWrite(t, filepath.Join(out, "f.md"), "---\nname: evilfile\n---\nleak\n")
	if err := os.MkdirAll(filepath.Join(sk, "evilfile"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(out, "evil"), filepath.Join(sk, "evil")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(out, "f.md"), filepath.Join(sk, "evilfile", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	zzWrite(t, filepath.Join(sk, "evilbig", "SKILL.md"), "---\nname: evilbig\n---\n"+strings.Repeat("x", 300*1024))
	zzWrite(t, filepath.Join(sk, "evilbad", "SKILL.md"), "---\nname: evilbad\nno end\n")
	zzWrite(t, filepath.Join(sk, "good", "SKILL.md"), "---\n---\nfine\n")
	return Load("", start)
}

func TestZZPosEscapesSkipped(t *testing.T) {
	list, warns := zzEvil(t)
	for _, s := range list {
		if strings.HasPrefix(s.Name, "evil") {
			t.Errorf("loaded %q", s.Name)
		}
	}
	if zzFind(list, "good") == nil {
		t.Errorf("valid sibling not loaded: %+v", list)
	}
	for _, f := range []string{"evil", "evilfile", "evilbig", "evilbad"} {
		found := false
		for _, w := range warns {
			if strings.Contains(w, f) && !strings.Contains(w, "\n") {
				found = true
			}
		}
		if !found {
			t.Errorf("no warning line naming %q in %q", f, warns)
		}
	}
}

func TestZZPosDirSymlinkEscape(t *testing.T) {
	base, start := zzStart(t)
	out := filepath.Join(base, "outside")
	zzWrite(t, filepath.Join(out, "skills", "x", "SKILL.md"), "---\nname: x\n---\nleak\n")
	if err := os.Symlink(out, filepath.Join(start, ".yipyip")); err != nil {
		t.Fatal(err)
	}
	if list, _ := Load("", start); len(list) != 0 {
		t.Fatalf("loaded skills through an escaping .yipyip symlink: %+v", list)
	}
}

// TestZZEvilAccepted passes only when an unsafe entry was loaded.
func TestZZEvilAccepted(t *testing.T) {
	list, _ := zzEvil(t)
	for _, s := range list {
		if strings.HasPrefix(s.Name, "evil") {
			return
		}
	}
	t.Fatal("REJECTED: every unsafe entry was skipped")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/skills/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: loading, defaults, missing dir, escapes skipped with warnings" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/skills TestZZPosProject TestZZPosMissingDir TestZZPosParseErrors TestZZPosEscapesSkipped TestZZPosDirSymlinkEscape
CMD
expect_fail "negative: no symlink escape, oversized or bad file is ever loaded" <<'CMD'
go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvilAccepted$' ./internal/skills
CMD
expect_fail "negative: internal/skills never writes, execs or talks http" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'os.WriteFile' -e 'os.Create' -e 'os.OpenFile' -e 'os.Mkdir' -e 'os.Remove' -e 'os.Rename' -e '"os/exec"' -e '"net/http"' internal/skills
CMD
expect_fail "negative: no YAML or other new dependency" <<'CMD'
no_match -rn --include='*.go' -e 'yaml"' internal/skills
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
