//go:build ignore

// Supervisor probe for T101 (internal/crashlog). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"yipyip/internal/crashlog"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

func tmp() string {
	d, err := os.MkdirTemp("", "clprobe")
	if err != nil {
		setup("%v", err)
	}
	return d
}

func opts(dir string) crashlog.Options {
	return crashlog.Options{
		Version:   "v-probe",
		Resolve:   func(rel string) (string, error) { return filepath.Join(dir, rel), nil },
		SessionID: func() string { return "abc123def456" },
	}
}

//go:noinline
func probePanics() { panic("probe panic") }

func recovered(l *crashlog.Logger) {
	defer func() {
		if r := recover(); r != nil {
			l.Recovered("probe where", r)
		}
	}()
	probePanics()
}

func read(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, ".yipyip", "crash.log"))
	if err != nil {
		setup("read crash.log: %v", err)
	}
	return string(b)
}

func main() {
	if len(os.Args) < 2 {
		setup("usage: probe <format|perms|cap|leak|escape>")
	}
	switch os.Args[1] {
	case "format":
		dir := tmp()
		l := crashlog.New(opts(dir))
		recovered(l)
		if _, err := l.Fatal("boom fatal"); err != nil {
			fail("Fatal: %v", err)
		}
		got := read(dir)
		for _, want := range []string{"=== yip crash ", " recovered ===", " fatal ===", "version: v-probe", "where: probe where",
			"session: abc123def456", "panic: probe panic", "panic: boom fatal", "stack:", "probePanics", "=== end ==="} {
			if !strings.Contains(got, want) {
				fail("record lacks %q:\n%s", want, got)
			}
		}
		if p, err := l.Path(); err != nil || p != filepath.Join(dir, ".yipyip", "crash.log") {
			fail("Path() = %q, %v", p, err)
		}
		pass("format")
	case "perms":
		dir := tmp()
		l := crashlog.New(opts(dir))
		recovered(l)
		di, err := os.Stat(filepath.Join(dir, ".yipyip"))
		if err != nil {
			fail("%v", err)
		}
		fi, err := os.Stat(filepath.Join(dir, ".yipyip", "crash.log"))
		if err != nil {
			fail("%v", err)
		}
		if di.Mode().Perm() != 0o700 || fi.Mode().Perm() != 0o600 {
			fail("dir %v file %v, want 0700 / 0600", di.Mode().Perm(), fi.Mode().Perm())
		}
		pass("perms")
	case "cap":
		dir := tmp()
		l := crashlog.New(opts(dir))
		big := strings.Repeat("x", 60<<10)
		cut := false
		last := int64(0)
		for i := 0; i < 40; i++ {
			l.Recovered("cap", fmt.Sprintf("seq-%02d %s", i, big))
			fi, err := os.Stat(filepath.Join(dir, ".yipyip", "crash.log"))
			if err != nil {
				fail("%v", err)
			}
			if fi.Size() > crashlog.MaxBytes {
				fail("size %d above MaxBytes after record %d", fi.Size(), i)
			}
			if fi.Size() < last {
				cut = true
				if fi.Size() > crashlog.KeepBytes {
					fail("cut to %d, want at most %d", fi.Size(), crashlog.KeepBytes)
				}
			}
			last = fi.Size()
		}
		got := read(dir)
		if !cut {
			fail("the log was never cut")
		}
		if !strings.Contains(got, "seq-39 ") || strings.Contains(got, "seq-00 ") {
			fail("newest record missing or oldest kept")
		}
		if !strings.HasPrefix(got, "=== yip crash ") {
			fail("log does not start at a record boundary")
		}
		pass("cap")
	case "leak":
		dir := tmp()
		o := opts(dir)
		o.Redact = func(s string) string { return strings.ReplaceAll(s, "SECRET123", "[redacted]") }
		l := crashlog.New(o)
		func() {
			defer func() {
				if r := recover(); r != nil {
					l.Recovered("leak", r)
					l.Fatal(r)
				}
			}()
			panic("token SECRET123 in the panic value")
		}()
		if strings.Contains(read(dir), "SECRET123") {
			accepted("the secret reached crash.log")
		}
		rejected("secret redacted from crash.log")
	case "escape":
		dir := tmp()
		o := opts(dir)
		o.Resolve = func(string) (string, error) { return "", errors.New("outside the start directory") }
		l := crashlog.New(o)
		if _, err := l.Fatal("x"); err == nil {
			fail("Fatal reported no error for a failing Resolve")
		}
		l.Recovered("escape", "y")
		var found []string
		filepath.Walk(dir, func(p string, i os.FileInfo, err error) error {
			if err == nil && !i.IsDir() {
				found = append(found, p)
			}
			return nil
		})
		if len(found) > 0 {
			accepted("a file was written despite the failing Resolve: %v", found)
		}
		rejected("nothing written when Resolve fails")
	default:
		setup("unknown case %q", os.Args[1])
	}
}
