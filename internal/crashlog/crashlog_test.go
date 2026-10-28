package crashlog

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// testLogger returns a Logger confined to a fresh temp dir and that dir.
func testLogger(t *testing.T, mod func(*Options)) (*Logger, string) {
	t.Helper()
	dir := t.TempDir()
	o := Options{
		Version: "v-test",
		Resolve: func(rel string) (string, error) { return filepath.Join(dir, rel), nil },
	}
	if mod != nil {
		mod(&o)
	}
	return New(o), dir
}

// readLog returns the contents of crash.log under dir.
func readLog(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".yipyip", "crash.log"))
	if err != nil {
		t.Fatalf("read crash.log: %v", err)
	}
	return string(b)
}

// panicsHere panics so a recovered stack names it.
//
//go:noinline
func panicsHere(msg string) { panic(msg) }

// recoverInto panics in panicsHere and records the recovered value with l.
func recoverInto(l *Logger, where, msg string) {
	defer func() {
		if r := recover(); r != nil {
			l.Recovered(where, r)
		}
	}()
	panicsHere(msg)
}

func TestCrashlogRecordFormat(t *testing.T) {
	l, dir := testLogger(t, nil)
	path, err := l.Fatal("boom")
	if err != nil {
		t.Fatalf("Fatal: %v", err)
	}
	if want := filepath.Join(dir, ".yipyip", "crash.log"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	got := readLog(t, dir)
	head := regexp.MustCompile(`^=== yip crash \d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ fatal ===\n`)
	if !head.MatchString(got) {
		t.Fatalf("bad header:\n%s", got)
	}
	for _, want := range []string{"version: v-test\n", "panic: boom\n", "stack:\n", "TestCrashlogRecordFormat"} {
		if !strings.Contains(got, want) {
			t.Fatalf("record lacks %q:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, "=== end ===\n") {
		t.Fatalf("record does not end with the end marker")
	}
	if strings.Contains(got, "where:") || strings.Contains(got, "session:") {
		t.Fatalf("fatal record without session has where/session lines:\n%s", got)
	}
}

func TestCrashlogRecoveredMarked(t *testing.T) {
	l, dir := testLogger(t, nil)
	recoverInto(l, "tool loop", "bad thing")
	got := readLog(t, dir)
	for _, want := range []string{" recovered ===\n", "where: tool loop\n", "panic: bad thing\n", "panicsHere"} {
		if !strings.Contains(got, want) {
			t.Fatalf("record lacks %q:\n%s", want, got)
		}
	}
}

func TestCrashlogSessionID(t *testing.T) {
	id := "abc123def456"
	l, dir := testLogger(t, func(o *Options) { o.SessionID = func() string { return id } })
	l.Recovered("x", "v")
	if got := readLog(t, dir); !strings.Contains(got, "session: abc123def456\n") {
		t.Fatalf("no session line:\n%s", got)
	}
	id = ""
	if err := os.Remove(filepath.Join(dir, ".yipyip", "crash.log")); err != nil {
		t.Fatal(err)
	}
	l.Recovered("x", "v")
	if got := readLog(t, dir); strings.Contains(got, "session:") {
		t.Fatalf("session line for an empty id:\n%s", got)
	}
}

func TestCrashlogPerms(t *testing.T) {
	l, dir := testLogger(t, nil)
	l.Recovered("x", "v")
	di, err := os.Stat(filepath.Join(dir, ".yipyip"))
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, ".yipyip", "crash.log"))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("dir %v file %v, want 0700 / 0600", di.Mode().Perm(), fi.Mode().Perm())
	}

	l2, dir2 := testLogger(t, nil)
	if err := os.Mkdir(filepath.Join(dir2, ".yipyip"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir2, ".yipyip"), 0o755); err != nil {
		t.Fatal(err)
	}
	l2.Recovered("x", "v")
	di2, err := os.Stat(filepath.Join(dir2, ".yipyip"))
	if err != nil {
		t.Fatal(err)
	}
	if di2.Mode().Perm() != 0o755 {
		t.Fatalf("existing dir mode changed to %v", di2.Mode().Perm())
	}
}

func TestCrashlogCap(t *testing.T) {
	l, dir := testLogger(t, nil)
	big := strings.Repeat("x", 60<<10)
	cut := false
	var last int64
	for i := 0; i < 40; i++ {
		l.Recovered("cap", fmt.Sprintf("seq-%02d %s", i, big))
		fi, err := os.Stat(filepath.Join(dir, ".yipyip", "crash.log"))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Size() > MaxBytes {
			t.Fatalf("size %d above MaxBytes", fi.Size())
		}
		if fi.Size() < last {
			cut = true
			if fi.Size() > KeepBytes {
				t.Fatalf("cut to %d, want at most %d", fi.Size(), KeepBytes)
			}
		}
		last = fi.Size()
	}
	if !cut {
		t.Fatal("the log was never cut")
	}
	got := readLog(t, dir)
	if !strings.HasPrefix(got, "=== yip crash ") {
		t.Fatal("log does not start with a record header")
	}
	if strings.Contains(got, "seq-00 ") {
		t.Fatal("oldest record kept")
	}
	i := strings.LastIndex(got, "=== yip crash ")
	newest := got[i:]
	if !strings.Contains(newest, "panic: seq-39 "+big+"\n") || !strings.HasSuffix(newest, "=== end ===\n") {
		t.Fatal("newest record not intact")
	}
	if strings.Count(got, "=== yip crash ") != strings.Count(got, "=== end ===\n") {
		t.Fatal("partial record kept")
	}
}

