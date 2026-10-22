#!/usr/bin/env bash
# Verification for T301 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named guard read/write tests" <<'CMD'
go_tests ./internal/tools TestGuardReadAbsoluteOutside TestGuardReadDotDot TestGuardReadSymlinkOutside TestGuardReadErrors TestGuardWriteStillConfined
CMD
expect_ok "existing guard tests unchanged and green" <<'CMD'
go_tests ./internal/tools TestGuardRelative TestGuardAbsoluteInside TestGuardDotDotEscape TestGuardAbsoluteOutside TestGuardSymlinkEscape TestGuardSymlinkInside TestGuardNewFile TestGuardDanglingSymlink TestGuardRootSymlinked
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package tools_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"yipyip/internal/tools"
)

func zzDirs(t *testing.T) (root, outside string, g *tools.PathGuard) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, outside = filepath.Join(base, "start"), filepath.Join(base, "outside")
	for _, d := range []string{root, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "nowhere"), filepath.Join(root, "dang")); err != nil {
		t.Fatal(err)
	}
	g, err = tools.NewPathGuard(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, outside, g
}

func TestZZGuardReadPos(t *testing.T) {
	root, outside, g := zzDirs(t)
	want := filepath.Join(outside, "f.txt")
	for in, exp := range map[string]string{
		want:                 want,
		"../outside/f.txt":   want,
		"link/f.txt":         want,
		"x.txt":              filepath.Join(root, "x.txt"),
		"/":                  "/",
		outside:              outside,
	} {
		got, err := g.ResolveRead(in)
		if err != nil || got != exp {
			t.Fatalf("ResolveRead(%q) = %q, %v; want %q", in, got, err, exp)
		}
	}
	for _, in := range []string{"x.txt", "sub/new.txt", filepath.Join(root, "a.txt")} {
		if _, err := g.Resolve(in); err != nil {
			t.Fatalf("Resolve(%q) inside: %v", in, err)
		}
	}
}

// TestZZGuardEvil passes (exit 0) only when write mode lets an escape through
// or read mode accepts an empty, NUL or dangling-symlink path.
func TestZZGuardEvil(t *testing.T) {
	_, outside, g := zzDirs(t)
	for _, in := range []string{filepath.Join(outside, "f.txt"), filepath.Join(outside, "new.txt"), "../outside/new.txt", "link/f.txt", "link/sub/new.txt", "link", "..", "/", "/etc/passwd"} {
		got, err := g.Resolve(in)
		if err == nil || !errors.Is(err, tools.ErrOutsideStartDir) {
			t.Logf("write mode accepted %q -> %q (%v)", in, got, err)
			return
		}
	}
	for _, in := range []string{"", "a\x00b", "dang", "dang/x.txt"} {
		if got, err := g.ResolveRead(in); err == nil {
			t.Logf("read mode accepted %q -> %q", in, got)
			return
		}
	}
	t.Fatal("REJECTED: write mode confined, bad read paths refused")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/tools/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: ResolveRead resolves absolute, .., symlinked outside paths; Resolve accepts inside paths" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/tools TestZZGuardReadPos
CMD
expect_fail "negative: write mode lets an escape through or read mode accepts empty/NUL/dangling" <<'CMD'
out="$(go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZGuardEvil$' ./internal/tools 2>&1)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q -e 'REJECTED:' && exit 1
printf '%s\n' "$out" | grep -q -e '^ok' && exit 0
exit 125
CMD
expect_ok "pathguard.go changed against ede3886 (this task owns it)" <<'CMD'
! git diff --quiet ede3886 -- internal/tools/pathguard.go && grep -q -e 'func (g \*PathGuard) ResolveRead' internal/tools/pathguard.go
CMD
expect_fail "negative: another security-critical file differs from HEAD (diff)" <<'CMD'
out="$(git diff --name-only HEAD -- internal/provider/transport.go internal/agent/approval.go internal/tools/bash.go internal/config/redact.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: a file outside pathguard.go and its test changed (callers belong to T302; or a protected path)" <<'CMD'
out="$(git status --porcelain --untracked-files=all)" || exit 125
printf '%s\n' "$out" | sed -e 's/^...//' -e 's/^.* -> //' | grep -v -x -e '' -e 'versions/v6/Tasks.md' | grep -v -E -x -e 'internal/tools/pathguard(_test)?\.go' | no_match -e .
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet ede3886 -- go.mod go.sum
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
