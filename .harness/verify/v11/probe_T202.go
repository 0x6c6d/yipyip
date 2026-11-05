//go:build ignore

// Supervisor probe for T202 (workers never edit it). Cases:
//
//	within  exit 0 when a 1-call turn with MaxModelCalls 5 ends without error
//	limit   "REJECTED:" + exit 1 when, with MaxModelCalls 2 and endless tool calls,
//	        exactly 2 requests reach the provider; exit 0 when more got through
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

func fail(format string, a ...any) {
	fmt.Printf(format+"\n", a...)
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		fail("usage: probe within|limit")
	}
	endless := os.Args[1] == "limit"
	srv := providertest.NewFunc(func(n int, _ providertest.Request) providertest.Response {
		if !endless {
			return providertest.Response{Text: "done"}
		}
		return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: fmt.Sprintf("c%d", n), Name: "glob", Arguments: `{"pattern":"*"}`}}}
	})
	defer srv.Close()
	base, err := os.MkdirTemp("", "probe-t202")
	if err != nil {
		fail("tempdir: %v", err)
	}
	defer os.RemoveAll(base)
	base, _ = filepath.EvalSymlinks(base)
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "work")
	if err := os.MkdirAll(filepath.Join(home, ".config", "yipyip"), 0o755); err != nil {
		fail("mkdir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "yipyip", "config.json"), []byte(providertest.ConfigJSON(srv.URL, "", 100000)), 0o644); err != nil {
		fail("config: %v", err)
	}
	cfg, err := config.Load(home, dir, func(string) string { return "" })
	if err != nil {
		fail("config.Load: %v", err)
	}
	limit := 5
	if endless {
		limit = 2
	}
	sess, err := agent.NewSession(agent.Options{Config: cfg, StartDir: dir, MaxModelCalls: limit})
	if err != nil {
		fail("NewSession: %v", err)
	}
	sess.SetMode(agent.ModeAuto)
	if err := sess.Submit("go"); err != nil {
		fail("Submit: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	limitErr, anyErr := false, false
	for {
		ev, err := sess.NextEvent(ctx)
		if err != nil {
			sess.Cancel()
			break
		}
		if ev.Kind == agent.EvError {
			anyErr = true
			if strings.Contains(ev.Text, "model call limit reached") {
				limitErr = true
			}
		}
		if ev.Kind == agent.EvApproval && ev.Approval != nil {
			ev.Approval.Respond(agent.Deny)
		}
		if ev.Kind == agent.EvTurnDone {
			break
		}
	}
	sess.Wait()
	reqs := len(srv.Requests())
	if !endless {
		if anyErr || reqs != 1 || sess.ModelCalls() != 1 {
			fmt.Printf("within: err=%v requests=%d ModelCalls=%d\n", anyErr, reqs, sess.ModelCalls())
			os.Exit(1)
		}
		fmt.Println("within: ok")
		return
	}
	if reqs > 2 {
		fmt.Printf("ACCEPTED: %d requests reached the provider with a limit of 2\n", reqs)
		os.Exit(0)
	}
	if reqs == 2 && limitErr && sess.ModelCalls() == 2 {
		fmt.Println("REJECTED: the third request was refused (model call limit reached)")
		os.Exit(1)
	}
	fail("limit: unexpected state requests=%d limitErr=%v ModelCalls=%d", reqs, limitErr, sess.ModelCalls())
}
