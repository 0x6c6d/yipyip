//go:build ignore

// Supervisor probe for T404 (subagent manager). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync/atomic"
	"time"

	"yipyip/internal/agent"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

func status(m *agent.Manager, id string, want agent.SubStatus) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := m.Get(id); ok && info.Status == want {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
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
	ctx := context.Background()
	release := make(chan struct{})
	var changes atomic.Int64
	run := func(ctx context.Context, a *agent.Subagent, task, model string) (string, error) {
		a.Log("started " + task)
		a.AddTokens(7)
		if task == "fail" {
			return "", errors.New("boom")
		}
		select {
		case <-release:
			return "final:" + task, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	m := agent.NewManager(4, run, func(agent.SubInfo) { changes.Add(1) })

	switch os.Args[1] {
	case "spawn":
		start := time.Now()
		id, err := m.Spawn(ctx, "t1", "")
		if err != nil || id != "a1" || time.Since(start) > 200*time.Millisecond {
			fail("Spawn = %q, %v after %v (must return at once)", id, err, time.Since(start))
		}
		if info, ok := m.Get(id); !ok || info.Status != agent.SubRunning || info.Task != "t1" {
			fail("Get(a1) = %+v", info)
		}
		close(release)
		out, err := m.Wait(ctx, id)
		if err != nil || out != "final:t1" {
			fail("Wait = %q, %v", out, err)
		}
		info, _ := m.Get(id)
		if info.Status != agent.SubDone || info.Final != "final:t1" || info.Tokens != 7 {
			fail("after Wait: %+v", info)
		}
		if tr, ok := m.Transcript(id); !ok || !slices.Contains(tr, "started t1") {
			fail("Transcript = %q", tr)
		}
		id2, _ := m.Spawn(ctx, "t2", "")
		if id2 != "a2" || len(m.List()) != 2 || m.List()[0].ID != "a1" || changes.Load() < 2 {
			fail("second spawn id=%q list=%+v changes=%d", id2, m.List(), changes.Load())
		}
		fid, _ := m.Spawn(ctx, "fail", "")
		if !status(m, fid, agent.SubFailed) {
			fail("failing run must end as failed")
		}
		if info, _ := m.Get(fid); info.Err == "" {
			fail("failed subagent without Err")
		}
		pass("spawn, wait, transcript, tokens, failed")
	case "limit":
		for i := 0; i < 4; i++ {
			if _, err := m.Spawn(ctx, fmt.Sprintf("t%d", i), ""); err != nil {
				fail("spawn %d failed: %v", i+1, err)
			}
		}
		_, err := m.Spawn(ctx, "t5", "")
		running := m.Running()
		close(release)
		if err == nil || running != 4 {
			accepted("5th concurrent spawn with limit 4: err=%v running=%d", err, running)
		}
		rejected("%v", err)
	case "cancel":
		id, _ := m.Spawn(ctx, "t1", "")
		if err := m.Cancel(id); err != nil {
			fail("Cancel: %v", err)
		}
		wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if _, err := m.Wait(wctx, id); err == nil || !status(m, id, agent.SubCancelled) || m.Running() != 0 {
			fail("cancelled subagent: wait err=%v", err)
		}
		pid, _ := m.Spawn(ctx, "t2", "")
		pctx, pcancel := context.WithCancel(ctx)
		cid, _ := m.Spawn(pctx, "t3", "")
		pcancel()
		if !status(m, cid, agent.SubCancelled) || !status(m, pid, agent.SubRunning) {
			fail("parent context cancel must stop only its subagents")
		}
		m.CancelAll()
		if !status(m, pid, agent.SubCancelled) {
			fail("CancelAll")
		}
		pass("cancel, parent cancel, CancelAll")
	case "unknown":
		wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if _, err := m.Wait(wctx, "a99"); err == nil || errors.Is(err, context.DeadlineExceeded) {
			accepted("Wait on an unknown id: %v", err)
		}
		if err := m.Cancel("a99"); err == nil {
			accepted("Cancel on an unknown id succeeded")
		}
		rejected("unknown ids refused")
	default:
		setup("unknown case %q", os.Args[1])
	}
}
