//go:build ignore

// Supervisor probe for T302 (bash runner). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yipyip/internal/tools"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("PANIC:", r)
			os.Exit(125)
		}
	}()
	if len(os.Args) != 2 {
		setup("usage: probe <case>")
	}
	dir, err := os.MkdirTemp("", "bashprobe")
	if err != nil {
		setup("%v", err)
	}
	r := &tools.BashRunner{Dir: dir}
	ctx := context.Background()

	switch os.Args[1] {
	case "exit":
		res := r.Run(ctx, "echo hi; echo err >&2; touch here; exit 3", 10*time.Second)
		if res.ExitCode != 3 || !strings.Contains(res.Output, "hi") || !strings.Contains(res.Output, "err") || res.TimedOut || res.Canceled {
			fail("result = %+v", res)
		}
		if _, err := os.Stat(filepath.Join(dir, "here")); err != nil {
			fail("command did not run in Dir: %v", err)
		}
		pass("exit code and combined output")
	case "trunc":
		long := strings.Repeat("x", 35000)
		if got := tools.Truncate(long); got != strings.Repeat("x", 30000)+"\n[output truncated: 5000 characters omitted]" {
			fail("Truncate(35000 x) has length %d and tail %q", len(got), got[max(0, len(got)-60):])
		}
		if got := tools.Truncate(strings.Repeat("ä", 30001)); got != strings.Repeat("ä", 30000)+"\n[output truncated: 1 characters omitted]" {
			fail("Truncate must count characters (runes), not bytes")
		}
		if got := tools.Truncate("short"); got != "short" {
			fail("Truncate(short) = %q", got)
		}
		res := r.Run(ctx, "head -c 35000 /dev/zero | tr '\\0' y", 10*time.Second)
		if !strings.HasPrefix(res.Output, strings.Repeat("y", 30000)) || !strings.Contains(res.Output, "[output truncated: 5000 characters omitted]") {
			fail("Run output not truncated with marker (len %d)", len(res.Output))
		}
		pass("truncation")
	case "clamp":
		if tools.DefaultTimeout != 120*time.Second || tools.MaxTimeout != 600*time.Second || tools.MaxOutputChars != 30000 {
			fail("constants: %v %v %d", tools.DefaultTimeout, tools.MaxTimeout, tools.MaxOutputChars)
		}
		for in, want := range map[time.Duration]time.Duration{0: 120 * time.Second, -time.Second: 120 * time.Second, 5 * time.Second: 5 * time.Second, 700 * time.Second: 600 * time.Second} {
			if got := tools.ClampTimeout(in); got != want {
				fail("ClampTimeout(%v) = %v, want %v", in, got, want)
			}
		}
		pass("timeout clamping")
	case "timeout":
		start := time.Now()
		res := r.Run(ctx, "sleep 30", time.Second)
		if el := time.Since(start); el > 5*time.Second || !res.TimedOut || res.ExitCode == 0 {
			accepted("timeout not enforced: %+v after %v", res, el)
		}
		rejected("timed out: %+v", res)
	case "group":
		res := r.Run(ctx, "(sleep 2; touch marker) & sleep 30", time.Second)
		time.Sleep(3 * time.Second)
		if _, err := os.Stat(filepath.Join(dir, "marker")); err == nil {
			accepted("background child survived the timeout: %+v", res)
		}
		rejected("process group killed")
	case "cancel":
		cctx, cancel := context.WithCancel(ctx)
		time.AfterFunc(300*time.Millisecond, cancel)
		start := time.Now()
		res := r.Run(cctx, "sleep 30", 60*time.Second)
		if el := time.Since(start); el > 3*time.Second || !res.Canceled {
			accepted("cancel not honoured: %+v after %v", res, el)
		}
		rejected("cancelled: %+v", res)
	case "env":
		os.Setenv("YIP_PROBE_SECRET", "SECRET123")
		r.StripEnv = []string{"YIP_PROBE_SECRET"}
		res := r.Run(ctx, `echo "v=${YIP_PROBE_SECRET:-unset}"; env`, 10*time.Second)
		if strings.Contains(res.Output, "SECRET123") || !strings.Contains(res.Output, "v=unset") || !strings.Contains(res.Output, "PATH=") {
			accepted("stripped variable visible or environment lost")
		}
		rejected("variable stripped, rest of the environment kept")
	default:
		setup("unknown case %q", os.Args[1])
	}
}
