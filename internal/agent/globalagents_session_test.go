package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yipyip/internal/provider/providertest"
)

// newGlobalSessionSrv builds a session on srv whose global dir holds global
// (written before the session is created).
func newGlobalSessionSrv(t *testing.T, srv *providertest.Server, global string) *Session {
	t.Helper()
	base, dir := newLoopSession(t, srv)
	writeGlobal(t, base.cfg.GlobalDir, global)
	s, err := NewSession(Options{Config: base.cfg, StartDir: dir})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() {
		s.Cancel()
		waitTurn(t, s, 5*time.Second)
	})
	return s
}

func systemOf(t *testing.T, req providertest.Request) string {
	t.Helper()
	if len(req.Messages) == 0 || req.Messages[0].Role != "system" {
		t.Fatalf("first message is not a system message: %+v", req.Messages)
	}
	return req.Messages[0].Content
}

func TestGlobalAgentsReset(t *testing.T) {
	srv := providertest.New(fakeResp{Text: "ok"})
	defer srv.Close()
	s := newGlobalSessionSrv(t, srv, "GLOBAL-OLD\n")
	if !strings.Contains(s.SystemPrompt(), "GLOBAL-OLD") {
		t.Fatal("initial prompt lacks GLOBAL-OLD")
	}
	writeGlobal(t, s.cfg.GlobalDir, "GLOBAL-NEW\n")
	if p := s.SystemPrompt(); !strings.Contains(p, "GLOBAL-OLD") || strings.Contains(p, "GLOBAL-NEW") {
		t.Fatal("global file is watched; prompt changed without Reset")
	}
	s.Reset()
	p := s.SystemPrompt()
	if !strings.Contains(p, "GLOBAL-NEW") || strings.Contains(p, "GLOBAL-OLD") {
		t.Fatalf("after Reset prompt wrong:\n%s", p)
	}
	submitAndWait(t, s, "hello")
	reqs := srv.Requests()
	if len(reqs) == 0 {
		t.Fatal("no requests")
	}
	sys := systemOf(t, reqs[0])
	if !strings.Contains(sys, "GLOBAL-NEW") || strings.Contains(sys, "GLOBAL-OLD") {
		t.Fatalf("request system message wrong:\n%s", sys)
	}
}

// The main model calls spawn_agent (via spawnWaitMain); the subagent reuses the parent system prompt.
func TestGlobalAgentsSubagent(t *testing.T) {
	const task = "SUB-GLOBAL-TASK"
	srv := providertest.NewFunc(func(n int, req providertest.Request) fakeResp {
		if firstUser(req) == task {
			return fakeResp{Text: "sub done"}
		}
		return spawnWaitMain(task, req)
	})
	defer srv.Close()
	s := newGlobalSessionSrv(t, srv, "GLOBAL-SUB-NOTE\n")
	s.SetMode(ModeAuto)
	submitAndWait(t, s, mainPrompt)
	found := false
	for _, req := range srv.Requests() {
		if firstUser(req) != task {
			continue
		}
		found = true
		if !strings.Contains(systemOf(t, req), "GLOBAL-SUB-NOTE") {
			t.Fatalf("subagent system message lacks the global note:\n%s", systemOf(t, req))
		}
	}
	if !found {
		t.Fatal("no subagent request seen")
	}
}

func TestGlobalAgentsInitPrompt(t *testing.T) {
	srv := providertest.New(fakeResp{Text: "ok"})
	defer srv.Close()
	s := newGlobalSessionSrv(t, srv, "GLOBAL-INIT-NOTE\n")
	if err := os.WriteFile(filepath.Join(s.StartDir(), AgentsFile), []byte("PROJECT-INIT-NOTE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gfile := filepath.Join(s.cfg.GlobalDir, AgentsFile)
	before, err := os.ReadFile(gfile)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	waitTurn(t, s, 15*time.Second)
	reqs := srv.Requests()
	if len(reqs) == 0 {
		t.Fatal("no requests")
	}
	user := firstUser(reqs[0])
	if !strings.Contains(user, "PROJECT-INIT-NOTE") || strings.Contains(user, "GLOBAL-INIT-NOTE") {
		t.Fatalf("/init user prompt wrong:\n%s", user)
	}
	if !strings.Contains(systemOf(t, reqs[0]), "GLOBAL-INIT-NOTE") {
		t.Fatal("/init system message lacks the global note")
	}
	after, err := os.ReadFile(gfile)
	if err != nil || string(after) != string(before) {
		t.Fatalf("global file changed: %q err=%v", after, err)
	}
	ents, err := os.ReadDir(s.cfg.GlobalDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.Name() != AgentsFile && e.Name() != "config.json" {
			t.Errorf("unexpected file in global dir: %s", e.Name())
		}
	}
}
