package tools

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// guardFixture creates base/{root,outside} with a few files and links and
// returns the symlink-resolved base, root and outside directories.
func guardFixture(t *testing.T) (base, root, outside string) {
	t.Helper()
	b, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base = b
	root = filepath.Join(base, "root")
	outside = filepath.Join(base, "outside")
	for _, d := range []string{filepath.Join(root, "sub"), outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(root, "a.txt"):           "a",
		filepath.Join(root, "sub", "f.txt"):    "f",
		filepath.Join(outside, "secret.txt"):   "s",
		filepath.Join(base, "rootsibling.txt"): "x",
	}
	for f, c := range files {
		if err := os.WriteFile(f, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return base, root, outside
}

func newGuard(t *testing.T, dir string) *PathGuard {
	t.Helper()
	g, err := NewPathGuard(dir)
	if err != nil {
		t.Fatalf("NewPathGuard(%q): %v", dir, err)
	}
	return g
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func wantResolve(t *testing.T, g *PathGuard, in, want string) {
	t.Helper()
	got, err := g.Resolve(in)
	if err != nil {
		t.Fatalf("Resolve(%q): unexpected error %v", in, err)
	}
	if got != want {
		t.Fatalf("Resolve(%q) = %q, want %q", in, got, want)
	}
}

func wantOutside(t *testing.T, g *PathGuard, in string) {
	t.Helper()
	got, err := g.Resolve(in)
	if err == nil {
		t.Fatalf("Resolve(%q) = %q, want error", in, got)
	}
	if !errors.Is(err, ErrOutsideStartDir) {
		t.Fatalf("Resolve(%q) error %v does not wrap ErrOutsideStartDir", in, err)
	}
}

func TestGuardRelative(t *testing.T) {
	_, root, _ := guardFixture(t)
	g := newGuard(t, root)
	if g.Root() != root {
		t.Fatalf("Root() = %q, want %q", g.Root(), root)
	}
	wantResolve(t, g, "a.txt", filepath.Join(root, "a.txt"))
	wantResolve(t, g, "sub/f.txt", filepath.Join(root, "sub", "f.txt"))
	wantResolve(t, g, "./sub/../a.txt", filepath.Join(root, "a.txt"))
	wantResolve(t, g, ".", root)
	wantResolve(t, g, "sub/..", root)
	if _, err := g.Resolve(""); err == nil {
		t.Fatal("Resolve(\"\") succeeded, want error")
	}
}

func TestGuardAbsoluteInside(t *testing.T) {
	_, root, _ := guardFixture(t)
	g := newGuard(t, root)
	wantResolve(t, g, filepath.Join(root, "a.txt"), filepath.Join(root, "a.txt"))
	wantResolve(t, g, root, root)
	wantResolve(t, g, root+string(filepath.Separator), root)
	wantResolve(t, g, filepath.Join(root, "sub", "f.txt"), filepath.Join(root, "sub", "f.txt"))
}

func TestGuardDotDotEscape(t *testing.T) {
	_, root, _ := guardFixture(t)
	g := newGuard(t, root)
	for _, p := range []string{"..", "../outside/secret.txt", "sub/../../outside/secret.txt", "../root/../outside", "../../../../../../etc/passwd", "../rootsibling.txt"} {
		wantOutside(t, g, p)
	}
	// ".." in a name is not an escape.
	wantResolve(t, g, "..foo", filepath.Join(root, "..foo"))
}

func TestGuardAbsoluteOutside(t *testing.T) {
	base, root, outside := guardFixture(t)
	g := newGuard(t, root)
	// A sibling sharing the root's name as a prefix must not count as inside.
	sibling := root + "sibling"
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(outside, "secret.txt"), "/etc/passwd", base, "/", sibling, filepath.Join(sibling, "new.txt"), root + "x.txt", filepath.Join(root, "..", "outside")} {
		wantOutside(t, g, p)
	}
}

func TestGuardSymlinkEscape(t *testing.T) {
	_, root, outside := guardFixture(t)
	symlink(t, outside, filepath.Join(root, "link-out"))
	symlink(t, filepath.Join(outside, "secret.txt"), filepath.Join(root, "file-out"))
	// A chain: chain1 -> chain2 -> outside.
	symlink(t, filepath.Join(root, "link-out"), filepath.Join(root, "chain2"))
	symlink(t, "chain2", filepath.Join(root, "chain1"))
	// Relative symlink climbing out.
	symlink(t, "../outside", filepath.Join(root, "rel-out"))
	g := newGuard(t, root)
	for _, p := range []string{"file-out", "link-out", "link-out/secret.txt", "chain1", "chain1/secret.txt", "rel-out/secret.txt", filepath.Join(root, "link-out", "secret.txt"), "file-out/x"} {
		wantOutside(t, g, p)
	}
}

func TestGuardSymlinkInside(t *testing.T) {
	_, root, _ := guardFixture(t)
	symlink(t, filepath.Join(root, "sub"), filepath.Join(root, "link-in"))
	symlink(t, "a.txt", filepath.Join(root, "file-in"))
	symlink(t, "link-in", filepath.Join(root, "chain-in"))
	g := newGuard(t, root)
	wantResolve(t, g, "link-in/f.txt", filepath.Join(root, "sub", "f.txt"))
	wantResolve(t, g, "link-in", filepath.Join(root, "sub"))
	wantResolve(t, g, "file-in", filepath.Join(root, "a.txt"))
	wantResolve(t, g, "chain-in/f.txt", filepath.Join(root, "sub", "f.txt"))
	wantResolve(t, g, "link-in/new.txt", filepath.Join(root, "sub", "new.txt"))
}

func TestGuardNewFile(t *testing.T) {
	_, root, outside := guardFixture(t)
	symlink(t, outside, filepath.Join(root, "link-out"))
	g := newGuard(t, root)
	wantResolve(t, g, "new.txt", filepath.Join(root, "new.txt"))
	wantResolve(t, g, "new/dir/f.txt", filepath.Join(root, "new", "dir", "f.txt"))
	wantResolve(t, g, filepath.Join(root, "sub", "new-file"), filepath.Join(root, "sub", "new-file"))
	wantResolve(t, g, "a.txt/below-a-file", filepath.Join(root, "a.txt", "below-a-file"))
	wantOutside(t, g, "link-out/new.txt")
	wantOutside(t, g, "link-out/deeper/new.txt")
	wantOutside(t, g, "new/../../outside/new.txt")
	// Resolve never creates anything.
	if _, err := os.Lstat(filepath.Join(root, "new")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Resolve created files: %v", err)
	}
}

func TestGuardDanglingSymlink(t *testing.T) {
	base, root, _ := guardFixture(t)
	symlink(t, filepath.Join(base, "nowhere", "x"), filepath.Join(root, "dangling"))
	symlink(t, "missing-in-root", filepath.Join(root, "dangling-in"))
	g := newGuard(t, root)
	for _, p := range []string{"dangling", "dangling/new.txt", "dangling-in", "dangling-in/new.txt"} {
		if r, err := g.Resolve(p); err == nil {
			t.Fatalf("Resolve(%q) = %q, want error for dangling symlink", p, r)
		}
	}
	if _, err := os.Lstat(filepath.Join(base, "nowhere")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Resolve created the symlink target: %v", err)
	}
}

func TestGuardRootSymlinked(t *testing.T) {
	base, root, outside := guardFixture(t)
	rootlink := filepath.Join(base, "rootlink")
	symlink(t, root, rootlink)
	g := newGuard(t, rootlink)
	if g.Root() != root {
		t.Fatalf("Root() = %q, want symlink-resolved %q", g.Root(), root)
	}
	wantResolve(t, g, "a.txt", filepath.Join(root, "a.txt"))
	wantResolve(t, g, filepath.Join(rootlink, "a.txt"), filepath.Join(root, "a.txt"))
	wantResolve(t, g, filepath.Join(rootlink, "new.txt"), filepath.Join(root, "new.txt"))
	wantResolve(t, g, rootlink, root)
	wantOutside(t, g, filepath.Join(rootlink, "..", "outside", "secret.txt"))
	wantOutside(t, g, filepath.Join(outside, "secret.txt"))

	// Constructor errors: missing dir, a file, empty.
	for _, d := range []string{filepath.Join(base, "missing"), filepath.Join(root, "a.txt"), ""} {
		if _, err := NewPathGuard(d); err == nil {
			t.Fatalf("NewPathGuard(%q) succeeded, want error", d)
		}
	}
}
