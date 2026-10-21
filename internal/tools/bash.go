package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	// DefaultTimeout applies when no (or a non-positive) timeout is given.
	DefaultTimeout = 120 * time.Second
	// MaxTimeout is the upper bound for any command timeout.
	MaxTimeout = 600 * time.Second
	// MaxOutputChars is the number of characters (runes) of output kept.
	MaxOutputChars = 30000
)

// maxKeptBytes bounds the output memory held for one command.
const maxKeptBytes = 1 << 20

// waitDelay bounds how long Wait blocks on pipes held open by background
// children after the command exited or was killed.
const waitDelay = 500 * time.Millisecond

// BashResult is the outcome of one command.
type BashResult struct {
	Output   string // combined stdout+stderr, already truncated with Truncate
	ExitCode int    // process exit code; -1 when killed (timeout/cancel) or not started
	TimedOut bool
	Canceled bool
}

// BashRunner runs shell commands in the start directory. It is the only place
// in non-test code that starts processes.
type BashRunner struct {
	Dir      string   // working directory (the start directory)
	StripEnv []string // variable names removed from the child environment
}

// ClampTimeout maps d into (0, MaxTimeout]: d <= 0 yields DefaultTimeout,
// d > MaxTimeout yields MaxTimeout.
func ClampTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return DefaultTimeout
	}
	if d > MaxTimeout {
		return MaxTimeout
	}
	return d
}

// Truncate keeps the first MaxOutputChars runes of s and appends a marker
// naming the number of omitted runes; shorter strings are returned unchanged.
func Truncate(s string) string {
	n := utf8.RuneCountInString(s)
	if n <= MaxOutputChars {
		return s
	}
	return s[:runePrefixLen(s, MaxOutputChars)] + truncMarker(n-MaxOutputChars)
}

func truncMarker(omitted int) string {
	return fmt.Sprintf("\n[output truncated: %d characters omitted]", omitted)
}

// runePrefixLen returns the byte length of the first n runes of s (decoded
// as utf8.RuneCountInString does).
func runePrefixLen(s string, n int) int {
	i := 0
	for k := 0; k < n && i < len(s); k++ {
		_, w := utf8.DecodeRuneInString(s[i:])
		i += w
	}
	return i
}

// Run executes `bash -c command` in b.Dir with stdin from /dev/null. On
// timeout or when ctx is done the whole process group is killed.
func (b *BashRunner) Run(ctx context.Context, command string, timeout time.Duration) BashResult {
	tctx, cancel := context.WithTimeout(ctx, ClampTimeout(timeout))
	defer cancel()

	out := &outputBuffer{}
	cmd := exec.CommandContext(tctx, "bash", "-c", command)
	cmd.Dir = b.Dir
	cmd.Env = b.env()
	cmd.Stdin = nil // exec connects stdin to the null device
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative pid: signal the whole process group.
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err != nil {
			_ = cmd.Process.Kill()
		}
		return err
	}
	cmd.WaitDelay = waitDelay

	if err := cmd.Start(); err != nil {
		return BashResult{Output: Truncate(err.Error()), ExitCode: -1}
	}
	waitErr := cmd.Wait()

	res := BashResult{Output: out.result()}
	switch {
	case ctx.Err() != nil:
		res.Canceled = true
		res.ExitCode = -1
	case errors.Is(tctx.Err(), context.DeadlineExceeded):
		res.TimedOut = true
		res.ExitCode = -1
	default:
		res.ExitCode = cmd.ProcessState.ExitCode()
		if res.ExitCode == -1 && waitErr != nil && res.Output == "" {
			res.Output = Truncate(waitErr.Error())
		}
	}
	if res.TimedOut || res.Canceled {
		// Make sure nothing of the group survives, even if Cancel raced.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return res
}

// env returns os.Environ() without the variables named in StripEnv.
func (b *BashRunner) env() []string {
	strip := make(map[string]bool, len(b.StripEnv))
	for _, name := range b.StripEnv {
		strip[name] = true
	}
	all := os.Environ()
	env := make([]string, 0, len(all))
	for _, kv := range all {
		name, _, _ := strings.Cut(kv, "=")
		if strip[name] {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// outputBuffer keeps the first maxKeptBytes bytes of the combined output and
// counts the runes of the whole stream exactly, decoding as
// utf8.RuneCountInString would on the full output.
type outputBuffer struct {
	mu        sync.Mutex
	kept      []byte
	overflow  bool
	carry     []byte // incomplete rune at the end of the stream so far
	runeCount int    // runes decoded so far, excluding carry
}

func (o *outputBuffer) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if room := maxKeptBytes - len(o.kept); room > 0 {
		k := min(room, len(p))
		o.kept = append(o.kept, p[:k]...)
		if k < len(p) {
			o.overflow = true
		}
	} else if len(p) > 0 {
		o.overflow = true
	}

	data := p
	if len(o.carry) > 0 {
		data = append(o.carry, p...)
		o.carry = nil
	}
	i := 0
	for i < len(data) {
		if !utf8.FullRune(data[i:]) {
			o.carry = append([]byte(nil), data[i:]...)
			break
		}
		_, w := utf8.DecodeRune(data[i:])
		i += w
		o.runeCount++
	}
	return len(p), nil
}

// result returns the output with Truncate semantics applied to the whole
// stream, even when bytes beyond maxKeptBytes were dropped.
func (o *outputBuffer) result() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.overflow {
		return Truncate(string(o.kept))
	}
	total := o.runeCount + utf8.RuneCount(o.carry)
	kept := string(o.kept)
	// kept holds at least maxKeptBytes/utf8.UTFMax > MaxOutputChars runes, so
	// its first MaxOutputChars runes decode exactly as in the full stream.
	return kept[:runePrefixLen(kept, MaxOutputChars)] + truncMarker(total-MaxOutputChars)
}
