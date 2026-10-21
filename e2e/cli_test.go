package e2e

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const cliTimeout = 20 * time.Second

var (
	cliBuildOnce sync.Once
	cliBinPath   string
	cliBuildErr  error
)

// repoRoot finds the directory with go.mod above the working directory.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found above the working directory")
		}
		dir = parent
	}
}

// cliBinary builds the real yip binary once per test run and returns its path.
func cliBinary(t *testing.T) string {
	t.Helper()
	cliBuildOnce.Do(func() {
		root, err := repoRoot()
		if err != nil {
			cliBuildErr = err
			return
		}
		tmp, err := os.MkdirTemp("", "yip-cli-bin")
		if err != nil {
			cliBuildErr = err
			return
		}
		cliBinPath = filepath.Join(tmp, "yip")
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-ldflags", "-X main.version=test", "-o", cliBinPath, "./cmd/yip")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			cliBuildErr = errors.New("go build: " + err.Error() + "\n" + string(out))
		}
	})
	if cliBuildErr != nil {
		t.Fatal(cliBuildErr)
	}
	return cliBinPath
}

// cliResult is the outcome of one binary run.
type cliResult struct {
	stdout, stderr string
	code           int
}

// runCLI runs the binary in dir with a clean environment (PATH, HOME and
// extra only), without a TTY.
func runCLI(t *testing.T, home, dir string, extra []string, args ...string) cliResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cliBinary(t), args...)
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + home}, extra...)
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

// cliDirs returns a fresh temp HOME and working directory.
func cliDirs(t *testing.T) (home, dir string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, dir = filepath.Join(base, "home"), filepath.Join(base, "work")
	for _, d := range []string{home, dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home, dir
}

func writeCLIGlobalConfig(t *testing.T, home, content string) string {
	t.Helper()
	dir := filepath.Join(home, ".config", "yipyip")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCLIVersionAnyDir(t *testing.T) {
	home, dirA := cliDirs(t)
	_, dirB := cliDirs(t)
	if err := os.WriteFile(filepath.Join(dirB, "yipyip.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{dirA, dirB, "/"} {
		res := runCLI(t, home, dir, nil, "--version")
		if res.code != 0 || strings.TrimSpace(res.stdout) != "yip test" {
			t.Errorf("--version in %s: code %d stdout %q stderr %q, want 0 and %q", dir, res.code, res.stdout, res.stderr, "yip test")
		}
	}
}

func TestCLIHelpListsCommands(t *testing.T) {
	home, dir := cliDirs(t)
	if err := os.WriteFile(filepath.Join(dir, "yipyip.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runCLI(t, home, dir, nil, "--help")
	if res.code != 0 {
		t.Fatalf("--help: code %d stderr %q", res.code, res.stderr)
	}
	for _, c := range []string{"/model", "/effort", "/init", "/new", "/exit", "/compact", "/undo", "/mode", "/agents", "/help"} {
		if !strings.Contains(res.stdout, c) {
			t.Errorf("--help output lacks %s", c)
		}
	}
}

func TestCLIMissingEnvExit1(t *testing.T) {
	home, dir := cliDirs(t)
	writeCLIGlobalConfig(t, home, `{"provider":{"p":{"name":"{env:YIP_TEST_NAME}","options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"{env:YIP_TEST_UNSET}"}}}}`)
	res := runCLI(t, home, dir, []string{"YIP_TEST_NAME=SECRET123"})
	if res.code != 1 {
		t.Errorf("exit code = %d, want 1 (stderr %q)", res.code, res.stderr)
	}
	if want := "yip: environment variable YIP_TEST_UNSET is not set or empty"; strings.TrimSpace(res.stderr) != want {
		t.Errorf("stderr = %q, want %q", res.stderr, want)
	}
	if strings.Contains(res.stderr, "SECRET123") || strings.Contains(res.stdout, "SECRET123") {
		t.Errorf("secret value leaked: stdout %q stderr %q", res.stdout, res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want empty", res.stdout)
	}
}

func TestCLIInvalidConfigExit1(t *testing.T) {
	home, dir := cliDirs(t)
	path := writeCLIGlobalConfig(t, home, "garbage {{{ not json")
	res := runCLI(t, home, dir, nil)
	if res.code != 1 {
		t.Errorf("exit code = %d, want 1 (stderr %q)", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, path) && !strings.Contains(res.stderr, "config.json") {
		t.Errorf("stderr = %q, want it to name %s", res.stderr, path)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want empty", res.stdout)
	}
}
