package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

// systemOf returns the content of the system message of req (Messages[0]).
func systemOf(t *testing.T, req providertest.Request) string {
	t.Helper()
	if len(req.Messages) == 0 || req.Messages[0].Role != "system" {
		t.Fatalf("Messages[0] is not a system message: %+v", req.Messages)
	}
	return req.Messages[0].Content
}

func TestGlobalAgentsTUI(t *testing.T) {
	e := newEnvServer(t, providertest.New(resp{Text: "ok"}), envOpts{
		contextLimit:  8000,
		globalAgents:  "GLOBAL-E2E-NOTE\n",
		projectAgents: "PROJECT-E2E-NOTE\n",
	})
	e.submit(t, "hello")
	reqs := e.Server.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request reached the provider")
	}
	sys := systemOf(t, reqs[0])
	g, gn := strings.Index(sys, "# Global AGENTS.md"), strings.Index(sys, "GLOBAL-E2E-NOTE")
	p, pn := strings.Index(sys, "# AGENTS.md"), strings.Index(sys, "PROJECT-E2E-NOTE")
	if g < 0 || gn < g || p < 0 || pn < p {
		t.Fatalf("sections missing or misplaced (g=%d gn=%d p=%d pn=%d) in %q", g, gn, p, pn, sys)
	}
	if !(g < gn && gn < p && p < pn) {
		t.Errorf("global section must come before project section (g=%d gn=%d p=%d pn=%d)", g, gn, p, pn)
	}
	if e.Session.Mode() != agent.ModeAuto {
		t.Errorf("mode %v, want auto", e.Session.Mode())
	}
	if v := e.Model.View(); strings.Contains(strings.ToLower(v), "approve") {
		t.Errorf("approval prompt in view:\n%s", v)
	}
}

func TestGlobalAgentsHeadless(t *testing.T) {
	srv := providertest.New(resp{Text: "ok"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	if err := os.WriteFile(filepath.Join(home, ".config", "yipyip", "AGENTS.md"), []byte("GLOBAL-HEADLESS-NOTE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i, args := range [][]string{{"-p", "hello"}, {"-p", "hello", "--mode", "manual"}} {
		res := runYip(t, home, dir, "", headlessEnv, args...)
		if res.code != 0 {
			t.Fatalf("run %d: code %d stdout %q stderr %q", i, res.code, res.stdout, res.stderr)
		}
		reqs := srv.Requests()
		if len(reqs) != i+1 {
			t.Fatalf("run %d: %d requests", i, len(reqs))
		}
		if sys := systemOf(t, reqs[i]); !strings.Contains(sys, "# Global AGENTS.md") || !strings.Contains(sys, "GLOBAL-HEADLESS-NOTE") {
			t.Errorf("run %d: system message lacks global AGENTS.md: %q", i, sys)
		}
		checkNoSecret(t, res)
	}
}

func TestGlobalAgentsSubagent(t *testing.T) {
	srv := providertest.NewFunc(func(n int, req providertest.Request) providertest.Response {
		if firstUser(req) == "sub task" {
			return resp{Text: "sub done"}
		}
		last := req.Messages[len(req.Messages)-1]
		if last.Role == "tool" && strings.HasPrefix(last.Content, "subagent ") && strings.HasSuffix(last.Content, " started") {
			id := strings.TrimSuffix(strings.TrimPrefix(last.Content, "subagent "), " started")
			return resp{ToolCalls: []providertest.ToolCall{call("w1", "wait_agent", `{"id":"`+id+`"}`)}}
		}
		if last.Role == "user" {
			return resp{ToolCalls: []providertest.ToolCall{call("s1", "spawn_agent", `{"task":"sub task"}`)}}
		}
		return resp{Text: "main done"}
	})
	e := newEnvServer(t, srv, envOpts{contextLimit: 8000, globalAgents: "GLOBAL-SUB-E2E\n"})
	e.submit(t, "go")
	found := false
	for _, r := range e.Server.Requests() {
		if firstUser(r) != "sub task" {
			continue
		}
		found = true
		if sys := systemOf(t, r); !strings.Contains(sys, "GLOBAL-SUB-E2E") {
			t.Errorf("subagent system message lacks global AGENTS.md: %q", sys)
		}
	}
	if !found {
		t.Fatalf("no subagent request with user message %q among %d requests", "sub task", len(e.Server.Requests()))
	}
}

func TestGlobalAgentsAbsentE2E(t *testing.T) {
	e := newEnvServer(t, providertest.New(resp{Text: "ok"}), envOpts{contextLimit: 8000})
	e.submit(t, "hello")
	reqs := e.Server.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request")
	}
	if sys := systemOf(t, reqs[0]); strings.Contains(sys, "# Global AGENTS.md") {
		t.Errorf("unexpected global section: %q", sys)
	}
}
