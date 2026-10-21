package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fileEnv struct {
	root, outside string
	reg           *Registry
	hooked        []string
}

func newFileEnv(t *testing.T) *fileEnv {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &fileEnv{root: filepath.Join(base, "root"), outside: filepath.Join(base, "outside")}
	for _, d := range []string{e.root, e.outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(e.outside, "secret.txt"), []byte("TOPSECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(e.outside, filepath.Join(e.root, "out")); err != nil {
		t.Fatal(err)
	}
	e.reg = newTestRegistry(t, Options{StartDir: e.root, BeforeWrite: func(p string) { e.hooked = append(e.hooked, p) }})
	return e
}

func (e *fileEnv) run(name string, args map[string]any) Result {
	b, _ := json.Marshal(args)
	return e.reg.Run(context.Background(), name, b)
}

func (e *fileEnv) put(t *testing.T, rel, content string) string {
	t.Helper()
	p := filepath.Join(e.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadFile(t *testing.T) {
	e := newFileEnv(t)
	e.put(t, "a/b.txt", "hello\nworld\n")
	if res := e.run("read_file", map[string]any{"path": "a/b.txt"}); res.IsError || res.Output != "hello\nworld\n" {
		t.Fatalf("read: %+v", res)
	}
	if res := e.run("read_file", map[string]any{"path": "a"}); !res.IsError {
		t.Errorf("directory read accepted: %+v", res)
	}
	if res := e.run("read_file", map[string]any{"path": "nope.txt"}); !res.IsError {
		t.Errorf("missing read accepted: %+v", res)
	}
	big := strings.Repeat("x", 10_000_000)
	e.put(t, "big.txt", big)
	if res := e.run("read_file", map[string]any{"path": "big.txt"}); res.IsError || res.Output != Truncate(big) {
		t.Errorf("big read not truncated (len %d)", len(res.Output))
	}
}

func TestReadFileOutside(t *testing.T) {
	e := newFileEnv(t)
	for _, p := range []string{"../outside/secret.txt", "out/secret.txt", filepath.Join(e.outside, "secret.txt")} {
		res := e.run("read_file", map[string]any{"path": p})
		if !res.IsError || strings.Contains(res.Output, "TOPSECRET") || !strings.Contains(res.Output, "outside the start directory") {
			t.Errorf("read %q: %+v", p, res)
		}
	}
}

func TestWriteFileCreatesDirs(t *testing.T) {
	e := newFileEnv(t)
	res := e.run("write_file", map[string]any{"path": "x/y/z.txt", "content": "abc"})
	if res.IsError || res.Output != "wrote 3 bytes to x/y/z.txt" {
		t.Fatalf("write: %+v", res)
	}
	p := filepath.Join(e.root, "x", "y", "z.txt")
	if b, _ := os.ReadFile(p); string(b) != "abc" {
		t.Errorf("content %q", b)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o644 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	// Existing file keeps its mode; empty content is allowed.
	q := e.put(t, "script.sh", "old")
	if err := os.Chmod(q, 0o755); err != nil {
		t.Fatal(err)
	}
	if res := e.run("write_file", map[string]any{"path": "script.sh", "content": ""}); res.IsError {
		t.Fatalf("empty write: %+v", res)
	}
	if fi, _ := os.Stat(q); fi.Mode().Perm() != 0o755 || fi.Size() != 0 {
		t.Errorf("mode %v size %d", fi.Mode().Perm(), fi.Size())
	}
	// Missing content and directory targets are errors.
	if res := e.run("write_file", map[string]any{"path": "m.txt"}); !res.IsError {
		t.Errorf("missing content accepted")
	}
	n := len(e.hooked)
	if res := e.run("write_file", map[string]any{"path": "x", "content": "d"}); !res.IsError || len(e.hooked) != n {
		t.Errorf("write onto directory: %+v hooks %d->%d", res, n, len(e.hooked))
	}
}

func TestWriteFileOutsideRejected(t *testing.T) {
	e := newFileEnv(t)
	for _, p := range []string{"out/evil.txt", "out/sub/evil.txt", "../outside/evil.txt", filepath.Join(e.outside, "evil.txt")} {
		res := e.run("write_file", map[string]any{"path": p, "content": "x"})
		if !res.IsError || !strings.Contains(res.Output, "outside the start directory") {
			t.Errorf("write %q: %+v", p, res)
		}
	}
	entries, _ := os.ReadDir(e.outside)
	if len(entries) != 1 || entries[0].Name() != "secret.txt" {
		t.Errorf("outside dir changed: %v", entries)
	}
	if len(e.hooked) != 0 {
		t.Errorf("BeforeWrite called: %v", e.hooked)
	}
}

func TestEditFileMissing(t *testing.T) {
	e := newFileEnv(t)
	p := e.put(t, "f.txt", "alpha beta")
	cases := []map[string]any{
		{"path": "nope.txt", "old_string": "a", "new_string": "b"},
		{"path": "f.txt", "old_string": "zzz", "new_string": "b"},
		{"path": "f.txt", "old_string": "", "new_string": "b"},
		{"path": "out/secret.txt", "old_string": "TOP", "new_string": "b"},
	}
	for _, c := range cases {
		if res := e.run("edit_file", c); !res.IsError {
			t.Errorf("accepted %v: %+v", c, res)
		}
	}
	res := e.run("edit_file", cases[1])
	if res.Output != "old_string not found in f.txt" {
		t.Errorf("message %q", res.Output)
	}
	if b, _ := os.ReadFile(p); string(b) != "alpha beta" || len(e.hooked) != 0 {
		t.Errorf("file %q hooks %v", b, e.hooked)
	}
	if b, _ := os.ReadFile(filepath.Join(e.outside, "secret.txt")); string(b) != "TOPSECRET" {
		t.Errorf("outside changed: %q", b)
	}
}

func TestEditFileAmbiguous(t *testing.T) {
	e := newFileEnv(t)
	p := e.put(t, "f.txt", "x x x")
	res := e.run("edit_file", map[string]any{"path": "f.txt", "old_string": "x", "new_string": "y"})
	if !res.IsError || res.Output != "old_string is ambiguous: 3 occurrences in f.txt" {
		t.Fatalf("res %+v", res)
	}
	if b, _ := os.ReadFile(p); string(b) != "x x x" || len(e.hooked) != 0 {
		t.Errorf("file %q hooks %v", b, e.hooked)
	}
}

func TestEditFileOK(t *testing.T) {
	e := newFileEnv(t)
	p := e.put(t, "d/f.sh", "one\ntwo\nthree\n")
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
	res := e.run("edit_file", map[string]any{"path": "d/f.sh", "old_string": "two\n", "new_string": ""})
	if res.IsError || res.Output != "edited d/f.sh" {
		t.Fatalf("res %+v", res)
	}
	if b, _ := os.ReadFile(p); string(b) != "one\nthree\n" {
		t.Errorf("content %q", b)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o755 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
}

func TestBeforeWriteHook(t *testing.T) {
	e := newFileEnv(t)
	e.put(t, "f.txt", "a b")
	e.run("write_file", map[string]any{"path": "n/new.txt", "content": "1"})
	e.run("write_file", map[string]any{"path": "f.txt", "content": "2"})
	e.run("edit_file", map[string]any{"path": "f.txt", "old_string": "2", "new_string": "3"})
	want := []string{filepath.Join(e.root, "n", "new.txt"), filepath.Join(e.root, "f.txt"), filepath.Join(e.root, "f.txt")}
	if strings.Join(e.hooked, "|") != strings.Join(want, "|") {
		t.Fatalf("hooks %v, want %v", e.hooked, want)
	}
	// Failed operations do not call the hook.
	e.run("edit_file", map[string]any{"path": "f.txt", "old_string": "nothere", "new_string": "x"})
	e.run("edit_file", map[string]any{"path": "missing", "old_string": "a", "new_string": "x"})
	e.run("write_file", map[string]any{"path": "n", "content": "x"})
	e.run("write_file", map[string]any{"path": "out/x", "content": "x"})
	e.run("read_file", map[string]any{"path": "f.txt"})
	if len(e.hooked) != 3 {
		t.Errorf("unexpected hook calls: %v", e.hooked)
	}
}
