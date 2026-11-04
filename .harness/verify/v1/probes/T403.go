//go:build ignore

// Supervisor probe for T403 (agent loop, tokens, cancellation). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

func session(cfgJSON string) (*agent.Session, string) {
	base, err := os.MkdirTemp("", "loopprobe")
	if err != nil {
		setup("%v", err)
	}
	base, _ = filepath.EvalSymlinks(base)
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	os.MkdirAll(filepath.Join(home, ".config", "yipyip"), 0o755)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(home, ".config", "yipyip", "config.json"), []byte(cfgJSON), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("PROBE-FILE-CONTENT\n"), 0o644)
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
		setup("NewSession: %v", err)
	}
	return s, dir
}

// wait waits for the running turn; false when it did not end within d.
func wait(s *agent.Session, d time.Duration) bool {
	done := make(chan struct{})
	go func() { s.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
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
	type R = providertest.Response
	type TC = providertest.ToolCall
	type U = providertest.Usage
	switch os.Args[1] {
	case "answer":
		srv := providertest.New(
			R{ToolCalls: []TC{{ID: "c1", Name: "read_file", Arguments: `{"path":"notes.txt"}`}}, Usage: &U{Prompt: 100, Completion: 20}},
			R{Text: "all done", Usage: &U{Prompt: 150, Completion: 10}})
		defer srv.Close()
		s, _ := session(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000))
		s.SetMode(agent.ModeAuto)
		if err := s.Submit("read it"); err != nil {
			fail("Submit: %v", err)
		}
		if !wait(s, 15*time.Second) {
			fail("turn did not finish")
		}
		evs := s.PollEvents()
		text, toolResult := "", false
		for _, ev := range evs {
			if ev.Kind == agent.EvText {
				text += ev.Text
			}
			if ev.Kind == agent.EvToolResult && ev.Tool == "read_file" && strings.Contains(ev.Text, "PROBE-FILE-CONTENT") && !ev.IsError {
				toolResult = true
			}
		}
		if text != "all done" || !toolResult || len(evs) == 0 || evs[len(evs)-1].Kind != agent.EvTurnDone {
			fail("events: text=%q toolResult=%v (last event must be EvTurnDone)", text, toolResult)
		}
		reqs := srv.Requests()
		if len(reqs) != 2 {
			fail("requests = %d", len(reqs))
		}
		fed := false
		for _, m := range reqs[1].Messages {
			if m.Role == "tool" && m.ToolCallID == "c1" && strings.Contains(m.Content, "PROBE-FILE-CONTENT") {
				fed = true
			}
		}
		if !fed || reqs[0].Model != "m1" || reqs[0].ReasoningEffort != "medium" {
			fail("tool result not fed back, or model/effort wrong: model=%q effort=%q", reqs[0].Model, reqs[0].ReasoningEffort)
		}
		if tk := s.Tokens(); tk != (agent.Tokens{In: 250, Out: 30, Total: 280}) {
			fail("tokens = %+v, want 250/30/280", tk)
		}
		if used, _ := s.ContextSize(); used != 150 {
			fail("context used = %d, want last prompt_tokens 150", used)
		}
		if h := s.History(); len(h) != 5 || h[4].Role != "assistant" || h[4].Content != "all done" {
			fail("history = %+v", h)
		}
		pass("tool call -> result -> answer")
	case "nomodel":
		s, _ := session(`{"provider": {}}`)
		if err := s.Submit("hi"); err == nil || !errors.Is(err, agent.ErrNoModel) {
			accepted("Submit without a model: %v", err)
		}
		rejected("no model configured")
	case "busy":
		srv := providertest.New(R{Text: "slow", Delay: 3 * time.Second})
		defer srv.Close()
		s, _ := session(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000))
		if err := s.Submit("one"); err != nil {
			setup("Submit: %v", err)
		}
		err := s.Submit("two")
		s.Cancel()
		if err == nil || !errors.Is(err, agent.ErrBusy) {
			accepted("second Submit during a turn: %v", err)
		}
		rejected("busy")
	case "cancel":
		srv := providertest.New(R{ToolCalls: []TC{{ID: "c1", Name: "bash", Arguments: `{"command":"sleep 30"}`}}})
		defer srv.Close()
		s, _ := session(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000))
		s.SetMode(agent.ModeAuto)
		if err := s.Submit("sleep"); err != nil {
			setup("Submit: %v", err)
		}
		time.Sleep(700 * time.Millisecond)
		start := time.Now()
		s.Cancel()
		ok := wait(s, 5*time.Second)
		el := time.Since(start)
		evs := s.PollEvents()
		if !ok || el > 2*time.Second || s.Busy() || len(evs) == 0 || evs[len(evs)-1].Kind != agent.EvTurnDone {
			accepted("cancel: finished=%v after %v busy=%v", ok, el, s.Busy())
		}
		rejected("turn cancelled in %v", el)
	case "limit":
		srv := providertest.NewFunc(func(n int, req providertest.Request) R {
			if n >= 60 {
				return R{Text: "done"}
			}
			return R{ToolCalls: []TC{{ID: fmt.Sprintf("c%d", n), Name: "glob", Arguments: `{"pattern":"*.txt"}`}}}
		})
		defer srv.Close()
		s, _ := session(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000))
		if err := s.Submit("work long"); err != nil {
			setup("Submit: %v", err)
		}
		if !wait(s, 90*time.Second) {
			accepted("turn never stopped")
		}
		limitErr := false
		for _, ev := range s.PollEvents() {
			if ev.Kind == agent.EvError && strings.Contains(ev.Text, "iteration limit") {
				limitErr = true
			}
		}
		if n := len(srv.Requests()); n != 61 || agent.MaxIterations != 0 || limitErr {
			accepted("requests=%d limit error shown=%v", n, limitErr)
		}
		rejected("main agent ran past 50 iterations without a limit")
	case "error":
		srv := providertest.New(R{Status: 500, Body: "boom"}, R{Text: "recovered"})
		defer srv.Close()
		s, _ := session(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000))
		s.Submit("first")
		if !wait(s, 10*time.Second) {
			fail("turn with HTTP 500 did not finish")
		}
		shown := false
		for _, ev := range s.PollEvents() {
			if ev.Kind == agent.EvError && strings.Contains(ev.Text, "500") {
				shown = true
			}
		}
		if err := s.Submit("second"); err != nil || !wait(s, 10*time.Second) {
			fail("session unusable after an HTTP error: %v", err)
		}
		text := ""
		for _, ev := range s.PollEvents() {
			if ev.Kind == agent.EvText {
				text += ev.Text
			}
		}
		if !shown || text != "recovered" {
			fail("error shown=%v, next reply=%q", shown, text)
		}
		pass("HTTP error shown, session keeps working")
	case "effort":
		srv := providertest.New()
		defer srv.Close()
		s, _ := session(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000))
		s.Submit("a")
		wait(s, 10*time.Second)
		if err := s.SetModel("fake/m2"); err != nil {
			setup("SetModel: %v", err)
		}
		s.Submit("b")
		wait(s, 10*time.Second)
		reqs := srv.Requests()
		if len(reqs) != 2 || reqs[0].ReasoningEffort != "medium" || reqs[1].ReasoningEffort != "" || reqs[1].Model != "m2" {
			fail("effort must be sent only when the model reasons: %d requests", len(reqs))
		}
		if tk := s.Tokens(); tk.In <= 0 || tk.Out <= 0 || tk.Total != tk.In+tk.Out {
			fail("missing usage must be estimated: %+v", tk)
		}
		pass("reasoning_effort and token estimate")
	default:
		setup("unknown case %q", os.Args[1])
	}
}
