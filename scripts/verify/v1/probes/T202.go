//go:build ignore

// Supervisor probe for T202 ({env:NAME}, providers/models, secrets). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"yipyip/internal/config"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

var env = map[string]string{"YIP_PROBE_KEY": "SECRET123", "YIP_PROBE_EMPTY": ""}

func getenv(k string) string { return env[k] }

// dirs writes the global config (home) and the optional project file (start dir).
func dirs(global, project string) (string, string) {
	base, err := os.MkdirTemp("", "cfgprobe")
	if err != nil {
		setup("%v", err)
	}
	home, start := filepath.Join(base, "home"), filepath.Join(base, "start")
	if err := os.MkdirAll(filepath.Join(home, ".config", "yipyip"), 0o755); err != nil {
		setup("%v", err)
	}
	if err := os.MkdirAll(start, 0o755); err != nil {
		setup("%v", err)
	}
	if global != "" {
		if err := os.WriteFile(filepath.Join(home, ".config", "yipyip", "config.json"), []byte(global), 0o644); err != nil {
			setup("%v", err)
		}
	}
	if project != "" {
		if err := os.WriteFile(filepath.Join(start, "yipyip.json"), []byte(project), 0o644); err != nil {
			setup("%v", err)
		}
	}
	return home, start
}

const global = `{"max_subagents": 3, "provider": {"fake": {"npm": "@ai-sdk/openai-compatible", "name": "Fake",
 "options": {"baseURL": "http://127.0.0.1:9/v1/", "apiKey": "{env:YIP_PROBE_KEY}"},
 "models": {"m1": {"name": "M1", "cost": {"input": 1, "output": 2}, "limit": {"context": 8000}},
            "m2": {"reasoning": false}}}}}`

const project = `{"default_model": "fake/m2", "provider": {"fake": {"models": {"m3": {"name": "M3"}}}}}`

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
	switch os.Args[1] {
	case "load":
		home, start := dirs(global, project)
		c, err := config.Load(home, start, getenv)
		if err != nil {
			fail("Load: %v", err)
		}
		if ids := c.ModelIDs(); !reflect.DeepEqual(ids, []string{"fake/m1", "fake/m2", "fake/m3"}) {
			fail("ModelIDs = %v (project override must merge models key by key)", ids)
		}
		if d := c.DefaultModelID(); d != "fake/m2" {
			fail("DefaultModelID = %q", d)
		}
		if c.MaxSubagents != 3 || !c.AutoCompact {
			fail("MaxSubagents=%d AutoCompact=%v", c.MaxSubagents, c.AutoCompact)
		}
		p, m1, ok := c.Lookup("fake/m1")
		if !ok || p.APIKey != "SECRET123" || p.BaseURL != "http://127.0.0.1:9/v1" || p.Name != "Fake" {
			fail("Lookup fake/m1: ok=%v baseURL=%q name=%q apiKey resolved=%v", ok, p.BaseURL, p.Name, p.APIKey == "SECRET123")
		}
		if m1.ID != "fake/m1" || m1.Key != "m1" || m1.Provider != "fake" || m1.Name != "M1" || !m1.Reasoning || m1.ContextLimit != 8000 {
			fail("model m1 = %+v", m1)
		}
		if _, m2, _ := c.Lookup("fake/m2"); m2.Reasoning {
			fail("m2 reasoning:false not parsed")
		}
		if _, _, ok := c.Lookup("fake/none"); ok {
			fail("Lookup of unknown id succeeded")
		}
		if b := c.BaseURLs(); !reflect.DeepEqual(b, []string{"http://127.0.0.1:9/v1"}) {
			fail("BaseURLs = %v", b)
		}
		if c.Redactor == nil || c.Redactor.Redact("k=SECRET123") != "k=[redacted]" {
			fail("resolved secret not registered with the Redactor")
		}
		if !slices.Contains(c.EnvNames, "YIP_PROBE_KEY") {
			fail("EnvNames = %v", c.EnvNames)
		}
		home2, start2 := dirs("", "")
		c2, err := config.Load(home2, start2, getenv)
		if err != nil || c2.MaxSubagents != 4 || !c2.AutoCompact || c2.DefaultModelID() != "" || c2.Redactor == nil {
			fail("missing files must give defaults: err=%v", err)
		}
		pass("load, override, env substitution, models, cost ignored")
	case "missing":
		home, start := dirs(`{"provider": {"p": {"name": "{env:YIP_PROBE_KEY}", "options": {"baseURL": "http://127.0.0.1:9", "apiKey": "{env:YIP_PROBE_UNSET}"}}}}`, "")
		_, err := config.Load(home, start, getenv)
		if err == nil {
			accepted("Load succeeded with an unset variable")
		}
		var me *config.MissingEnvError
		if !errors.As(err, &me) || me.Name != "YIP_PROBE_UNSET" {
			accepted("error is not a *MissingEnvError naming YIP_PROBE_UNSET: %v", err)
		}
		if !strings.Contains(err.Error(), "YIP_PROBE_UNSET") || strings.Contains(err.Error(), "SECRET123") {
			accepted("error text must name the variable and contain no value: %q", err.Error())
		}
		rejected("%v", err)
	case "empty":
		home, start := dirs(`{"provider": {"p": {"options": {"baseURL": "http://127.0.0.1:9", "apiKey": "x-{env:YIP_PROBE_EMPTY}"}}}}`, "")
		_, err := config.Load(home, start, getenv)
		var me *config.MissingEnvError
		if err == nil || !errors.As(err, &me) || me.Name != "YIP_PROBE_EMPTY" {
			accepted("empty variable not reported: %v", err)
		}
		rejected("%v", err)
	case "malformed":
		home, start := dirs(global, `{"default_model": `)
		_, err := config.Load(home, start, getenv)
		if err == nil {
			accepted("invalid JSON accepted")
		}
		if !strings.Contains(err.Error(), "yipyip.json") {
			accepted("error does not name the file: %v", err)
		}
		rejected("%v", err)
	default:
		setup("unknown case %q", os.Args[1])
	}
}
