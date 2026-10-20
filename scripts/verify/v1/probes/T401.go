//go:build ignore

// Supervisor probe for T401 (approval gate). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"yipyip/internal/agent"
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
	ctx := context.Background()
	var asked atomic.Int64
	var answer atomic.Int64 // Decision to give; -1 = never answer
	var last atomic.Pointer[agent.ApprovalRequest]
	g := agent.NewGate(func(r *agent.ApprovalRequest) {
		asked.Add(1)
		last.Store(r)
		if d := answer.Load(); d >= 0 {
			go r.Respond(agent.Decision(d))
		}
	})
	check := func(mode agent.Mode, tool string) bool {
		ok, err := g.Check(ctx, mode, agent.MainAgentID, tool, "{}")
		if err != nil {
			fail("Check(%s, %s) error: %v", mode, tool, err)
		}
		return ok
	}

	switch os.Args[1] {
	case "free":
		for tool, want := range map[string]bool{"bash": true, "write_file": true, "edit_file": true, "spawn_agent": true,
			"read_file": false, "glob": false, "grep": false, "wait_agent": false, "cancel_agent": false} {
			if agent.IsGated(tool) != want {
				fail("IsGated(%q) != %v", tool, want)
			}
		}
		answer.Store(int64(agent.Deny))
		for _, tool := range []string{"read_file", "glob", "grep", "wait_agent", "cancel_agent"} {
			if !check(agent.ModeManual, tool) {
				fail("ungated %s denied in manual mode", tool)
			}
		}
		for _, tool := range []string{"bash", "write_file", "edit_file", "spawn_agent"} {
			if !check(agent.ModeAuto, tool) {
				fail("%s denied in auto mode", tool)
			}
		}
		if asked.Load() != 0 {
			fail("prompted %d times for ungated tools or in auto mode", asked.Load())
		}
		if agent.DeniedMessage != "denied by user" {
			fail("DeniedMessage = %q", agent.DeniedMessage)
		}
		pass("no prompts for reads or in auto mode")
	case "deny":
		answer.Store(int64(agent.Deny))
		if check(agent.ModeManual, "bash") || asked.Load() != 1 {
			accepted("bash ran without approval (asked %d)", asked.Load())
		}
		rejected("denied bash refused")
	case "always":
		answer.Store(int64(agent.AllowAlways))
		if !check(agent.ModeManual, "bash") || !check(agent.ModeManual, "bash") || asked.Load() != 1 {
			fail("'always' must allow bash and suppress later prompts for it (asked %d)", asked.Load())
		}
		answer.Store(int64(agent.Allow))
		if !check(agent.ModeManual, "write_file") || asked.Load() != 2 {
			fail("'always' for bash must not cover write_file (asked %d)", asked.Load())
		}
		if !check(agent.ModeManual, "write_file") || asked.Load() != 3 {
			fail("plain 'y' must not be remembered (asked %d)", asked.Load())
		}
		g.Reset()
		if !check(agent.ModeManual, "bash") || asked.Load() != 4 {
			fail("Reset must forget 'always' (asked %d)", asked.Load())
		}
		pass("always is per tool and reset by Reset")
	case "cancel":
		answer.Store(-1)
		cctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer cancel()
		start := time.Now()
		ok, err := g.Check(cctx, agent.ModeManual, agent.MainAgentID, "bash", "{}")
		if ok || err == nil || time.Since(start) > 2*time.Second {
			accepted("unanswered prompt: ok=%v err=%v after %v", ok, err, time.Since(start))
		}
		rejected("cancelled wait returns an error: %v", err)
	case "label":
		answer.Store(-1)
		done := make(chan bool)
		go func() {
			ok, _ := g.Check(ctx, agent.ModeManual, "a1", "bash", "ls -la")
			done <- ok
		}()
		deadline := time.Now().Add(2 * time.Second)
		for last.Load() == nil && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		r := last.Load()
		if r == nil || r.AgentID != "a1" || r.Tool != "bash" || r.Args != "ls -la" {
			fail("request = %+v", r)
		}
		r.Respond(agent.Allow)
		r.Respond(agent.Deny) // first answer wins, later ones are ignored
		select {
		case ok := <-done:
			if !ok {
				fail("first answer (Allow) did not win")
			}
		case <-time.After(2 * time.Second):
			fail("Check did not return after Respond")
		}
		pass("request labelled with the agent id")
	default:
		setup("unknown case %q", os.Args[1])
	}
}
