//go:build ignore

// Supervisor probe for T304 (read_file, write_file, edit_file). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

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
	base, err := os.MkdirTemp("", "ftprobe")
	must(err)
	base, err = filepath.EvalSymlinks(base)
	must(err)
	root, outside := filepath.Join(base, "root"), filepath.Join(base, "outside")
	must(os.MkdirAll(root, 0o755))
	must(os.MkdirAll(outside, 0o755))
	must(os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("TOPSECRET"), 0o644))
	must(os.Symlink(outside, filepath.Join(root, "out")))
	var hooked []string
	reg, err := tools.NewRegistry(tools.Options{StartDir: root, BeforeWrite: func(p string) { hooked = append(hooked, p) }})
	must(err)
	ctx := context.Background()
	run := func(name, args string) tools.Result { return reg.Run(ctx, name, json.RawMessage(args)) }

	switch os.Args[1] {
	case "ok":
		gated := map[string]bool{}
		for _, t := range reg.Tools() {
			gated[t.Name] = t.Gated
		}
		if g, ok := gated["read_file"]; !ok || g || !gated["write_file"] || !gated["edit_file"] {
			fail("tool list/gating wrong: %v", gated)
		}
		if res := run("write_file", `{"path":"d/new.txt","content":"one\ntwo\n"}`); res.IsError {
			fail("write_file: %+v", res)
		}
		if b, _ := os.ReadFile(filepath.Join(root, "d", "new.txt")); string(b) != "one\ntwo\n" {
			fail("written content = %q", b)
		}
		if len(hooked) != 1 || hooked[0] != filepath.Join(root, "d", "new.txt") {
			fail("BeforeWrite calls = %q", hooked)
		}
		if res := run("read_file", `{"path":"d/new.txt"}`); res.IsError || !strings.Contains(res.Output, "one\ntwo") {
			fail("read_file: %+v", res)
		}
		if res := run("edit_file", `{"path":"d/new.txt","old_string":"two","new_string":"three"}`); res.IsError {
			fail("edit_file: %+v", res)
		}
		if b, _ := os.ReadFile(filepath.Join(root, "d", "new.txt")); string(b) != "one\nthree\n" || len(hooked) != 2 {
			fail("after edit: %q, hook calls %d", b, len(hooked))
		}
		pass("read/write/edit inside the start dir")
	case "escape":
		res := run("write_file", `{"path":"../outside/evil.txt","content":"x"}`)
		if !res.IsError || exists(filepath.Join(outside, "evil.txt")) || len(hooked) != 0 {
			accepted("write via .. not refused: %+v", res)
		}
		res = run("read_file", `{"path":"../outside/secret.txt"}`)
		if !res.IsError || strings.Contains(res.Output, "TOPSECRET") {
			accepted("read via .. not refused: %+v", res)
		}
		res = run("read_file", `{"path":"`+filepath.Join(outside, "secret.txt")+`"}`)
		if !res.IsError || strings.Contains(res.Output, "TOPSECRET") {
			accepted("read of absolute outside path not refused: %+v", res)
		}
		rejected("escapes refused")
	case "symlink":
		res := run("write_file", `{"path":"out/evil.txt","content":"x"}`)
		if !res.IsError || exists(filepath.Join(outside, "evil.txt")) {
			accepted("write through symlink not refused: %+v", res)
		}
		res = run("read_file", `{"path":"out/secret.txt"}`)
		if !res.IsError || strings.Contains(res.Output, "TOPSECRET") {
			accepted("read through symlink not refused: %+v", res)
		}
		res = run("edit_file", `{"path":"out/secret.txt","old_string":"TOP","new_string":"x"}`)
		if b, _ := os.ReadFile(filepath.Join(outside, "secret.txt")); !res.IsError || string(b) != "TOPSECRET" {
			accepted("edit through symlink not refused: %+v", res)
		}
		rejected("symlink escapes refused")
	case "edit":
		p := filepath.Join(root, "x.txt")
		must(os.WriteFile(p, []byte("x x"), 0o644))
		for _, a := range []string{
			`{"path":"x.txt","old_string":"x","new_string":"y"}`,
			`{"path":"x.txt","old_string":"zzz","new_string":"y"}`,
			`{"path":"x.txt","old_string":"","new_string":"y"}`,
			`{"path":"missing.txt","old_string":"x","new_string":"y"}`,
		} {
			if res := run("edit_file", a); !res.IsError {
				accepted("edit_file accepted %s: %+v", a, res)
			}
		}
		if b, _ := os.ReadFile(p); string(b) != "x x" || len(hooked) != 0 {
			accepted("failed edits changed the file or called BeforeWrite: %q %v", b, hooked)
		}
		rejected("ambiguous, missing and empty old_string refused")
	default:
		setup("unknown case %q", os.Args[1])
	}
}
