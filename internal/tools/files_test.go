package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yipyip/internal/config"
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

func readEnv(t *testing.T) *fileEnv {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &fileEnv{root: filepath.Join(base, "start"), outside: filepath.Join(base, "outside")}
	for _, d := range []string{e.root, e.outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(e.outside, "secret.txt"), []byte("TOPSECRET\nkey SECRET123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(e.outside, filepath.Join(e.root, "link")); err != nil {
		t.Fatal(err)
	}
	e.reg = newTestRegistry(t, Options{StartDir: e.root, Redactor: config.NewRedactor("SECRET123"),
		BeforeWrite: func(p string) { e.hooked = append(e.hooked, p) }})
	return e
}

func (e *fileEnv) outPaths() []string {
	return []string{e.outside, "../outside", "link"}
}

func TestReadFileAnywhere(t *testing.T) {
	e := readEnv(t)
	e.put(t, "in.txt", "inside\n")
	for _, p := range []string{filepath.Join(e.outside, "secret.txt"), "../outside/secret.txt", "link/secret.txt"} {
		res := e.run("read_file", map[string]any{"path": p})
		if res.IsError || !strings.Contains(res.Output, "TOPSECRET") {
			t.Errorf("read %q: %+v", p, res)
		}
	}
	if res := e.run("read_file", map[string]any{"path": "in.txt"}); res.IsError || res.Output != "inside\n" {
		t.Errorf("inside read: %+v", res)
	}
}

func TestReadGlobOutside(t *testing.T) {
	e := readEnv(t)
	e.put(t, "in.txt", "inside\n")
	abs := filepath.Join(e.outside, "secret.txt")
	for _, p := range e.outPaths() {
		res := e.run("glob", map[string]any{"pattern": "*.txt", "path": p})
		if res.IsError || strings.TrimSpace(res.Output) != abs {
			t.Errorf("glob %q: %+v", p, res)
		}
	}
	res := e.run("glob", map[string]any{"pattern": "*.txt"})
	if res.IsError || !strings.Contains(res.Output, "in.txt") || strings.Contains(res.Output, e.root) {
		t.Errorf("inside glob: %+v", res)
	}
}

func TestReadGrepOutside(t *testing.T) {
	e := readEnv(t)
	abs := filepath.Join(e.outside, "secret.txt")
	for _, p := range e.outPaths() {
		res := e.run("grep", map[string]any{"pattern": "TOPSECRET", "path": p})
		if res.IsError || strings.TrimSpace(res.Output) != abs+":1:TOPSECRET" {
			t.Errorf("grep %q: %+v", p, res)
		}
		res = e.run("grep", map[string]any{"pattern": "TOPSECRET", "path": p, "glob": "*.txt"})
		if res.IsError || !strings.Contains(res.Output, abs+":1:TOPSECRET") {
			t.Errorf("grep glob %q: %+v", p, res)
		}
	}
}

func TestReadEtcStyle(t *testing.T) {
	if _, err := os.Stat("/etc/passwd"); err != nil {
		t.Skip("no /etc/passwd")
	}
	e := readEnv(t)
	if res := e.run("read_file", map[string]any{"path": "/etc/passwd"}); res.IsError {
		t.Fatalf("read /etc/passwd: %+v", res)
	}
}

func TestReadRedactedOutside(t *testing.T) {
	e := readEnv(t)
	res := e.run("read_file", map[string]any{"path": "link/secret.txt"})
	if res.IsError || strings.Contains(res.Output, "SECRET123") || !strings.Contains(res.Output, "[redacted]") {
		t.Errorf("read_file: %+v", res)
	}
	res = e.run("grep", map[string]any{"pattern": "key", "path": e.outside})
	if res.IsError || strings.Contains(res.Output, "SECRET123") || !strings.Contains(res.Output, "[redacted]") {
		t.Errorf("grep: %+v", res)
	}
}

func TestReadDanglingSymlink(t *testing.T) {
	e := readEnv(t)
	if err := os.Symlink(filepath.Join(e.outside, "nowhere"), filepath.Join(e.root, "dang")); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"read_file", "glob", "grep"} {
		args := map[string]any{"path": "dang", "pattern": "x"}
		if res := e.run(tool, args); !res.IsError {
			t.Errorf("%s dangling accepted: %+v", tool, res)
		}
	}
}

func TestWriteConfined(t *testing.T) {
	e := readEnv(t)
	secret := filepath.Join(e.outside, "secret.txt")
	for _, p := range []string{filepath.Join(e.outside, "x.txt"), "../outside/x.txt", "link/x.txt", secret, "link/secret.txt", "link/sub/new.txt"} {
		res := e.run("write_file", map[string]any{"path": p, "content": "PWNED"})
		if !res.IsError || !strings.Contains(res.Output, "outside the start directory") {
			t.Errorf("write_file %q: %+v", p, res)
		}
	}
	for _, p := range []string{secret, "../outside/secret.txt", "link/secret.txt", "link/x.txt"} {
		res := e.run("edit_file", map[string]any{"path": p, "old_string": "TOPSECRET", "new_string": "PWNED"})
		if !res.IsError || !strings.Contains(res.Output, "outside the start directory") {
			t.Errorf("edit_file %q: %+v", p, res)
		}
	}
	entries, _ := os.ReadDir(e.outside)
	data, _ := os.ReadFile(secret)
	if len(entries) != 1 || !strings.HasPrefix(string(data), "TOPSECRET") || len(e.hooked) != 0 {
		t.Errorf("outside changed: entries=%d data=%q hooked=%v", len(entries), data, e.hooked)
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
