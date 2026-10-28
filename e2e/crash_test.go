package e2e

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"yipyip/internal/provider/providertest"
)

var (
	crashBinMu sync.Mutex
	crashBins  = map[string]string{}
)

// crashBinary builds yip with -X main.testPanic=mode (once per mode) and
// returns its path.
func crashBinary(t *testing.T, mode string) string {
	t.Helper()
	crashBinMu.Lock()
	defer crashBinMu.Unlock()
	if p, ok := crashBins[mode]; ok {
		return p
	}
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	tmp, err := os.MkdirTemp("", "yip-crash-bin")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(tmp, "yip")
	ctx, cancel := context.WithTimeout(context.Background(), 6*cliTimeout)
	defer cancel()
	ld := "-X main.version=test"
	if mode != "" {
		ld += " -X main.testPanic=" + mode
	}
	cmd := exec.CommandContext(ctx, "go", "build", "-ldflags", ld, "-o", bin, "./cmd/yip")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	crashBins[mode] = bin
	return bin
}

// runBinary runs bin in dir with a clean environment (PATH, HOME, extra),
// without a TTY, and returns its result.
func runBinary(t *testing.T, bin, home, dir string, extra []string, args ...string) cliResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + home}, extra...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	res := cliResult{}
	err := cmd.Run()
	res.stdout, res.stderr = stdout.String(), stderr.String()
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running yip %v: %v (stderr %q)", args, err, res.stderr)
		}
		res.code = ee.ExitCode()
	}
	return res
}

// crashDirs returns a temp HOME and start directory with a valid config
// using {env:FAKE_KEY}.
func crashDirs(t *testing.T) (home, dir string) {
	t.Helper()
	home, dir = cliDirs(t)
	writeCLIGlobalConfig(t, home, providertest.ConfigJSON("http://127.0.0.1:9/v1", "{env:FAKE_KEY}", 8000))
	return home, dir
}

func TestCrashFatalExit2(t *testing.T) {
	home, dir := crashDirs(t)
	res := runBinary(t, crashBinary(t, "launch"), home, dir, []string{"FAKE_KEY=" + secret})
	if res.code != 2 {
		t.Fatalf("exit code = %d, want 2 (stderr %q)", res.code, res.stderr)
	}
	logPath := filepath.Join(dir, ".yipyip", "crash.log")
	if want := "yip: crashed, details in " + logPath; strings.TrimSpace(res.stderr) != want {
		t.Errorf("stderr = %q, want %q", res.stderr, want)
	}
	fi, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("crash.log mode = %v, want 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Dir(logPath))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf(".yipyip mode = %v, want 0700", di.Mode().Perm())
	}
	b, _ := os.ReadFile(logPath)
	log := string(b)
	for _, want := range []string{"=== yip crash 20", "version: test", "panic: forced test panic", "stack:", "goroutine", "=== end ==="} {
		if !strings.Contains(log, want) {
			t.Errorf("crash.log lacks %q:\n%s", want, log)
		}
	}
}

// TestCrashSecretNotLogged forces a panic whose value holds FAKE_KEY=SECRET123.
func TestCrashSecretNotLogged(t *testing.T) {
	home, dir := crashDirs(t)
	res := runBinary(t, crashBinary(t, "leak"), home, dir, []string{"FAKE_KEY=" + secret})
	if res.code != 2 {
		t.Fatalf("exit code = %d, want 2 (stderr %q)", res.code, res.stderr)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".yipyip", "crash.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "panic: forced test panic") {
		t.Errorf("crash.log lacks the panic line:\n%s", b)
	}
	if strings.Contains(string(b), secret) {
		t.Errorf("crash.log holds %s", secret)
	}
	if strings.Contains(res.stderr, secret) || strings.Contains(res.stdout, secret) {
		t.Errorf("output holds %s: stdout %q stderr %q", secret, res.stdout, res.stderr)
	}
}

func TestCrashCapped(t *testing.T) {
	home, dir := crashDirs(t)
	logPath := filepath.Join(dir, ".yipyip", "crash.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	var old strings.Builder
	rec := "=== yip crash 2020-01-01T00:00:00Z fatal ===\nversion: old\npanic: old\nstack:\n" + strings.Repeat("x", 1000) + "\n=== end ===\n"
	for old.Len() < 1200*1024 {
		old.WriteString(rec)
	}
	if err := os.WriteFile(logPath, []byte(old.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	res := runBinary(t, crashBinary(t, "launch"), home, dir, []string{"FAKE_KEY=" + secret})
	if res.code != 2 {
		t.Fatalf("exit code = %d, want 2 (stderr %q)", res.code, res.stderr)
	}
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 512*1024 {
		t.Errorf("crash.log is %d bytes, want <= 512 KiB", len(b))
	}
	s := string(b)
	i := strings.LastIndex(s, "=== yip crash ")
	if i < 0 || !strings.Contains(s[i:], "panic: forced test panic") || !strings.HasSuffix(s, "=== end ===\n") {
		t.Errorf("crash.log does not end with the new record")
	}
	if !strings.Contains(s, "version: old") {
		t.Errorf("crash.log kept no old record")
	}
}

func TestCrashNoPanicNoLog(t *testing.T) {
	home, dir := crashDirs(t)
	res := runBinary(t, crashBinary(t, "launch"), home, dir, []string{"FAKE_KEY=" + secret}, "--version")
	if res.code != 0 {
		t.Fatalf("--version exit code = %d (stderr %q)", res.code, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".yipyip", "crash.log")); !os.IsNotExist(err) {
		t.Errorf("crash.log exists after --version (err %v)", err)
	}
}
