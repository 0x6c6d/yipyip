//go:build ignore

// Supervisor probe for T301 (PathGuard). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"yipyip/internal/tools"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

func must(err error) {
	if err != nil {
		setup("%v", err)
	}
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("PANIC:", r)
			os.Exit(125)
		}
	}()
	if len(os.Args) != 2 {
		setup("usage: probe <case>")
	}
	base, err := os.MkdirTemp("", "pgprobe")
	must(err)
	base, err = filepath.EvalSymlinks(base)
	must(err)
	root, outside := filepath.Join(base, "root"), filepath.Join(base, "outside")
	must(os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	must(os.MkdirAll(outside, 0o755))
	must(os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644))
	must(os.WriteFile(filepath.Join(root, "sub", "f.txt"), []byte("f"), 0o644))
	must(os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s"), 0o644))
	must(os.Symlink(outside, filepath.Join(root, "link-out")))
	must(os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "file-out")))
	must(os.Symlink(filepath.Join(root, "sub"), filepath.Join(root, "link-in")))
	must(os.Symlink(filepath.Join(base, "nowhere", "x"), filepath.Join(root, "dangling")))
	rootlink := filepath.Join(base, "rootlink")
	must(os.Symlink(root, rootlink))

	g, err := tools.NewPathGuard(rootlink)
	if err != nil {
		fail("NewPathGuard on a symlinked start dir: %v", err)
	}
	escapes := func(paths ...string) {
		for _, p := range paths {
			r, err := g.Resolve(p)
			if err == nil {
				accepted("Resolve(%q) = %q, want an error", p, r)
			}
			if !errors.Is(err, tools.ErrOutsideStartDir) {
				accepted("Resolve(%q) error does not wrap ErrOutsideStartDir: %v", p, err)
			}
		}
		rejected("all escapes rejected: %q", paths)
	}

	switch os.Args[1] {
	case "inside":
		if g.Root() != root {
			fail("Root() = %q, want symlink-resolved %q", g.Root(), root)
		}
		want := map[string]string{
			"a.txt":                                filepath.Join(root, "a.txt"),
			filepath.Join(root, "a.txt"):           filepath.Join(root, "a.txt"),
			filepath.Join(rootlink, "a.txt"):       filepath.Join(root, "a.txt"),
			"new/dir/f.txt":                        filepath.Join(root, "new", "dir", "f.txt"),
			"link-in/f.txt":                        filepath.Join(root, "sub", "f.txt"),
			"sub/../a.txt":                         filepath.Join(root, "a.txt"),
			".":                                    root,
			filepath.Join(root, "sub", "new-file"): filepath.Join(root, "sub", "new-file"),
		}
		for in, w := range want {
			got, err := g.Resolve(in)
			if err != nil || got != w {
				fail("Resolve(%q) = %q, %v; want %q", in, got, err, w)
			}
		}
		pass("paths inside the start dir resolve")
	case "dotdot":
		escapes("../outside/secret.txt", "sub/../../outside/secret.txt", "..")
	case "abs":
		escapes(filepath.Join(outside, "secret.txt"), "/etc/passwd", base)
	case "symlink":
		escapes("file-out", "link-out/secret.txt", "link-out")
	case "symlinknew":
		escapes("link-out/newfile.txt", "link-out/deeper/new.txt")
	case "dangling":
		if r, err := g.Resolve("dangling"); err == nil {
			accepted("dangling symlink resolved to %q", r)
		}
		rejected("dangling symlink rejected")
	case "empty":
		if r, err := g.Resolve(""); err == nil {
			accepted("empty path resolved to %q", r)
		}
		rejected("empty path rejected")
	default:
		setup("unknown case %q", os.Args[1])
	}
}
