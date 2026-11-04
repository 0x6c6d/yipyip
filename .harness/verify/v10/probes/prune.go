//go:build ignore

// Supervisor probe for T301 (session.Store.Prune). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"yipyip/internal/session"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

func id(i int) string { return fmt.Sprintf("%012x", i+1) }

// seed writes n session files; file i has updated = base + i seconds (higher i = newer).
func seed(dir string, n int) {
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		ts := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339)
		body := fmt.Sprintf(`{"version":1,"id":%q,"created":%q,"updated":%q}`, id(i), ts, ts)
		if err := os.WriteFile(filepath.Join(dir, id(i)+".json"), []byte(body), 0o600); err != nil {
			setup("%v", err)
		}
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func newStart() (start, sdir string) {
	d, err := os.MkdirTemp("", "pruneprobe")
	if err != nil {
		setup("%v", err)
	}
	start, err = filepath.EvalSymlinks(d)
	if err != nil {
		setup("%v", err)
	}
	sdir = filepath.Join(start, ".yipyip", "sessions")
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		setup("%v", err)
	}
	return start, sdir
}

func main() {
	if len(os.Args) < 2 {
		setup("usage: probe <keep|zero|escape>")
	}
	start, sdir := newStart()
	switch os.Args[1] {
	case "keep":
		seed(sdir, 30)
		for _, stray := range []string{"notes.txt", "abc.json"} {
			if err := os.WriteFile(filepath.Join(sdir, stray), []byte("x"), 0o600); err != nil {
				setup("%v", err)
			}
		}
		st, err := session.NewStore(start)
		if err != nil {
			setup("%v", err)
		}
		if err := st.Prune(25, id(0), id(1)); err != nil {
			fail("Prune: %v", err)
		}
		for i := 0; i < 30; i++ {
			want := i >= 5 || i < 2 // the 25 newest (5..29) plus the 2 protected (0, 1)
			if got := exists(filepath.Join(sdir, id(i)+".json")); got != want {
				fail("file %d exists=%v, want %v", i, got, want)
			}
		}
		for _, stray := range []string{"notes.txt", "abc.json"} {
			if !exists(filepath.Join(sdir, stray)) {
				fail("stray file %s was deleted", stray)
			}
		}
		pass("25 newest + 2 protected kept, stray files untouched")
	case "zero":
		seed(sdir, 30)
		st, err := session.NewStore(start)
		if err != nil {
			setup("%v", err)
		}
		err = st.Prune(0)
		for i := 0; i < 30; i++ {
			if !exists(filepath.Join(sdir, id(i)+".json")) {
				accepted("keep=0 deleted file %d", i)
			}
		}
		if err == nil {
			accepted("keep=0 returned no error")
		}
		rejected("keep=0 refused, nothing deleted: %v", err)
	case "escape":
		outside, err := os.MkdirTemp("", "pruneout")
		if err != nil {
			setup("%v", err)
		}
		seed(outside, 30)
		if err := os.Remove(sdir); err != nil {
			setup("%v", err)
		}
		if err := os.Symlink(outside, sdir); err != nil {
			setup("%v", err)
		}
		st, err := session.NewStore(start)
		if err != nil {
			setup("%v", err)
		}
		err = st.Prune(25)
		for i := 0; i < 30; i++ {
			if !exists(filepath.Join(outside, id(i)+".json")) {
				accepted("Prune deleted outside the start directory: file %d", i)
			}
		}
		if err == nil {
			accepted("Prune followed a symlink out of the start directory without error")
		}
		rejected("symlink escape refused, outside files intact: %v", err)
	default:
		setup("unknown case %q", os.Args[1])
	}
}
