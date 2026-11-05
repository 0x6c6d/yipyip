package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"yipyip/internal/provider/providertest"
)

func TestHeadlessExit4(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "late", Delay: 30 * time.Second})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	start := time.Now()
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--timeout", "1s", "--json")
	if el := time.Since(start); el >= 10*time.Second {
		t.Errorf("took %v, want under 10s", el)
	}
	if res.code != 4 {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	r := lastResult(t, jsonLines(t, res.stdout))
	if r["reason"] != "timeout" || r["exit_code"] != float64(4) {
		t.Errorf("result %v", r)
	}
	checkNoSecret(t, res)
}

// firstUser returns the content of the first user message of req.
func firstUser(req providertest.Request) string {
	for _, m := range req.Messages {
		if m.Role == "user" {
			return m.Content
		}
	}
	return ""
}

var endlessGlob = providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "g", Name: "glob", Arguments: `{"pattern":"*"}`}}}

func TestHeadlessExit5(t *testing.T) {
	srv := providertest.NewFunc(func(n int, req providertest.Request) providertest.Response {
		if firstUser(req) == "loop" {
			return endlessGlob
		}
		last := req.Messages[len(req.Messages)-1]
		if last.Role == "tool" && strings.HasPrefix(last.Content, "subagent ") && strings.HasSuffix(last.Content, " started") {
			id := strings.TrimSuffix(strings.TrimPrefix(last.Content, "subagent "), " started")
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "w1", Name: "wait_agent", Arguments: `{"id":"` + id + `"}`}}}
		}
		if last.Role == "user" {
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "s1", Name: "spawn_agent", Arguments: `{"task":"loop"}`}}}
		}
		return providertest.Response{Text: "main done"}
	})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--max-model-calls", "2", "--json")
	if res.code != 5 {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	if n := len(srv.Requests()); n != 2 {
		t.Errorf("requests = %d, want 2", n)
	}
	r := lastResult(t, jsonLines(t, res.stdout))
	if r["reason"] != "max_calls" || r["exit_code"] != float64(5) || r["model_calls"] != float64(2) {
		t.Errorf("result %v", r)
	}
	checkNoSecret(t, res)
}

func TestHeadlessExit5Main(t *testing.T) {
	srv := providertest.NewFunc(func(n int, req providertest.Request) providertest.Response { return endlessGlob })
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--max-model-calls", "3", "--json")
	if res.code != 5 {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	if n := len(srv.Requests()); n != 3 {
		t.Errorf("requests = %d, want 3", n)
	}
	r := lastResult(t, jsonLines(t, res.stdout))
	if r["reason"] != "max_calls" || r["model_calls"] != float64(3) {
		t.Errorf("result %v", r)
	}

	// Without --json: the limit line on stderr.
	res = runYip(t, home, dir, "", headlessEnv, "-p", "go", "--max-model-calls", "1")
	if res.code != 5 || !strings.Contains(res.stderr, "model call limit reached") {
		t.Errorf("plain: code %d stderr %q", res.code, res.stderr)
	}
	checkNoSecret(t, res)
}

func TestHeadlessSigterm(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "late", Delay: 30 * time.Second})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	cmd := exec.Command(cliBinary(t), "-p", "go", "--json")
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + home}, headlessEnv...)
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.Now().Add(10 * time.Second)
	for len(srv.Requests()) == 0 {
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("the fake never got a request (stderr %q)", stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	start := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("no exit within 10s after SIGTERM")
	}
	if el := time.Since(start); el >= 10*time.Second {
		t.Errorf("took %v", el)
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("exit %v, want code 1 (stdout %q stderr %q)", err, stdout.String(), stderr.String())
	}
	r := lastResult(t, jsonLines(t, stdout.String()))
	if r["reason"] != "error" || r["exit_code"] != float64(1) {
		t.Errorf("result %v", r)
	}
	if strings.Contains(stdout.String()+stderr.String(), "SECRET123") {
		t.Errorf("secret leaked")
	}
}
