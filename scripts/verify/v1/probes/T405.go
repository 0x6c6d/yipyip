//go:build ignore

// Supervisor probe for T405 (subagent tools in the session). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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

type R = providertest.Response
type TC = providertest.ToolCall
type U = providertest.Usage

func session(srv *providertest.Server, project string) *agent.Session {
	base, err := os.MkdirTemp("", "subprobe")
	if err != nil {
		setup("%v", err)
	}
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	os.MkdirAll(filepath.Join(home, ".config", "yipyip"), 0o755)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(home, ".config", "yipyip", "config.json"), []byte(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000)), 0o644)
	if project != "" {
		os.WriteFile(filepath.Join(dir, "yipyip.json"), []byte(project), 0o644)
	}
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
	return s
}

// task returns the subagent task marker of a request, or "" for a main-agent request.
func task(req providertest.Request) string {
	for _, m := range req.Messages {
		if m.Role == "user" && strings.Contains(m.Content, "TASK-SUB") {
			i := strings.Index(m.Content, "TASK-SUB")
			return strings.Fields(m.Content[i:])[0]
		}
	}
	return ""
}

func toolMsgs(req providertest.Request) int {
	n := 0
	for _, m := range req.Messages {
		if m.Role == "tool" {
			n++
		}
	}
	return n
}

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

// approveAll answers every approval request with Allow and records "agentID:tool".
func approveAll(s *agent.Session) (func() []string, func()) {
	var mu sync.Mutex
	var seen []string
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			ev, err := s.NextEvent(ctx)
			if err != nil {
				return
			}
			if ev.Kind == agent.EvApproval && ev.Approval != nil {
				mu.Lock()
				seen = append(seen, ev.Approval.AgentID+":"+ev.Approval.Tool)
				mu.Unlock()
				ev.Approval.Respond(agent.Allow)
			}
		}
	}()
	return func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(seen) }, cancel
}

