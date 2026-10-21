package tools

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func newRunner(t *testing.T) (*BashRunner, string) {
	t.Helper()
	dir := t.TempDir()
	return &BashRunner{Dir: dir}, dir
}

func TestBashExitCode(t *testing.T) {
	r, _ := newRunner(t)
	ctx := context.Background()
	for _, tc := range []struct {
		cmd  string
		want int
	}{
		{"true", 0},
		{"exit 3", 3},
		{"false", 1},
		{"no-such-command-yipyip", 127},
	} {
		res := r.Run(ctx, tc.cmd, 10*time.Second)
		if res.ExitCode != tc.want || res.TimedOut || res.Canceled {
			t.Errorf("Run(%q) = %+v, want exit %d", tc.cmd, res, tc.want)
		}
	}

	bad := &BashRunner{Dir: filepath.Join(t.TempDir(), "missing")}
	res := bad.Run(ctx, "true", 10*time.Second)
	if res.ExitCode != -1 || res.Output == "" {
		t.Errorf("start failure: %+v, want exit -1 with error text", res)
	}
}

func TestBashCombinedOutput(t *testing.T) {
	r, _ := newRunner(t)
	res := r.Run(context.Background(), "echo one; echo two >&2; echo three", 10*time.Second)
	if res.ExitCode != 0 {
		t.Fatalf("result = %+v", res)
	}
	if res.Output != "one\ntwo\nthree\n" {
		t.Errorf("output = %q, want stdout and stderr combined in order", res.Output)
	}

	res = r.Run(context.Background(), "cat; echo done", 10*time.Second)
	if res.Output != "done\n" {
		t.Errorf("stdin not empty: output = %q", res.Output)
	}
}

func TestBashDir(t *testing.T) {
	r, dir := newRunner(t)
	res := r.Run(context.Background(), "pwd -P; touch created", 10*time.Second)
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Output) != want {
		t.Errorf("pwd = %q, want %q", res.Output, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "created")); err != nil {
		t.Errorf("file not created in Dir: %v", err)
	}
}

func TestBashTimeout(t *testing.T) {
	r, _ := newRunner(t)
	start := time.Now()
	res := r.Run(context.Background(), "echo started; sleep 30", time.Second)
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("Run took %v", el)
	}
	if !res.TimedOut || res.Canceled || res.ExitCode != -1 {
		t.Errorf("result = %+v, want TimedOut with exit -1", res)
	}
	if !strings.Contains(res.Output, "started") {
		t.Errorf("output before timeout lost: %q", res.Output)
	}
}

func TestBashTimeoutKillsGroup(t *testing.T) {
	r, dir := newRunner(t)
	start := time.Now()
	res := r.Run(context.Background(), "(sleep 2; touch marker) & sleep 30", time.Second)
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("Run took %v", el)
	}
	if !res.TimedOut {
		t.Fatalf("result = %+v, want TimedOut", res)
	}
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "marker")); err == nil {
		t.Error("background child survived the timeout")
	}
}

