//go:build ignore

// Supervisor probe for T305 (glob and grep). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"yipyip/internal/tools"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

func write(root, rel, content string) {
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		setup("%v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		setup("%v", err)
	}
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return out
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
	root, err := os.MkdirTemp("", "ggprobe")
	if err != nil {
		setup("%v", err)
	}
	write(root, "a.go", "package a\n")
	write(root, "x/b.go", "package x\nfunc B() {}\n")
	write(root, "x/y/c.go", "package y\n")
	write(root, "x/y/c.txt", "func in text\n")
	write(root, ".git/config.go", "func B() {}\n")
	write(root, "bin.dat", "func B() {}\x00\x01\x02")
	reg, err := tools.NewRegistry(tools.Options{StartDir: root})
	if err != nil {
		setup("NewRegistry: %v", err)
	}
	ctx := context.Background()
	run := func(name, args string) tools.Result { return reg.Run(ctx, name, json.RawMessage(args)) }

	switch os.Args[1] {
	case "globstar":
		res := run("glob", `{"pattern":"**/*.go"}`)
		if got := lines(res.Output); res.IsError || !reflect.DeepEqual(got, []string{"a.go", "x/b.go", "x/y/c.go"}) {
			fail("glob **/*.go = %q (IsError %v)", got, res.IsError)
		}
		cases := []struct {
			pat, path string
			want      bool
		}{{"**/*.go", "x/y/c.go", true}, {"**/*.go", "a.go", true}, {"x/*.go", "x/y/c.go", false},
			{"x/**/c.go", "x/c.go", true}, {"x/**/c.go", "x/y/c.go", true}, {"*.go", "x/b.go", false}, {"x/?.go", "x/b.go", true}}
		for _, c := range cases {
			if tools.MatchGlob(c.pat, c.path) != c.want {
				fail("MatchGlob(%q, %q) != %v", c.pat, c.path, c.want)
			}
		}
		pass("glob with **")
	case "format":
		res := run("grep", `{"pattern":"func B"}`)
		if got := lines(res.Output); res.IsError || !reflect.DeepEqual(got, []string{"x/b.go:2:func B() {}"}) {
			fail("grep 'func B' = %q (want only x/b.go:2:func B() {}; .git and binary files skipped)", got)
		}
		res = run("grep", `{"pattern":"^func","glob":"*.txt"}`)
		if got := lines(res.Output); !reflect.DeepEqual(got, []string{"x/y/c.txt:1:func in text"}) {
			fail("grep with glob filter = %q", got)
		}
		pass("grep path:line:text")
	case "escape":
		for _, c := range [][2]string{{"grep", `{"pattern":"x","path":"../"}`}, {"glob", `{"pattern":"*","path":"/etc"}`}} {
			if res := run(c[0], c[1]); !res.IsError {
				accepted("%s %s ran outside the start dir: %q", c[0], c[1], res.Output)
			}
		}
		rejected("search paths outside the start dir refused")
	case "badregex":
		if res := run("grep", `{"pattern":"("}`); !res.IsError {
			accepted("invalid regex accepted: %+v", res)
		}
		rejected("invalid regex refused")
	default:
		setup("unknown case %q", os.Args[1])
	}
}