// mainFlow: spawn TASK-SUB, then wait_agent a1, then answer.
func mainFlow(req providertest.Request) R {
	switch toolMsgs(req) {
	case 0:
		return R{ToolCalls: []TC{{ID: "s1", Name: "spawn_agent", Arguments: `{"task":"TASK-SUB do it"}`}}}
	case 1:
		return R{ToolCalls: []TC{{ID: "w1", Name: "wait_agent", Arguments: `{"id":"a1"}`}}}
	}
	return R{Text: "main done", Usage: &U{Prompt: 50, Completion: 5}}
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
	switch os.Args[1] {
	case "flow", "nospawn":
		srv := providertest.NewFunc(func(n int, req providertest.Request) R {
			if task(req) != "" {
				return R{Text: "sub finished", Usage: &U{Prompt: 10, Completion: 5}}
			}
			return mainFlow(req)
		})
		defer srv.Close()
		s := session(srv, "")
		s.SetMode(agent.ModeAuto)
		if err := s.Submit("MAIN-REQ"); err != nil || !wait(s, 20*time.Second) {
			setup("turn did not finish: %v", err)
		}
		reqs := srv.Requests()
		if os.Args[1] == "nospawn" {
			for _, r := range reqs {
				if task(r) != "" {
					if slices.Contains(r.ToolNames, "spawn_agent") || !slices.Contains(r.ToolNames, "read_file") {
						accepted("subagent tool list: %v", r.ToolNames)
					}
					rejected("subagent tools without spawn_agent: %v", r.ToolNames)
				}
			}
			accepted("no subagent request was made")
		}
		subs := s.Subagents()
		if len(subs) != 1 || subs[0].ID != "a1" || subs[0].Status != agent.SubDone || subs[0].Tokens != 15 || subs[0].Final != "sub finished" {
			fail("Subagents() = %+v", subs)
		}
		if !slices.Contains(reqs[0].ToolNames, "spawn_agent") || !slices.Contains(reqs[0].ToolNames, "wait_agent") || !slices.Contains(reqs[0].ToolNames, "cancel_agent") {
			fail("main tool list: %v", reqs[0].ToolNames)
		}
		waited := false
		for _, m := range reqs[len(reqs)-1].Messages {
			if m.Role == "tool" && m.ToolCallID == "w1" && strings.Contains(m.Content, "sub finished") {
				waited = true
			}
		}
		if !waited {
			fail("wait_agent result does not carry the subagent's final text")
		}
		if tk := s.Tokens(); tk.Total < 70 {
			fail("subagent tokens not added to the session total: %+v", tk)
		}
		if tr, ok := s.SubagentTranscript("a1"); !ok || len(tr) == 0 {
			fail("SubagentTranscript(a1) empty")
		}
		pass("spawn -> wait -> final text, tokens added")
	case "limit":
		srv := providertest.NewFunc(func(n int, req providertest.Request) R {
			if task(req) != "" {
				return R{Text: "slow", Delay: 2 * time.Second}
			}
			if toolMsgs(req) == 0 {
				return R{ToolCalls: []TC{
					{ID: "s1", Name: "spawn_agent", Arguments: `{"task":"TASK-SUB-1"}`},
					{ID: "s2", Name: "spawn_agent", Arguments: `{"task":"TASK-SUB-2"}`}}}
			}
			return R{Text: "main done"}
		})
		defer srv.Close()
		s := session(srv, `{"max_subagents": 1}`)
		s.SetMode(agent.ModeAuto)
		if err := s.Submit("MAIN-REQ"); err != nil || !wait(s, 20*time.Second) {
			setup("turn did not finish: %v", err)
		}
		spawnErr := false
		for _, ev := range s.PollEvents() {
			if ev.Kind == agent.EvToolResult && ev.Tool == "spawn_agent" && ev.IsError {
				spawnErr = true
			}
		}
		if n := len(s.Subagents()); n != 1 || !spawnErr {
			accepted("spawn beyond max_subagents: subagents=%d error result=%v", n, spawnErr)
		}
		rejected("second concurrent spawn refused")
	case "label":
		srv := providertest.NewFunc(func(n int, req providertest.Request) R {
			if task(req) != "" {
				if toolMsgs(req) == 0 {
					return R{ToolCalls: []TC{{ID: "b1", Name: "bash", Arguments: `{"command":"echo sub"}`}}}
				}
				return R{Text: "sub finished"}
			}
			return mainFlow(req)
		})
		defer srv.Close()
		s := session(srv, "")
		seen, stop := approveAll(s)
		defer stop()
		if err := s.Submit("MAIN-REQ"); err != nil || !wait(s, 20*time.Second) {
			setup("turn did not finish: %v", err)
		}
		got := seen()
		if !slices.Contains(got, "main:spawn_agent") || !slices.Contains(got, "a1:bash") {
			fail("approval requests = %v, want main:spawn_agent and a1:bash", got)
		}
		pass("subagent approvals labelled with its id")
	case "esc":
		srv := providertest.NewFunc(func(n int, req providertest.Request) R {
			if task(req) != "" {
				return R{Text: "never", Delay: 30 * time.Second}
			}
			return mainFlow(req)
		})
		defer srv.Close()
		s := session(srv, "")
		s.SetMode(agent.ModeAuto)
		if err := s.Submit("MAIN-REQ"); err != nil {
			setup("Submit: %v", err)
		}
		time.Sleep(time.Second)
		s.Cancel()
		ok := wait(s, 3*time.Second)
		time.Sleep(500 * time.Millisecond)
		subs := s.Subagents()
		if !ok || len(subs) != 1 || subs[0].Status != agent.SubCancelled {
			accepted("Esc during wait_agent: turn ended=%v subagents=%+v", ok, subs)
		}
		rejected("turn and its subagent cancelled")
	default:
		setup("unknown case %q", os.Args[1])
	}
}
