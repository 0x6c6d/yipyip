package tools

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// reaperGone reports whether pid has no /proc entry or is a zombie.
func reaperGone(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	state, _, ok := parseStat(string(b))
	return !ok || state == "Z"
}

// reaperRun runs command via BashRunner and returns the pid printed by it
// (the command must end with `echo $!`).
func reaperRun(t *testing.T, command string) int {
	t.Helper()
	r := &BashRunner{Dir: t.TempDir()}
	start := time.Now()
	res := r.Run(context.Background(), command+"\necho $!", 10*time.Second)
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("Run took %v", el)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit %d: %q", res.ExitCode, res.Output)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(res.Output))
	if err != nil {
		t.Fatalf("no pid in output %q", res.Output)
	}
	t.Cleanup(func() {
		if !reaperGone(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	if reaperGone(pid) {
		t.Fatalf("background pid %d not alive after Run", pid)
	}
	return pid
}

func waitGone(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for !reaperGone(pid) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return true
}

func TestReaperBackground(t *testing.T) {
	pid := reaperRun(t, "sleep 3011 >/dev/null 2>&1 &")
	start := time.Now()
	KillAll(2 * time.Second)
	if el := time.Since(start); el >= 2500*time.Millisecond {
		t.Errorf("KillAll took %v", el)
	}
	if !waitGone(pid, 200*time.Millisecond) {
		t.Fatal("background sleep survived KillAll")
	}
}

func TestReaperTermIgnored(t *testing.T) {
	pid := reaperRun(t, "trap '' TERM; sleep 3012 >/dev/null 2>&1 &")
	KillAll(300 * time.Millisecond)
	if !waitGone(pid, 200*time.Millisecond) {
		t.Fatal("TERM-ignoring sleep survived KillAll")
	}
}

func TestReaperIdempotent(t *testing.T) {
	pid := reaperRun(t, "sleep 3014 >/dev/null 2>&1 &")
	KillAll(2 * time.Second)
	if !waitGone(pid, 200*time.Millisecond) {
		t.Fatal("sleep survived first KillAll")
	}
	start := time.Now()
	KillAll(2 * time.Second)
	if el := time.Since(start); el >= 100*time.Millisecond {
		t.Errorf("second KillAll took %v", el)
	}

	pid = reaperRun(t, "sleep 3015 >/dev/null 2>&1 &")
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			KillAll(2 * time.Second)
		}()
	}
	wg.Wait()
	if !waitGone(pid, 200*time.Millisecond) {
		t.Fatal("sleep survived concurrent KillAll")
	}
}

func TestReaperExitedGroup(t *testing.T) {
	KillAll(2 * time.Second) // start from an empty tracker
	r := &BashRunner{Dir: t.TempDir()}
	res := r.Run(context.Background(), "echo hi", 10*time.Second)
	if res.ExitCode != 0 {
		t.Fatalf("exit %d: %q", res.ExitCode, res.Output)
	}
	pgids := trackedPGIDs()
	if len(pgids) != 1 {
		t.Fatalf("tracked = %v, want one entry", pgids)
	}
	if live := groupsLiveProc(pgids); live[0] {
		t.Fatalf("group %d of an exited command is live", pgids[0])
	}
	start := time.Now()
	KillAll(2 * time.Second)
	if el := time.Since(start); el >= 100*time.Millisecond {
		t.Errorf("KillAll on a dead group took %v (signalled?)", el)
	}
	if got := trackedPGIDs(); len(got) != 0 {
		t.Fatalf("dead group not dropped: %v", got)
	}
}

func TestReaperBystander(t *testing.T) {
	by := exec.Command("sleep", "3013")
	by.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := by.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = by.Process.Kill()
		_ = by.Wait()
	})
	pid := reaperRun(t, "sleep 3016 >/dev/null 2>&1 &")
	KillAll(2 * time.Second)
	if !waitGone(pid, 200*time.Millisecond) {
		t.Fatal("tracked sleep survived KillAll")
	}
	if reaperGone(by.Process.Pid) {
		t.Fatal("bystander was killed")
	}
}

func TestReaperTiming(t *testing.T) {
	KillAll(2 * time.Second)
	start := time.Now()
	KillAll(2 * time.Second)
	if el := time.Since(start); el >= 100*time.Millisecond {
		t.Errorf("KillAll with nothing left took %v", el)
	}

	pid := reaperRun(t, "trap '' TERM; sleep 3017 >/dev/null 2>&1 &")
	start = time.Now()
	KillAll(2 * time.Second)
	if el := time.Since(start); el >= 2500*time.Millisecond {
		t.Errorf("KillAll took %v", el)
	}
	if !waitGone(pid, 200*time.Millisecond) {
		t.Fatal("TERM-ignoring sleep survived KillAll")
	}
}
