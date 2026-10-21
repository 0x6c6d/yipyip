package tools

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func searchEnv(t *testing.T) *fileEnv {
	t.Helper()
	e := newFileEnv(t)
	e.put(t, "a.go", "package a\n")
	e.put(t, "x/b.go", "package x\nfunc B() {}\n")
	e.put(t, "x/y/c.go", "package y\n")
	e.put(t, "x/y/c.txt", "func in text\n")
	e.put(t, ".git/config.go", "func B() {}\n")
	e.put(t, "bin.dat", "func B() {}\x00\x01\x02")
	return e
}

func outLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pat, path string
		want      bool
	}{
		{"**/*.go", "a.go", true},
		{"**/*.go", "x/y/c.go", true},
		{"x/**/c.go", "x/c.go", true},
		{"x/**/c.go", "x/y/c.go", true},
		{"x/**/c.go", "x/y/z/c.go", true},
		{"x/*.go", "x/y/c.go", false},
		{"*.go", "x/b.go", false},
		{"*.go", "a.go", true},
		{"x/?.go", "x/b.go", true},
		{"x/[ab].go", "x/b.go", true},
		{"x/[ab].go", "x/c.go", false},
		{"**", "x/y/c.go", true},
		{"x/**", "x/y/c.go", true},
		{"x/**", "y/c.go", false},
		{"[", "a", false},
		{"x/[/c", "x/y/c", false},
		{"", "a", false},
	}
	for _, c := range cases {
		if got := MatchGlob(c.pat, c.path); got != c.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", c.pat, c.path, got, c.want)
		}
	}
}

func TestGlobDoubleStar(t *testing.T) {
	e := searchEnv(t)
	res := e.run("glob", map[string]any{"pattern": "**/*.go"})
	want := []string{"a.go", "x/b.go", "x/y/c.go"}
	if got := outLines(res.Output); res.IsError || !reflect.DeepEqual(got, want) {
		t.Fatalf("glob **/*.go = %q (%+v)", got, res)
	}
	// sub-path: output stays relative to the start directory
	res = e.run("glob", map[string]any{"pattern": "*.go", "path": "x"})
	if got := outLines(res.Output); !reflect.DeepEqual(got, []string{"x/b.go"}) {
		t.Fatalf("glob in x = %q", got)
	}
	if res = e.run("glob", map[string]any{"pattern": "*.rs"}); res.IsError || res.Output != "no matches" {
		t.Errorf("no match: %+v", res)
	}
	if res = e.run("glob", map[string]any{"pattern": "*", "path": "a.go"}); res.IsError {
		t.Errorf("file path: %+v", res)
	}
	// symlinks are neither followed nor listed
	if res = e.run("glob", map[string]any{"pattern": "**/*"}); strings.Contains(res.Output, "secret.txt") || strings.Contains(res.Output, "out") {
		t.Errorf("symlink followed or listed: %q", res.Output)
	}
}

func TestGlobTruncates(t *testing.T) {
	e := newFileEnv(t)
	for i := 0; i < 1005; i++ {
		e.put(t, "d/f"+strconv.Itoa(i)+".txt", "")
	}
	res := e.run("glob", map[string]any{"pattern": "**/*.txt"})
	lines := outLines(res.Output)
	if len(lines) != 1001 || lines[1000] != "[results truncated: 5 more]" || !sort.StringsAreSorted(lines[:1000]) {
		t.Fatalf("lines=%d last=%q", len(lines), lines[len(lines)-1])
	}
}

func TestGlobSkipsGit(t *testing.T) {
	e := searchEnv(t)
	res := e.run("glob", map[string]any{"pattern": "**/*"})
	if res.IsError || strings.Contains(res.Output, ".git") {
		t.Fatalf("glob listed .git: %+v", res)
	}
	if !strings.Contains(res.Output, "a.go") {
		t.Fatalf("glob output: %+v", res)
	}
}

