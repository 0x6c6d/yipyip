package e2e

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

// runYip runs the binary like runCLI, with stdin as its standard input
// (never a tty).
func runYip(t *testing.T, home, dir, stdin string, extra []string, args ...string) cliResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cliBinary(t), args...)
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + home}, extra...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := cliResult{stdout: stdout.String(), stderr: stderr.String()}
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("running yip %v: %v (stderr: %q)", args, err, res.stderr)
		}
		res.code = ee.ExitCode()
	}
	return res
}

// headlessDirs returns a temp HOME (with a global config using srv and the
// key {env:FAKE_KEY}) and a working directory.
func headlessDirs(t *testing.T, srv *providertest.Server) (home, dir string) {
	t.Helper()
	home, dir = cliDirs(t)
	writeCLIGlobalConfig(t, home, providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 100000))
	return home, dir
}

var headlessEnv = []string{"FAKE_KEY=SECRET123"}

func checkNoSecret(t *testing.T, res cliResult) {
	t.Helper()
	if strings.Contains(res.stdout, "SECRET123") || strings.Contains(res.stderr, "SECRET123") {
		t.Errorf("secret leaked: stdout %q stderr %q", res.stdout, res.stderr)
	}
}

func TestHeadlessPrompt(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "hello world"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "hi")
	if res.code != 0 || res.stdout != "hello world\n" {
		t.Fatalf("yip -p hi: code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	checkNoSecret(t, res)
}

func TestHeadlessStdin(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "hello world"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "from stdin\n", headlessEnv, "-p", "-")
	if res.code != 0 || res.stdout != "hello world\n" {
		t.Fatalf("yip -p -: code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	reqs := srv.Requests()
	if len(reqs) == 0 {
		t.Fatal("no provider request")
	}
	msgs := reqs[len(reqs)-1].Messages
	last := ""
	for _, m := range msgs {
		if m.Role == "user" {
			last = m.Content
		}
	}
	if last != "from stdin" {
		t.Errorf("last user message %q, want %q", last, "from stdin")
	}
	checkNoSecret(t, res)
}

func TestHeadlessUsage(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	home, dir := headlessDirs(t, srv)

	if res := runYip(t, home, dir, "", headlessEnv, "--json"); res.code != 2 {
		t.Errorf("yip --json: code %d stderr %q, want 2", res.code, res.stderr)
	}
	if res := runYip(t, home, dir, "", headlessEnv, "-p", "-"); res.code != 2 || res.stdout != "" {
		t.Errorf("yip -p - (empty stdin): code %d stdout %q stderr %q, want 2", res.code, res.stdout, res.stderr)
	}
	res := runYip(t, home, dir, "", headlessEnv, "-p", "/model")
	lines := strings.Split(strings.TrimRight(res.stderr, "\n"), "\n")
	if res.code != 2 || len(lines) != 1 || lines[0] == "" || res.stdout != "" {
		t.Errorf("yip -p /model: code %d stdout %q stderr %q, want 2 and one stderr line", res.code, res.stdout, res.stderr)
	}
	checkNoSecret(t, res)
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("usage errors reached the provider %d times", n)
	}
}

func TestHeadlessToolProgress(t *testing.T) {
	srv := providertest.New(
		providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "c1", Name: "glob", Arguments: `{"pattern":"*"}`}}},
		providertest.Response{Text: "final answer"},
	)
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "list files")
	if res.code != 0 || res.stdout != "final answer\n" {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "tool glob") {
		t.Errorf("stderr %q lacks %q", res.stderr, "tool glob")
	}
	checkNoSecret(t, res)
}
