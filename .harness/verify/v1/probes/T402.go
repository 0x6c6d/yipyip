//go:build ignore

// Supervisor probe for T402 (agent session core). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"yipyip/internal/agent"
	"yipyip/internal/config"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

const cfgJSON = `{"default_model": "fake/m1", "provider": {"fake": {"options": {"baseURL": "http://127.0.0.1:9/v1", "apiKey": "{env:FAKE_KEY}"},
 "models": {"m1": {"limit": {"context": 8000}}, "m2": {"reasoning": false}}}}}`

func session() (*agent.Session, string) {
	base, err := os.MkdirTemp("", "sessprobe")
	if err != nil {
		setup("%v", err)
	}
	base, _ = filepath.EvalSymlinks(base)
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	os.MkdirAll(filepath.Join(home, ".config", "yipyip"), 0o755)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(home, ".config", "yipyip", "config.json"), []byte(cfgJSON), 0o644)
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("PROBE-AGENTS-NOTE\n"), 0o644)
	cfg, err := config.Load(home, dir, func(k string) string {
		if k == "FAKE_KEY" {
			return "SECRET123"
		}
		return ""
	})
	if err != nil {
		setup("config.Load: %v", err)
	}
	s, err := agent.NewSession(agent.Options{Config: cfg, StartDir: dir})
	if err != nil {
		fail("NewSession: %v", err)
	}
	return s, dir
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
	s, dir := session()
	switch os.Args[1] {
	case "defaults":
		if s.ModelID() != "fake/m1" || s.Effort() != "medium" || s.Mode() != agent.ModeManual || s.Tokens() != (agent.Tokens{}) {
			fail("defaults: model=%q effort=%q mode=%q tokens=%+v", s.ModelID(), s.Effort(), s.Mode(), s.Tokens())
		}
		if !reflect.DeepEqual(s.Models(), []string{"fake/m1", "fake/m2"}) || !s.ReasoningEnabled() || s.StartDir() != dir {
			fail("models=%v reasoning=%v startDir=%q", s.Models(), s.ReasoningEnabled(), s.StartDir())
		}
		h := s.History()
		if len(h) != 1 || h[0].Role != "system" || h[0].Content != s.SystemPrompt() || !strings.Contains(s.SystemPrompt(), "PROBE-AGENTS-NOTE") {
			fail("history must be [system prompt incl. AGENTS.md]: %+v", h)
		}
		if used, limit := s.ContextSize(); used != 0 || limit != 8000 {
			fail("ContextSize = %d/%d", used, limit)
		}
		if err := s.SetModel("fake/m2"); err != nil || s.ModelID() != "fake/m2" || s.ReasoningEnabled() {
			fail("SetModel(fake/m2): err=%v reasoning=%v", err, s.ReasoningEnabled())
		}
		if s.ToggleMode() != agent.ModeAuto || s.Mode() != agent.ModeAuto {
			fail("ToggleMode did not switch to auto")
		}
		s.SetMode(agent.ModeManual)
		if err := s.SetEffort("high"); err != nil || s.Effort() != "high" {
			fail("SetEffort(high): %v", err)
		}
		if strings.Contains(s.Redact("k=SECRET123"), "SECRET123") {
			fail("session Redact does not mask the configured key")
		}
		pass("session defaults and setters")
	case "badmodel":
		err := s.SetModel("fake/nope")
		if err == nil || !errors.Is(err, agent.ErrUnknownModel) || s.ModelID() != "fake/m1" {
			accepted("unknown model: err=%v model=%q", err, s.ModelID())
		}
		rejected("%v", err)
	case "badeffort":
		err := s.SetEffort("extreme")
		if err == nil || !errors.Is(err, agent.ErrInvalidEffort) || s.Effort() != "medium" {
			accepted("invalid effort: err=%v effort=%q", err, s.Effort())
		}
		rejected("%v", err)
	case "reset":
		s.SetMode(agent.ModeAuto)
		s.Reset()
		if len(s.History()) != 1 || s.Tokens() != (agent.Tokens{}) || s.ModelID() != "fake/m1" {
			fail("Reset: history=%d tokens=%+v model=%q", len(s.History()), s.Tokens(), s.ModelID())
		}
		if evs := s.PollEvents(); len(s.PollEvents()) != 0 {
			fail("PollEvents must drain the queue (got %d then more)", len(evs))
		}
		pass("reset")
	default:
		setup("unknown case %q", os.Args[1])
	}
}