func TestBashCancel(t *testing.T) {
	r, _ := newRunner(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	start := time.Now()
	res := r.Run(ctx, "sleep 30", 60*time.Second)
	if el := time.Since(start); el > 3*time.Second {
		t.Fatalf("Run took %v", el)
	}
	if !res.Canceled || res.TimedOut || res.ExitCode != -1 {
		t.Errorf("result = %+v, want Canceled with exit -1", res)
	}
}

func TestBashTruncation(t *testing.T) {
	want := strings.Repeat("x", 30000) + "\n[output truncated: 5000 characters omitted]"
	if got := Truncate(strings.Repeat("x", 35000)); got != want {
		t.Errorf("Truncate(35000 x): len %d", len(got))
	}
	if got := Truncate(strings.Repeat("ä", 30001)); got != strings.Repeat("ä", 30000)+"\n[output truncated: 1 characters omitted]" {
		t.Error("Truncate must count runes, not bytes")
	}
	exact := strings.Repeat("€", 30000)
	if got := Truncate(exact); got != exact {
		t.Error("Truncate changed a string of exactly MaxOutputChars runes")
	}
	if got := Truncate("short"); got != "short" {
		t.Errorf("Truncate(short) = %q", got)
	}

	r, _ := newRunner(t)
	res := r.Run(context.Background(), "head -c 35000 /dev/zero | tr '\\0' y", 10*time.Second)
	if res.Output != strings.Repeat("y", 30000)+"\n[output truncated: 5000 characters omitted]" {
		t.Errorf("Run output not truncated (len %d)", len(res.Output))
	}

	// Beyond the kept memory: 3-byte runes, 700000 of them (2.1 MB).
	res = r.Run(context.Background(), "yes '€€€€€€€€€€' | tr -d '\\n' | head -c 2100000", 30*time.Second)
	wantOut := strings.Repeat("€", 30000) + "\n[output truncated: 670000 characters omitted]"
	if res.Output != wantOut {
		t.Errorf("large multi-byte output: len %d, tail %q", len(res.Output), res.Output[max(0, len(res.Output)-60):])
	}
}

func TestBashOutputBufferSplitRunes(t *testing.T) {
	// Feed the stream in awkward chunk sizes, so runes and invalid bytes
	// straddle write and memory boundaries; the count must match
	// utf8.RuneCountInString on the whole stream.
	var sb strings.Builder
	for sb.Len() < 3*maxKeptBytes {
		sb.WriteString("aä€😀\xff\xe2\x82")
	}
	stream := sb.String()
	for _, chunk := range []int{1, 2, 3, 5, 7, 4096, 1 << 20} {
		o := &outputBuffer{}
		for i := 0; i < len(stream); i += chunk {
			_, _ = o.Write([]byte(stream[i:min(i+chunk, len(stream))]))
		}
		if got, want := o.result(), Truncate(stream); got != want {
			t.Errorf("chunk %d: result differs (len %d vs %d)", chunk, len(got), len(want))
		}
		if len(o.kept) > maxKeptBytes {
			t.Errorf("chunk %d: kept %d bytes", chunk, len(o.kept))
		}
	}
	n := utf8.RuneCountInString(stream)
	if !strings.HasSuffix(Truncate(stream), "[output truncated: "+strconv.Itoa(n-MaxOutputChars)+" characters omitted]") {
		t.Error("marker count mismatch")
	}
}

func TestClampTimeout(t *testing.T) {
	if DefaultTimeout != 120*time.Second || MaxTimeout != 600*time.Second || MaxOutputChars != 30000 {
		t.Fatalf("constants: %v %v %d", DefaultTimeout, MaxTimeout, MaxOutputChars)
	}
	for in, want := range map[time.Duration]time.Duration{
		0:                  DefaultTimeout,
		-time.Second:       DefaultTimeout,
		5 * time.Second:    5 * time.Second,
		MaxTimeout:         MaxTimeout,
		700 * time.Second:  MaxTimeout,
		1000 * time.Second: MaxTimeout,
	} {
		if got := ClampTimeout(in); got != want {
			t.Errorf("ClampTimeout(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestSecretBashEnv(t *testing.T) {
	t.Setenv("YIP_TEST_SECRET", "SECRET123")
	t.Setenv("YIP_TEST_KEEP", "kept")
	r, _ := newRunner(t)
	r.StripEnv = []string{"YIP_TEST_SECRET"}
	res := r.Run(context.Background(), `echo "v=${YIP_TEST_SECRET:-unset}"; echo "k=$YIP_TEST_KEEP"; env`, 10*time.Second)
	if strings.Contains(res.Output, "SECRET123") {
		t.Error("stripped variable reached the command")
	}
	if !strings.Contains(res.Output, "v=unset") || !strings.Contains(res.Output, "k=kept") || !strings.Contains(res.Output, "PATH=") {
		t.Errorf("environment not kept: %q", res.Output)
	}
}
