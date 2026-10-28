package crashlog

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func memLogger(dir string) *Logger {
	return New(Options{
		Version: "test",
		Redact:  func(s string) string { return strings.ReplaceAll(s, "SECRET", "***") },
		Resolve: func(rel string) (string, error) { return filepath.Join(dir, rel), nil },
	})
}

func TestMemoryRecord(t *testing.T) {
	dir := t.TempDir()
	l := memLogger(dir)
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	l.memoryRecord("heap above limit", &ms, true)

	data, err := os.ReadFile(filepath.Join(dir, logRel))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{recordHead, " memory ===", "reason: heap above limit", "heap_alloc_mib:", "goroutines:", "heap_profile:", endLine} {
		if !strings.Contains(s, want) {
			t.Errorf("record lacks %q:\n%s", want, s)
		}
	}
	if fi, err := os.Stat(filepath.Join(dir, heapProfRel)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("heap profile: %v %v", fi, err)
	}
}

func TestWatchStopsAndNilIsSafe(t *testing.T) {
	var nl *Logger
	nl.Watch(context.Background(), time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { memLogger(t.TempDir()).Watch(ctx, time.Millisecond); close(done) }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not stop")
	}
}
