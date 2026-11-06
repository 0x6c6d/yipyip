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
	"unsafe"
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

func TestReaperSubreaper(t *testing.T) {
	InitSubreaper()
	InitSubreaper() // idempotent
	var v int32
	_, _, e := syscall.RawSyscall(syscall.SYS_PRCTL, 37 /* PR_GET_CHILD_SUBREAPER */, uintptr(unsafe.Pointer(&v)), 0)
	if e != 0 || v != 1 {
		t.Fatalf("child subreaper = %d (errno %v), want 1", v, e)
	}
}

func TestReaperSetsid(t *testing.T) {
	InitSubreaper()
	pid := reaperRun(t, "setsid sleep 3021 >/dev/null 2>&1 &")
	KillAll(2 * time.Second)
	if !waitGone(pid, 200*time.Millisecond) {
		t.Fatal("setsid sleep survived KillAll")
	}
}

// reaperSleeps returns the live pids whose command line is "sleep <arg>".
func reaperSleeps(arg string) []int {
	ents, _ := os.ReadDir("/proc")
	var out []int
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err == nil && string(b) == "sleep\x00"+arg+"\x00" && !reaperGone(pid) {
			out = append(out, pid)
		}
	}
	return out
}

func TestReaperDoubleFork(t *testing.T) {
	InitSubreaper()
	t.Cleanup(func() {
		for _, p := range reaperSleeps("3022") {
			_ = syscall.Kill(p, syscall.SIGKILL)
		}
	})
	r := &BashRunner{Dir: t.TempDir()}
	res := r.Run(context.Background(), "( setsid sh -c 'sleep 3022 >/dev/null 2>&1 &' & ) >/dev/null 2>&1", 10*time.Second)
	if res.ExitCode != 0 {
		t.Fatalf("exit %d: %q", res.ExitCode, res.Output)
	}
	var pids []int
	deadline := time.Now().Add(2 * time.Second)
	for len(pids) == 0 && time.Now().Before(deadline) {
		pids = reaperSleeps("3022")
		time.Sleep(10 * time.Millisecond)
	}
	if len(pids) == 0 {
		t.Fatal("double-forked sleep not running")
	}
	KillAll(2 * time.Second)
	for _, p := range pids {
		if !waitGone(p, 200*time.Millisecond) {
			t.Fatalf("double-forked sleep %d survived KillAll", p)
		}
	}
}

func TestReaperZombie(t *testing.T) {
	InitSubreaper()
	by := exec.Command("sleep", "3024")
	by.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := by.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = by.Process.Kill()
		_ = by.Wait()
	})
	pid := reaperRun(t, "setsid sleep 3023 >/dev/null 2>&1 &")
	// Wait until the sleep is re-parented to the test process.
	deadline := time.Now().Add(2 * time.Second)
	for {
		b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			t.Fatal(err)
		}
		_, ppid, _, _, _ := readProcStat(string(b))
		if ppid == os.Getpid() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sleep %d not re-parented (ppid %d)", pid, ppid)
		}
		time.Sleep(10 * time.Millisecond)
	}
	start := time.Now()
	KillAll(2 * time.Second)
	if el := time.Since(start); el >= time.Second {
		t.Fatalf("KillAll took %v: the zombie stalled the grace poll", el)
	}
	if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); err == nil {
		t.Fatalf("zombie child %d not reaped", pid)
	}
	if reaperGone(by.Process.Pid) {
		t.Fatal("same-session bystander was killed")
	}
}