func TestGrepFormat(t *testing.T) {
	e := searchEnv(t)
	res := e.run("grep", map[string]any{"pattern": "func B"})
	if res.IsError || res.Output != "x/b.go:2:func B() {}" {
		t.Fatalf("grep = %+v", res)
	}
	// single file and sub-directory: paths stay relative to the start dir
	if res = e.run("grep", map[string]any{"pattern": "package", "path": "x/b.go"}); res.Output != "x/b.go:1:package x" {
		t.Errorf("single file: %+v", res)
	}
	res = e.run("grep", map[string]any{"pattern": "^package", "path": "x"})
	if got := outLines(res.Output); !reflect.DeepEqual(got, []string{"x/b.go:1:package x", "x/y/c.go:1:package y"}) {
		t.Errorf("sub dir: %q", got)
	}
	if res = e.run("grep", map[string]any{"pattern": "zzz"}); res.IsError || res.Output != "no matches" {
		t.Errorf("no match: %+v", res)
	}
}

func TestGrepLongLine(t *testing.T) {
	e := newFileEnv(t)
	e.put(t, "long.txt", strings.Repeat("a", 3<<20)+"\nneedle\n")
	res := e.run("grep", map[string]any{"pattern": "needle"})
	if res.IsError || res.Output != "long.txt:2:needle" {
		t.Fatalf("grep after long line: %.100q", res.Output)
	}
}

func TestGrepTruncates(t *testing.T) {
	e := newFileEnv(t)
	e.put(t, "m.txt", strings.Repeat("hit\n", 1003))
	lines := outLines(e.run("grep", map[string]any{"pattern": "hit"}).Output)
	if len(lines) != 1001 || lines[0] != "m.txt:1:hit" || lines[1000] != "[results truncated: 3 more matches]" {
		t.Fatalf("lines=%d last=%q", len(lines), lines[len(lines)-1])
	}
}

func TestGrepGlobFilter(t *testing.T) {
	e := searchEnv(t)
	res := e.run("grep", map[string]any{"pattern": "^func", "glob": "*.txt"})
	if res.Output != "x/y/c.txt:1:func in text" {
		t.Fatalf("base-name glob: %+v", res)
	}
	res = e.run("grep", map[string]any{"pattern": "^package", "glob": "x/*.go"})
	if res.Output != "x/b.go:1:package x" {
		t.Fatalf("path glob: %+v", res)
	}
	res = e.run("grep", map[string]any{"pattern": "^package", "glob": "**/*.go"})
	if got := outLines(res.Output); len(got) != 3 {
		t.Fatalf("** glob: %q", got)
	}
}

func TestGrepSkipsBinary(t *testing.T) {
	e := searchEnv(t)
	if err := os.WriteFile(filepath.Join(e.root, "late.dat"), append([]byte("func B\n"+strings.Repeat("x", 8100)+"\n"), 0), 0o644); err != nil {
		t.Fatal(err)
	}
	res := e.run("grep", map[string]any{"pattern": "func B"})
	if strings.Contains(res.Output, "bin.dat") || strings.Contains(res.Output, ".git") {
		t.Fatalf("binary or .git searched: %q", res.Output)
	}
	// a NUL after the first 8000 bytes does not make a file binary
	if !strings.Contains(res.Output, "late.dat:1:func B") {
		t.Errorf("late-NUL file skipped: %q", res.Output)
	}
}

func TestGrepInvalidRegex(t *testing.T) {
	e := searchEnv(t)
	res := e.run("grep", map[string]any{"pattern": "("})
	if !res.IsError || !strings.Contains(res.Output, "missing closing )") {
		t.Fatalf("invalid regex: %+v", res)
	}
}

func TestSearchPathEscape(t *testing.T) {
	e := searchEnv(t)
	for _, tool := range []string{"glob", "grep"} {
		for _, p := range []string{"../", e.outside, "out"} {
			res := e.run(tool, map[string]any{"pattern": "x", "path": p})
			if !res.IsError || !strings.Contains(res.Output, "outside the start directory") {
				t.Errorf("%s path %q: %+v", tool, p, res)
			}
		}
	}
}
