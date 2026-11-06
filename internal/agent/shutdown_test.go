package agent

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"yipyip/internal/provider/providertest"
	"yipyip/internal/tools"
)

// shutdownSleepPIDs returns the pids whose cmdline is exactly `sleep <arg>`.
func shutdownSleepPIDs(arg string) []int {
	want := "sleep\x00" + arg + "\x00"
	paths, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	var pids []int
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil || string(b) != want {
			continue
		}
		pid, err := strconv.Atoi(filepath.Base(filepath.Dir(p)))
		if err == nil && !shutdownGone(pid) {
			pids = append(pids, pid)
		}
	}
	return pids
}

// shutdownGone reports whether pid has no /proc entry or is a zombie.
func shutdownGone(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return true
	}
	return s[i+2] == 'Z'
}

// shutdownCleanup SIGKILLs every live `sleep <arg>` when the test ends.
func shutdownCleanup(t *testing.T, args ...string) {
	t.Cleanup(func() {
		for _, a := range args {
			for _, pid := range shutdownSleepPIDs(a) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
}

// shutdownAlive waits until exactly one `sleep <arg>` is alive and returns it.
func shutdownAlive(t *testing.T, arg string) int {
	t.Helper()
	var pid int
	waitCond(t, 10*time.Second, "sleep "+arg+" alive", func() bool {
		if p := shutdownSleepPIDs(arg); len(p) == 1 {
			pid = p[0]
			return true
		}
		return false
	})
	return pid
}

func shutdownWaitGone(t *testing.T, pid int, d time.Duration, what string) {
	t.Helper()
	waitCond(t, d, what+" gone", func() bool { return shutdownGone(pid) })
}

func TestShutdownReapsMainAndSubagent(t *testing.T) {
	shutdownCleanup(t, "3031", "3032")
	srv := providertest.NewFunc(func(n int, req providertest.Request) fakeResp {
		if isSub(req) {
			if countTools(req) == 0 {
				return fakeResp{ToolCalls: []fakeCall{{ID: "sb1", Name: "bash", Arguments: `{"command":"sleep 3032 >/dev/null 2>&1 &"}`}}}
			}
			return fakeResp{Delay: 60 * time.Second, Text: "sub late"}
		}
		switch countTools(req) {
		case 0:
			return fakeResp{ToolCalls: []fakeCall{
				{ID: "b1", Name: "bash", Arguments: `{"command":"sleep 3031 >/dev/null 2>&1 &"}`},
				{ID: "s1", Name: "spawn_agent", Arguments: `{"task":"SUB-SLEEP"}`},
			}}
		case 2:
			return fakeResp{ToolCalls: []fakeCall{{ID: "w1", Name: "wait_agent", Arguments: `{"id":"a1"}`}}}
		}
		return fakeResp{Text: "main done"}
	})
	defer srv.Close()
	s := newSubSession(t, srv, "")
	s.SetMode(ModeAuto)
	if err := s.Submit(mainPrompt); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	mainPID := shutdownAlive(t, "3031")
	subPID := shutdownAlive(t, "3032")

	s.Shutdown()
	tools.KillAll(2 * time.Second)
	shutdownWaitGone(t, mainPID, 2*time.Second, "main sleep 3031")
	shutdownWaitGone(t, subPID, 2*time.Second, "subagent sleep 3032")
	waitTurn(t, s, 3*time.Second)
}

func TestShutdownForegroundCommand(t *testing.T) {
	shutdownCleanup(t, "3033")
	srv := providertest.NewFunc(func(n int, req providertest.Request) fakeResp {
		if countTools(req) == 0 {
			return fakeResp{ToolCalls: []fakeCall{{ID: "b1", Name: "bash", Arguments: `{"command":"sleep 3033"}`}}}
		}
		return fakeResp{Text: "done"}
	})
	defer srv.Close()
	s := newSubSession(t, srv, "")
	s.SetMode(ModeAuto)
	if err := s.Submit(mainPrompt); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	pid := shutdownAlive(t, "3033")
	s.Shutdown()
	shutdownWaitGone(t, pid, 2*time.Second, "foreground sleep 3033")
	waitTurn(t, s, 3*time.Second)
}

func TestShutdownIdempotent(t *testing.T) {
	srv := providertest.NewFunc(func(n int, req providertest.Request) fakeResp {
		return fakeResp{Delay: 60 * time.Second, Text: "late"}
	})
	defer srv.Close()
	s := newSubSession(t, srv, "")
	s.SetMode(ModeAuto)

	start := time.Now()
	s.Shutdown()
	s.Shutdown()
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("idle Shutdown took %v", d)
	}
	if s.Busy() {
		t.Fatal("idle session became busy")
	}

	if err := s.Submit(mainPrompt); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Shutdown()
			s.Shutdown()
		}()
	}
	wg.Wait()
	waitTurn(t, s, 3*time.Second)
	s.Shutdown()
}