func TestCrashlogCapSingleHugeRecord(t *testing.T) {
	l, dir := testLogger(t, nil)
	line := strings.Repeat("y", 1000) + "\n"
	l.Recovered("huge", "start\n"+strings.Repeat(line, 1200)+"tail")
	got := readLog(t, dir)
	if len(got) > KeepBytes {
		t.Fatalf("size %d above KeepBytes", len(got))
	}
	if !strings.HasPrefix(got, "=== yip crash ") || !strings.HasSuffix(got, "\n=== end ===\n") {
		t.Fatalf("huge record not cut to a closed head")
	}
	if !strings.Contains(got, "panic: start\n") {
		t.Fatal("head of the record lost")
	}
	body := strings.TrimSuffix(got, "=== end ===\n")
	for _, ln := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
		if strings.HasPrefix(ln, "y") && len(ln) != 1000 {
			t.Fatalf("line cut in the middle (%d chars)", len(ln))
		}
	}
}

func TestCrashlogRedacts(t *testing.T) {
	l, dir := testLogger(t, func(o *Options) {
		o.Version = "v-SECRET123"
		o.Redact = func(s string) string { return strings.ReplaceAll(s, "SECRET123", "[redacted]") }
	})
	func() {
		defer func() {
			if r := recover(); r != nil {
				l.Recovered("redact", r)
			}
		}()
		panicsHere("token SECRET123 here")
	}()
	if _, err := l.Fatal(errors.New("err SECRET123")); err != nil {
		t.Fatal(err)
	}
	got := readLog(t, dir)
	if strings.Contains(got, "SECRET123") {
		t.Fatalf("secret in crash.log:\n%s", got)
	}
	for _, want := range []string{"panic: token [redacted] here", "panic: err [redacted]", "version: v-[redacted]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("lacks %q", want)
		}
	}
}

func TestCrashlogResolveError(t *testing.T) {
	dir := t.TempDir()
	l := New(Options{
		Version: "v",
		Resolve: func(string) (string, error) { return "", errors.New("outside the start directory") },
	})
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	path, ferr := l.Fatal("x-value")
	l.Recovered("y", "y-value")
	os.Stderr = old
	w.Close()
	out, _ := io.ReadAll(r)
	r.Close()
	if ferr == nil || path != "" {
		t.Fatalf("Fatal = %q, %v; want an error", path, ferr)
	}
	for _, want := range []string{"panic: x-value", "panic: y-value", "=== yip crash "} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("stderr lacks %q:\n%s", want, out)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("files created: %v", entries)
	}
}

func TestCrashlogNoDefaultNoop(t *testing.T) {
	SetDefault(nil)
	t.Cleanup(func() { SetDefault(nil) })
	if Default() != nil {
		t.Fatal("default set")
	}
	Recovered("x", "v") // must not panic
	var nl *Logger
	if p, err := nl.Fatal("v"); p != "" || err != nil {
		t.Fatalf("nil Fatal = %q, %v", p, err)
	}
	nl.Recovered("x", "v")

	l, dir := testLogger(t, nil)
	SetDefault(l)
	if Default() != l {
		t.Fatal("Default() is not the set logger")
	}
	Recovered("via default", "v")
	if got := readLog(t, dir); !strings.Contains(got, "where: via default") {
		t.Fatalf("default not used:\n%s", got)
	}
}

func TestCrashlogConcurrent(t *testing.T) {
	l, dir := testLogger(t, nil)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				recoverInto(l, "g", fmt.Sprintf("g-%02d", i))
			} else if _, err := l.Fatal(fmt.Sprintf("g-%02d", i)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	got := readLog(t, dir)
	recs := strings.Split(got, "=== end ===\n")
	if recs[len(recs)-1] != "" {
		t.Fatal("log does not end with a closed record")
	}
	recs = recs[:len(recs)-1]
	if len(recs) != 20 {
		t.Fatalf("%d records, want 20", len(recs))
	}
	seen := map[string]bool{}
	for _, r := range recs {
		if !strings.HasPrefix(r, "=== yip crash ") || strings.Count(r, "=== yip crash ") != 1 {
			t.Fatalf("interleaved record:\n%s", r)
		}
		m := regexp.MustCompile(`panic: (g-\d\d)\n`).FindStringSubmatch(r)
		if m == nil {
			t.Fatalf("record without panic line:\n%s", r)
		}
		seen[m[1]] = true
	}
	if len(seen) != 20 {
		t.Fatalf("%d distinct records, want 20", len(seen))
	}
}
