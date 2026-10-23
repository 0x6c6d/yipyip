//go:build ignore

// Supervisor probe for v8 T501 (web_fetch through agent.Session: offered,
// approval-gated in manual mode, the agent.Options.FetchClient seam).
// Workers never edit it. Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED,
// 125 = setup error or crash.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

type outcome struct {
	hits      int64
	approvals []agent.Event
	results   []agent.Event
	toolNames []string
}

// turn runs one main turn whose model calls web_fetch on a local page once.
func turn(mode agent.Mode, useSeam bool, decide agent.Decision) outcome {
	var hits atomic.Int64
	pg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "the magic word is plover")
	}))
	defer pg.Close()
	target := pg.URL + "/doc"
	prov := providertest.NewFunc(func(n int, req providertest.Request) providertest.Response {
		if n == 0 {
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "c1", Name: "web_fetch", Arguments: `{"url":"` + target + `"}`}}}
		}
		return providertest.Response{Text: "done"}
	})
	defer prov.Close()
	base, err := os.MkdirTemp("", "agentfetch-probe")
	if err != nil {
		setup("tempdir: %v", err)
	}
	base, _ = filepath.EvalSymlinks(base)
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	for _, d := range []string{filepath.Join(home, ".config", "yipyip"), dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			setup("mkdir: %v", err)
		}
	}
	if err := os.WriteFile(config.GlobalPath(home), []byte(providertest.ConfigJSON(prov.URL, "{env:FAKE_KEY}", 8000)), 0o644); err != nil {
		setup("write config: %v", err)
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
	opts := agent.Options{Config: cfg, StartDir: dir}
	if useSeam {
		opts.FetchClient = provider.NewFetchHTTPClient(provider.FetchOptions{AllowAddr: pg.Listener.Addr().String(), Timeout: 5 * time.Second})
	}
	sess, err := agent.NewSession(opts)
	if err != nil {
		setup("NewSession: %v", err)
	}
	sess.SetMode(mode)
	if err := sess.Submit("read the page"); err != nil {
		setup("Submit: %v", err)
	}
	var out outcome
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for done := false; !done; {
		ev, err := sess.NextEvent(ctx)
		if err != nil {
			setup("turn did not end: %v", err)
		}
		switch ev.Kind {
		case agent.EvApproval:
			out.approvals = append(out.approvals, ev)
			if ev.Approval != nil {
				ev.Approval.Respond(decide)
			}
		case agent.EvToolResult:
			if ev.Tool == "web_fetch" {
				out.results = append(out.results, ev)
			}
		case agent.EvTurnDone:
			done = ev.AgentID == agent.MainAgentID || ev.AgentID == ""
		}
	}
	sess.Wait()
	if reqs := prov.Requests(); len(reqs) > 0 {
		out.toolNames = reqs[0].ToolNames
	}
	out.hits = hits.Load()
	return out
}

func offered(o outcome) bool {
	for _, n := range o.toolNames {
		if n == "web_fetch" {
			return true
		}
	}
	return false
}

func main() {
	defer func() {
		if p := recover(); p != nil {
			fmt.Println("PANIC:", p)
			os.Exit(125)
		}
	}()
	if len(os.Args) != 2 {
		setup("usage: probe <case>")
	}
	switch c := os.Args[1]; c {
	case "auto": // auto mode: no prompt, one GET, result header
		o := turn(agent.ModeAuto, true, agent.Deny)
		if !offered(o) || len(o.approvals) != 0 || o.hits != 1 || len(o.results) != 1 ||
			o.results[0].IsError || !strings.HasPrefix(o.results[0].Text, "url: ") || !strings.Contains(o.results[0].Text, "plover") {
			fail("auto: offered %v, approvals %d, hits %d, results %+v", offered(o), len(o.approvals), o.hits, o.results)
		}
		pass("auto")
	case "manual-allow": // manual mode: the prompt carries the URL; allow -> one GET
		o := turn(agent.ModeManual, true, agent.Allow)
		if len(o.approvals) != 1 || o.approvals[0].Tool != "web_fetch" || !strings.Contains(o.approvals[0].Args, "/doc") || o.hits != 1 {
			fail("manual allow: approvals %+v, hits %d", o.approvals, o.hits)
		}
		pass("manual allow")
	case "manual-deny": // manual mode, denied: nothing is sent
		o := turn(agent.ModeManual, true, agent.Deny)
		if !offered(o) {
			setup("web_fetch is not offered, nothing to check")
		}
		if len(o.approvals) == 1 && o.approvals[0].Tool == "web_fetch" && o.hits == 0 &&
			len(o.results) == 1 && o.results[0].IsError && strings.Contains(o.results[0].Text, agent.DeniedMessage) {
			rejected("denied, no request")
		}
		accepted("manual deny: approvals %d, hits %d, results %+v", len(o.approvals), o.hits, o.results)
	case "default-client": // no seam: the production client refuses 127.0.0.1
		o := turn(agent.ModeAuto, false, agent.Deny)
		if !offered(o) {
			setup("web_fetch is not offered, nothing to check")
		}
		if o.hits == 0 && len(o.results) == 1 && o.results[0].Text == "web_fetch failed: address not allowed" {
			rejected("loopback refused by the default client")
		}
		accepted("default client: hits %d, results %+v", o.hits, o.results)
	default:
		setup("unknown case %q", c)
	}
}
