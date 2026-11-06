package e2e

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"yipyip/internal/provider/providertest"
)

// cleanupGone reports that pid no longer exists or is a zombie.
func cleanupGone(pid int) bool {
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

// cleanupSleeps returns the live processes whose command line is exactly
// "sleep <arg>".
func cleanupSleeps(arg string) []int {
	ents, _ := os.ReadDir("/proc")
	var out []int
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil {
			continue
		}
		if string(b) == "sleep\x00"+arg+"\x00" && !cleanupGone(pid) {
			out = append(out, pid)
		}
	}
	return out
}

// killSleepsOnCleanup SIGKILLs every leftover "sleep <arg>" when t ends.
func killSleepsOnCleanup(t *testing.T, arg string) {
	t.Cleanup(func() {
		for _, p := range cleanupSleeps(arg) {
			_ = syscall.Kill(p, syscall.SIGKILL)
		}
	})
}

// sleepsGoneWithin waits up to d for every "sleep <arg>" to be gone.
func sleepsGoneWithin(arg string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if len(cleanupSleeps(arg)) == 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// bashSleepsCall is a bash tool call starting one setsid escapee and one
// group member "sleep <arg>", then writing started.txt.
func bashSleepsCall(arg string) providertest.Response {
	cmd := "setsid sleep " + arg + " >/dev/null 2>&1 & sleep " + arg + " >/dev/null 2>&1 & echo started > started.txt"
	return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "c1", Name: "bash",
		Arguments: `{"command":"` + cmd + `"}`}}}
}

func checkStarted(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "started.txt")); err != nil {
		t.Fatalf("the bash command never ran: %v", err)
	}
}

func checkSleepsGone(t *testing.T, arg string) {
	t.Helper()
	if !sleepsGoneWithin(arg, 200*time.Millisecond) {
		t.Fatalf("background sleeps survived yip: %v", cleanupSleeps(arg))
	}
}

func TestCleanupHeadless(t *testing.T) {
	killSleepsOnCleanup(t, "3041")
	srv := providertest.New(bashSleepsCall("3041"), providertest.Response{Text: "done"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go")
	if res.code != 0 {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	checkStarted(t, dir)
	checkSleepsGone(t, "3041")
}

func TestCleanupHeadlessTimeout(t *testing.T) {
	killSleepsOnCleanup(t, "3042")
	srv := providertest.New(bashSleepsCall("3042"), providertest.Response{Text: "late", Delay: 30 * time.Second})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--timeout", "2s")
	if res.code != 4 {
		t.Fatalf("code %d, want 4 (stderr %q)", res.code, res.stderr)
	}
	checkStarted(t, dir)
	checkSleepsGone(t, "3042")
}

func TestCleanupHeadlessMaxCalls(t *testing.T) {
	killSleepsOnCleanup(t, "3043")
	srv := providertest.New(bashSleepsCall("3043"), providertest.Response{Text: "done"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--max-model-calls", "1")
	if res.code != 5 {
		t.Fatalf("code %d, want 5 (stderr %q)", res.code, res.stderr)
	}
	checkStarted(t, dir)
	checkSleepsGone(t, "3043")
}

func TestCleanupHeadlessSignal(t *testing.T) {
	killSleepsOnCleanup(t, "3044")
	srv := providertest.New(bashSleepsCall("3044"), providertest.Response{Text: "late", Delay: 30 * time.Second})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	cmd := exec.Command(cliBinary(t), "-p", "go")
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + home}, headlessEnv...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-done
	})
	deadline := time.Now().Add(cliTimeout)
	for {
		if _, err := os.Stat(filepath.Join(dir, "started.txt")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("started.txt never appeared")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	var err error
	select {
	case err = <-done:
		done <- err // let the cleanup receive it again
	case <-time.After(5 * time.Second):
		t.Fatal("yip did not exit within 5 s of SIGHUP")
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("exit %v, want code 1", err)
	}
	checkSleepsGone(t, "3044")
}

func TestCleanupCrash(t *testing.T) {
	killSleepsOnCleanup(t, "3045")
	bin := crashBinary(t, "headless-end")
	srv := providertest.New(bashSleepsCall("3045"), providertest.Response{Text: "done"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runBinary(t, bin, home, dir, headlessEnv, "-p", "go")
	if res.code != 2 {
		t.Fatalf("code %d, want 2 (stderr %q)", res.code, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".yipyip", "crash.log")); err != nil {
		t.Fatalf("no crash record: %v", err)
	}
	checkStarted(t, dir)
	checkSleepsGone(t, "3045")
}

func TestCleanupOutsider(t *testing.T) {
	out := exec.Command("sleep", "3049")
	if err := out.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = out.Process.Kill()
		_ = out.Wait()
	})
	srv := providertest.New(providertest.Response{Text: "hi"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go")
	if res.code != 0 {
		t.Fatalf("code %d stderr %q", res.code, res.stderr)
	}
	if cleanupGone(out.Process.Pid) {
		t.Fatal("yip killed a process it did not start")
	}
}
