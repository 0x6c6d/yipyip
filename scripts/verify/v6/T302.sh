#!/usr/bin/env bash
# Verification for T302 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named read/write tests" <<'CMD'
go_tests ./internal/tools TestReadFileAnywhere TestReadGlobOutside TestReadGrepOutside TestReadEtcStyle TestReadRedactedOutside TestReadDanglingSymlink TestWriteConfined
CMD
expect_ok "spec acceptance: go test ./internal/tools -run Read and -run Write" <<'CMD'
go test -count=1 ./internal/tools -run Read && go test -count=1 ./internal/tools -run Write
CMD
expect_ok "plan phase 3 exit (tools part) and whole tools package" <<'CMD'
go test -count=1 ./internal/tools -run 'Read|Write|PathGuard|Guard' && go test -count=1 ./internal/tools
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yipyip/internal/config"
	"yipyip/internal/tools"
)

type zzEnv struct {
	root, outside string
	reg           *tools.Registry
	hooked        []string
}

func zzSetup(t *testing.T) *zzEnv {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &zzEnv{root: filepath.Join(base, "start"), outside: filepath.Join(base, "outside")}
	for _, d := range []string{e.root, e.outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(e.outside, "secret.txt"), []byte("OUTSIDE-CONTENT\nkey SECRET123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.root, "in.txt"), []byte("INSIDE-CONTENT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(e.outside, filepath.Join(e.root, "link")); err != nil {
		t.Fatal(err)
	}
	e.reg, err = tools.NewRegistry(tools.Options{StartDir: e.root, Redactor: config.NewRedactor("SECRET123"),
		BeforeWrite: func(p string) { e.hooked = append(e.hooked, p) }})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *zzEnv) run(tool string, args map[string]any) tools.Result {
	b, _ := json.Marshal(args)
	return e.reg.Run(context.Background(), tool, b)
}

func TestZZReadAnywherePos(t *testing.T) {
	e := zzSetup(t)
	abs := filepath.Join(e.outside, "secret.txt")
	for _, p := range []string{abs, "../outside/secret.txt", "link/secret.txt"} {
		res := e.run("read_file", map[string]any{"path": p})
		if res.IsError || !strings.Contains(res.Output, "OUTSIDE-CONTENT") || strings.Contains(res.Output, "SECRET123") {
			t.Fatalf("read_file %q: %+v", p, res)
		}
	}
	for _, p := range []string{e.outside, "../outside", "link"} {
		res := e.run("glob", map[string]any{"pattern": "*.txt", "path": p})
		if res.IsError || !strings.Contains(res.Output, abs) {
			t.Fatalf("glob %q: %+v", p, res)
		}
		res = e.run("grep", map[string]any{"pattern": "OUTSIDE", "path": p})
		if res.IsError || !strings.Contains(res.Output, abs+":1:OUTSIDE-CONTENT") {
			t.Fatalf("grep %q: %+v", p, res)
		}
		res = e.run("grep", map[string]any{"pattern": "key", "path": p})
		if res.IsError || strings.Contains(res.Output, "SECRET123") || !strings.Contains(res.Output, "[redacted]") {
			t.Fatalf("grep secret %q: %+v", p, res)
		}
	}
	res := e.run("glob", map[string]any{"pattern": "*.txt"})
	if res.IsError || !strings.Contains(res.Output, "in.txt") || strings.Contains(res.Output, e.root) {
		t.Fatalf("inside glob not relative: %+v", res)
	}
	res = e.run("grep", map[string]any{"pattern": "INSIDE"})
	if res.IsError || !strings.Contains(res.Output, "in.txt:1:INSIDE-CONTENT") || strings.Contains(res.Output, e.root) {
		t.Fatalf("inside grep not relative: %+v", res)
	}
	for _, tl := range e.reg.Tools() {
		if (tl.Name == "read_file" || tl.Name == "glob" || tl.Name == "grep") && tl.Gated {
			t.Fatalf("%s became gated", tl.Name)
		}
	}
}

// TestZZWriteEvil passes (exit 0) only when a write or edit escapes the start
// directory, the hook fires for it, or a dangling symlink is read.
func TestZZWriteEvil(t *testing.T) {
	e := zzSetup(t)
	abs := filepath.Join(e.outside, "secret.txt")
	for _, p := range []string{filepath.Join(e.outside, "new.txt"), "../outside/new.txt", "link/new.txt", "link/sub/new.txt", abs, "link/secret.txt"} {
		res := e.run("write_file", map[string]any{"path": p, "content": "PWNED"})
		if !res.IsError {
			t.Logf("write_file escaped: %q", p)
			return
		}
	}
	for _, p := range []string{abs, "../outside/secret.txt", "link/secret.txt"} {
		res := e.run("edit_file", map[string]any{"path": p, "old_string": "OUTSIDE-CONTENT", "new_string": "PWNED"})
		if !res.IsError {
			t.Logf("edit_file escaped: %q", p)
			return
		}
	}
	entries, _ := os.ReadDir(e.outside)
	data, _ := os.ReadFile(abs)
	if len(entries) != 1 || strings.Contains(string(data), "PWNED") || len(e.hooked) != 0 {
		t.Logf("outside changed or hook fired: entries=%d hooked=%v", len(entries), e.hooked)
		return
	}
	if err := os.Symlink(filepath.Join(e.outside, "nowhere"), filepath.Join(e.root, "dang")); err != nil {
		t.Fatal(err)
	}
	if res := e.run("read_file", map[string]any{"path": "dang"}); !res.IsError {
		t.Log("dangling symlink read")
		return
	}
	t.Fatal("REJECTED: every write/edit escape refused, nothing written, no hook, dangling refused")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/tools/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: outside files read, globbed, grepped with absolute paths and redaction; inside paths relative; reads ungated" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/tools TestZZReadAnywherePos
CMD
expect_fail "negative: a write/edit escapes the start directory, the hook fires, or a dangling symlink is read" <<'CMD'
out="$(go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZWriteEvil$' ./internal/tools 2>&1)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q -e 'REJECTED:' && exit 1
printf '%s\n' "$out" | grep -q -e '^ok' && exit 0
exit 125
CMD
expect_ok "write_file and edit_file still resolve with Resolve, reads with ResolveRead" <<'CMD'
test "$(grep -c -e 'r\.guard\.Resolve(' internal/tools/files.go)" -ge 2 && grep -q -e 'ResolveRead(' internal/tools/files.go && grep -q -e 'ResolveRead(' internal/tools/search.go
CMD
expect_fail "negative: old outside-read tests still present" <<'CMD'
no_match -rn -e 'func TestReadFileOutside' -e 'func TestSearchPathEscape' internal/tools
CMD
expect_fail "negative: a security-critical file differs from HEAD (diff)" <<'CMD'
out="$(git diff --name-only HEAD -- internal/provider/transport.go internal/tools/pathguard.go internal/agent/approval.go internal/tools/bash.go internal/config/redact.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: a file outside files.go, search.go and their tests changed (or a protected path)" <<'CMD'
out="$(git status --porcelain --untracked-files=all)" || exit 125
printf '%s\n' "$out" | sed -e 's/^...//' -e 's/^.* -> //' | grep -v -x -e '' -e 'versions/v6/Tasks.md' | grep -v -E -x -e 'internal/tools/(files|search)(_test)?\.go' | no_match -e .
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet ede3886 -- go.mod go.sum
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
