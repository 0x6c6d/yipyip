package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

// newLimitSession builds an auto-mode session talking to srv with the given
// MaxModelCalls (temp HOME and start dir).
func newLimitSession(t *testing.T, srv *providertest.Server, maxCalls int) *Session {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	for _, d := range []string{filepath.Join(home, ".config", "yipyip"), dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfgJSON := providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000)
	if err := os.WriteFile(config.GlobalPath(home), []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(home, dir, func(k string) string {
		if k == "FAKE_KEY" {
			return "SECRET123"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	s, err := NewSession(Options{Config: cfg, StartDir: dir, MaxModelCalls: maxCalls})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s.SetMode(ModeAuto)
	t.Cleanup(func() {
		s.Cancel()
		waitTurn(t, s, 5*time.Second)
	})
	return s
}

// endlessGlob always answers with one glob tool call.
func endlessGlob(n int) fakeResp {
	return fakeResp{ToolCalls: []fakeCall{{ID: fmt.Sprintf("g%d", n), Name: "glob", Arguments: `{"pattern":"*"}`}}}
}

func TestModelCallLimitMain(t *testing.T) {
	srv := providertest.NewFunc(func(n int, _ providertest.Request) fakeResp { return endlessGlob(n) })
	defer srv.Close()
	s := newLimitSession(t, srv, 2)
	evs := submitAndWait(t, s, "go")
	if got := len(srv.Requests()); got != 2 {
		t.Fatalf("provider saw %d requests, want 2", got)
	}
	if !findEvent(evs, EvError, "model call limit reached (2)") {
		t.Fatalf("no limit error event: %+v", evs)
	}
	if got := s.ModelCalls(); got != 2 {
		t.Fatalf("ModelCalls() = %d, want 2", got)
	}
	checkNoDangling(t, "history", s.History())
	if err := s.reserveModelCall(); !errors.Is(err, ErrModelCallLimit) {
		t.Fatalf("reserveModelCall after the limit = %v, want ErrModelCallLimit", err)
	}
	// Reset does not restore the budget.
	s.Reset()
	if got := s.ModelCalls(); got != 2 {
		t.Fatalf("ModelCalls() after Reset = %d, want 2", got)
	}
	evs = submitAndWait(t, s, "again")
	if got := len(srv.Requests()); got != 2 {
		t.Fatalf("provider saw %d requests after Reset, want 2", got)
	}
	if !findEvent(evs, EvError, "model call limit reached") {
		t.Fatalf("no limit error after Reset: %+v", evs)
	}
}

func TestModelCallLimitSubagents(t *testing.T) {
	// The main agent spawns a1, then waits for it. The subagent's first
	// request is held until the main agent's second request arrived, so the
	// order is main, main, sub and every counted request completes (no
	// request is cut by the turn's end).
	mainWaiting := make(chan struct{})
	var once sync.Once
	srv := providertest.NewFunc(func(n int, req providertest.Request) fakeResp {
		if isSub(req) {
			select {
			case <-mainWaiting:
			case <-time.After(5 * time.Second):
			}
			return endlessGlob(n)
		}
		if countTools(req) == 0 {
			return fakeResp{ToolCalls: []fakeCall{{ID: "s1", Name: "spawn_agent", Arguments: `{"task":"x"}`}}}
		}
		once.Do(func() { close(mainWaiting) })
		return fakeResp{ToolCalls: []fakeCall{{ID: fmt.Sprintf("w%d", n), Name: "wait_agent", Arguments: `{"id":"a1"}`}}}
	})
	defer srv.Close()
	s := newLimitSession(t, srv, 3)
	evs := submitAndWait(t, s, mainPrompt)
	if got := len(srv.Requests()); got != 3 {
		t.Fatalf("provider saw %d requests, want 3", got)
	}
	if !findEvent(evs, EvError, "model call limit reached") {
		t.Fatalf("no limit error event: %+v", evs)
	}
	if got := s.ModelCalls(); got != 3 {
		t.Fatalf("ModelCalls() = %d, want 3", got)
	}
	checkNoDangling(t, "history", s.History())
}

func TestModelCallLimitWithin(t *testing.T) {
	srv := providertest.NewFunc(func(int, providertest.Request) fakeResp { return fakeResp{Text: "done"} })
	defer srv.Close()
	s := newLimitSession(t, srv, 5)
	evs := submitAndWait(t, s, "go")
	if countKind(evs, EvError) != 0 {
		t.Fatalf("unexpected error: %+v", evs)
	}
	if got := len(srv.Requests()); got != 1 {
		t.Fatalf("provider saw %d requests, want 1", got)
	}
	if got := s.ModelCalls(); got != 1 {
		t.Fatalf("ModelCalls() = %d, want 1", got)
	}
}

func TestModelCallsUnlimited(t *testing.T) {
	srv := providertest.NewFunc(func(n int, _ providertest.Request) fakeResp {
		if n < 3 {
			return endlessGlob(n)
		}
		return fakeResp{Text: "done"}
	})
	defer srv.Close()
	s := newLimitSession(t, srv, 0)
	evs := submitAndWait(t, s, "go")
	if countKind(evs, EvError) != 0 {
		t.Fatalf("unexpected error: %+v", evs)
	}
	if got := len(srv.Requests()); got != 4 {
		t.Fatalf("provider saw %d requests, want 4", got)
	}
	if got := s.ModelCalls(); got != 4 {
		t.Fatalf("ModelCalls() = %d, want 4", got)
	}
}
