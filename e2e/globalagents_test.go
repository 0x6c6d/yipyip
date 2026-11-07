package e2e

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
	"yipyip/internal/tui"
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

func globalAgentsPath(e *env) string {
	return filepath.Join(e.Home, ".config", "yipyip", "AGENTS.md")
}

// submitActive sends text to the active tab (which may not be e.Session) and
// waits until marker, the fake's reply to it, shows in the view.
func (e *env) submitActive(t *testing.T, text, marker string) {
	t.Helper()
	m, _ := e.Model.Command(text)
	e.Model = m
	deadline := time.Now().Add(10 * time.Second)
	for {
		e.Model = e.Model.Drain()
		if strings.Contains(e.Model.View(), marker) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("reply %q did not show within 10s:\n%s", marker, e.Model.View())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func globalAgentsReset(t *testing.T, cmd string) {
	t.Helper()
	e := newEnvServer(t, providertest.New(resp{Text: "reply-one"}, resp{Text: "reply-two"}, resp{Text: "reply-three"}),
		envOpts{contextLimit: 8000, globalAgents: "GLOBAL-V1\n", tuiOpts: func(o *tui.Options) {
			// The tab factory builds a fresh session from HOME, like cmd/yip does.
			o.NewSession = func() (*agent.Session, error) {
				cfg, err := config.Load(o.Home, o.StartDir, func(k string) string {
					if k == "FAKE_KEY" {
						return "SECRET123"
					}
					return ""
				})
				if err != nil {
					return nil, err
				}
				s, err := agent.NewSession(agent.Options{Config: cfg, StartDir: o.StartDir})
				if err != nil {
					return nil, err
				}
				s.SetMode(agent.ModeAuto)
				t.Cleanup(s.Cancel)
				return s, nil
			}
		}})
	e.submit(t, "first")
	reqs := e.Server.Requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests, want 1", len(reqs))
	}
	if sys := systemOf(t, reqs[0]); !strings.Contains(sys, "GLOBAL-V1") {
		t.Fatalf("first system message lacks GLOBAL-V1: %q", sys)
	}
	if err := os.WriteFile(globalAgentsPath(e), []byte("GLOBAL-V2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.submit(t, "second")
	reqs = e.Server.Requests()
	if len(reqs) != 2 {
		t.Fatalf("%d requests, want 2", len(reqs))
	}
	if sys := systemOf(t, reqs[1]); !strings.Contains(sys, "GLOBAL-V1") || strings.Contains(sys, "GLOBAL-V2") {
		t.Fatalf("file must not be watched; system message: %q", sys)
	}
	e.submit(t, cmd)
	e.submitActive(t, "third", "reply-three")
	reqs = e.Server.Requests()
	if len(reqs) != 3 {
		t.Fatalf("%d requests, want 3", len(reqs))
	}
	if sys := systemOf(t, reqs[2]); !strings.Contains(sys, "GLOBAL-V2") || strings.Contains(sys, "GLOBAL-V1") {
		t.Fatalf("after %s the system message must carry GLOBAL-V2 only: %q", cmd, sys)
	}
}

func TestGlobalAgentsNew(t *testing.T) { globalAgentsReset(t, "/new") }

func TestGlobalAgentsClear(t *testing.T) { globalAgentsReset(t, "/clear") }

type cfgEntry struct {
	size    int64
	mode    fs.FileMode
	mtime   time.Time
	content []byte
}

func snapshotDir(t *testing.T, root string) map[string]cfgEntry {
	t.Helper()
	out := map[string]cfgEntry{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		var content []byte
		if info.Mode().IsRegular() {
			if content, err = os.ReadFile(p); err != nil {
				return err
			}
		}
		out[rel] = cfgEntry{info.Size(), info.Mode(), info.ModTime(), content}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGlobalAgentsInit(t *testing.T) {
	e := newEnvServer(t, providertest.New(
		resp{ToolCalls: []providertest.ToolCall{
			call("c1", "write_file", `{"path":"AGENTS.md","content":"# AGENTS\n\nPROJECT-INIT-E2E\n"}`),
		}},
		resp{Text: "AGENTS.md written"}),
		envOpts{contextLimit: 8000, globalAgents: "GLOBAL-INIT-E2E\n", projectAgents: "PROJECT-INIT-E2E\n"})
	cfgDir := filepath.Join(e.Home, ".config", "yipyip")
	before := snapshotDir(t, cfgDir)
	e.submit(t, "/init")
	reqs := e.Server.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request")
	}
	if sys := systemOf(t, reqs[0]); !strings.Contains(sys, "GLOBAL-INIT-E2E") {
		t.Errorf("system message lacks global AGENTS.md: %q", sys)
	}
	user := reqs[0].Messages[len(reqs[0].Messages)-1]
	if user.Role != "user" {
		t.Fatalf("last message is %q, want user", user.Role)
	}
	if !strings.Contains(user.Content, "PROJECT-INIT-E2E") {
		t.Errorf("init user message lacks the project AGENTS.md: %q", user.Content)
	}
	if strings.Contains(user.Content, "GLOBAL-INIT-E2E") {
		t.Errorf("init user message embeds the global AGENTS.md: %q", user.Content)
	}
	after := snapshotDir(t, cfgDir)
	if len(after) != len(before) {
		t.Fatalf("config dir entries changed: before %d, after %d", len(before), len(after))
	}
	for k, b := range before {
		a, ok := after[k]
		if !ok || a.size != b.size || a.mode != b.mode || !a.mtime.Equal(b.mtime) || !bytes.Equal(a.content, b.content) {
			t.Errorf("config entry %q changed by /init: %+v -> %+v", k, b, a)
		}
	}
}
