//go:build ignore

// Supervisor probe for T303 (tool registry, bash tool, redacted results). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"yipyip/internal/config"
	"yipyip/internal/tools"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

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
	dir, err := os.MkdirTemp("", "regprobe")
	if err != nil {
		setup("%v", err)
	}
	os.Setenv("YIP_PROBE_STRIP", "ZZTOPVALUE")
	reg, err := tools.NewRegistry(tools.Options{StartDir: dir, Redactor: config.NewRedactor("SECRET123"), StripEnv: []string{"YIP_PROBE_STRIP"}})
	if err != nil {
		setup("NewRegistry: %v", err)
	}
	ctx := context.Background()
	run := func(name, args string) tools.Result { return reg.Run(ctx, name, json.RawMessage(args)) }

	switch os.Args[1] {
	case "names":
		var names []string
		bashOK := false
		for _, t := range reg.Tools() {
			names = append(names, t.Name)
			if t.Name == "bash" {
				var schema map[string]any
				bashOK = t.Gated && json.Unmarshal(t.Parameters, &schema) == nil && schema["type"] == "object"
			}
			l := strings.ToLower(t.Name)
			for _, bad := range []string{"web", "fetch", "search", "browse", "http", "url", "download", "curl"} {
				if strings.Contains(l, bad) {
					accepted("web-like tool registered: %q", t.Name)
				}
			}
		}
		if !bashOK || !sort.StringsAreSorted(names) || reg.Has("web_fetch") || !reg.Has("bash") {
			accepted("tool list wrong: %v (bash gated with object schema: %v)", names, bashOK)
		}
		rejected("no web tools; tools = %v", names)
	case "unknown":
		if res := run("web_fetch", `{"url":"http://example.org"}`); !res.IsError {
			accepted("unknown tool ran: %+v", res)
		}
		rejected("unknown tool refused")
	case "badargs":
		for _, a := range []string{`{not json`, `{}`, `{"command": 5}`} {
			if res := run("bash", a); !res.IsError {
				accepted("bash accepted args %s: %+v", a, res)
			}
		}
		rejected("invalid arguments refused")
	case "leak":
		res := run("bash", `{"command":"echo SECRET123"}`)
		if strings.Contains(res.Output, "SECRET123") || !strings.Contains(res.Output, "[redacted]") {
			accepted("tool result not redacted: %q", res.Output)
		}
		rejected("tool result redacted")
	case "strip":
		res := run("bash", `{"command":"echo \"v=${YIP_PROBE_STRIP:-unset}\""}`)
		if !strings.Contains(res.Output, "v=unset") {
			accepted("StripEnv not applied to the bash tool: %q", res.Output)
		}
		rejected("variable stripped")
	case "exit":
		res := run("bash", `{"command":"echo hi; exit 4"}`)
		if !res.IsError || !strings.Contains(res.Output, "hi") || !strings.Contains(res.Output, "[exit code: 4]") {
			fail("exit 4 result = %+v", res)
		}
		res = run("bash", `{"command":"echo fine","timeout_seconds":5}`)
		if res.IsError || !strings.Contains(res.Output, "fine") || !strings.Contains(res.Output, "[exit code: 0]") {
			fail("exit 0 result = %+v", res)
		}
		pass("bash tool output format")
	default:
		setup("unknown case %q", os.Args[1])
	}
}
