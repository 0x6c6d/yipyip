package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

const subTimeout = 3 * time.Second

// blockingRun blocks until release is closed or ctx is done.
func blockingRun(release <-chan struct{}) RunFunc {
	return func(ctx context.Context, a *Subagent, task, model string) (string, error) {
		select {
		case <-release:
			return "final:" + task, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func waitStatus(t *testing.T, m *Manager, id string, want SubStatus) SubInfo {
	t.Helper()
	deadline := time.After(subTimeout)
	for {
		if info, ok := m.Get(id); ok && info.Status == want {
			return info
		}
		select {
		case <-deadline:
			info, _ := m.Get(id)
			t.Fatalf("subagent %s: status %q, want %q", id, info.Status, want)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func waitBounded(t *testing.T, m *Manager, id string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), subTimeout)
	defer cancel()
	out, err := m.Wait(ctx, id)
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait(%s) timed out", id)
	}
	return out, err
}

func TestSpawnReturnsImmediately(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	var mu sync.Mutex
	var changes []SubInfo
	m := NewManager(0, blockingRun(release), func(i SubInfo) {
		mu.Lock()
		changes = append(changes, i)
		mu.Unlock()
	})
	start := time.Now()
	id, err := m.Spawn(context.Background(), "t1", "m1")
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("Spawn took %v", d)
	}
	if id != "a1" {
		t.Fatalf("id = %q, want a1", id)
	}
	info, ok := m.Get(id)
	if !ok || info.Status != SubRunning || info.Task != "t1" || info.Model != "m1" {
		t.Fatalf("Get = %+v, %v", info, ok)
	}
	if m.Running() != 1 {
		t.Fatalf("Running = %d", m.Running())
	}
	id2, err := m.Spawn(context.Background(), "t2", "")
	if err != nil || id2 != "a2" {
		t.Fatalf("second spawn = %q, %v", id2, err)
	}
	l := m.List()
	if len(l) != 2 || l[0].ID != "a1" || l[1].ID != "a2" {
		t.Fatalf("List = %+v", l)
	}
	mu.Lock()
	n := len(changes)
	mu.Unlock()
	if n < 2 {
		t.Fatalf("onChange called %d times, want >= 2", n)
	}
}

func TestSpawnLimit(t *testing.T) {
	release := make(chan struct{})
	m := NewManager(4, blockingRun(release), nil)
	var wg sync.WaitGroup
	var mu sync.Mutex
	okCount, errCount := 0, 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := m.Spawn(context.Background(), fmt.Sprintf("t%d", i), "")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if !strings.Contains(err.Error(), "subagent limit reached (max 4)") {
					t.Errorf("unexpected error: %v", err)
				}
				errCount++
			} else {
				okCount++
			}
		}(i)
	}
	wg.Wait()
	if okCount != 4 || errCount != 4 || m.Running() != 4 {
		t.Fatalf("ok=%d err=%d running=%d", okCount, errCount, m.Running())
	}
	if _, err := m.Spawn(context.Background(), "t5", ""); err == nil {
		t.Fatal("5th concurrent spawn must fail")
	}
	if len(m.List()) != 4 {
		t.Fatalf("refused spawn registered: %d", len(m.List()))
	}
	close(release)
	for _, info := range m.List() {
		waitBounded(t, m, info.ID)
	}
	if m.Running() != 0 {
		t.Fatalf("Running = %d after finish", m.Running())
	}
	if _, err := m.Spawn(context.Background(), "again", ""); err != nil {
		t.Fatalf("spawn after slots freed: %v", err)
	}
}

func TestWaitReturnsFinal(t *testing.T) {
	release := make(chan struct{})
	m := NewManager(4, blockingRun(release), nil)
	id, err := m.Spawn(context.Background(), "t1", "")
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	out, err := waitBounded(t, m, id)
	if err != nil || out != "final:t1" {
		t.Fatalf("Wait = %q, %v", out, err)
	}
	info, _ := m.Get(id)
	if info.Status != SubDone || info.Final != "final:t1" {
		t.Fatalf("info = %+v", info)
	}
	// Wait with an already-done ctx on a running subagent returns ctx.Err().
	m2 := NewManager(1, blockingRun(make(chan struct{})), nil)
	id2, _ := m2.Spawn(context.Background(), "x", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m2.Wait(ctx, id2); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait with done ctx = %v", err)
	}
	m2.CancelAll()
	waitStatus(t, m2, id2, SubCancelled)
}

func TestCancelSubagent(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	var mu sync.Mutex
	var last SubInfo
	m := NewManager(4, blockingRun(release), func(i SubInfo) {
		mu.Lock()
		last = i
		mu.Unlock()
	})
	id, _ := m.Spawn(context.Background(), "t1", "")
	other, _ := m.Spawn(context.Background(), "t2", "")
	if err := m.Cancel(id); err != nil {
		t.Fatal(err)
	}
	if _, err := waitBounded(t, m, id); err == nil {
		t.Fatal("Wait on cancelled subagent must error")
	}
	info := waitStatus(t, m, id, SubCancelled)
	if info.Err != "cancelled" {
		t.Fatalf("Err = %q", info.Err)
	}
	if st, _ := m.Get(other); st.Status != SubRunning {
		t.Fatalf("other subagent status %q", st.Status)
	}
	if m.Running() != 1 {
		t.Fatalf("Running = %d", m.Running())
	}
	if err := m.Cancel(id); err != nil {
		t.Fatalf("Cancel on finished subagent: %v", err)
	}
	m.CancelAll()
	waitStatus(t, m, other, SubCancelled)
	if m.Running() != 0 {
		t.Fatalf("Running = %d", m.Running())
	}
	mu.Lock()
	defer mu.Unlock()
	if last.Status != SubCancelled {
		t.Fatalf("last onChange status %q", last.Status)
	}
}

func TestParentCancel(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	m := NewManager(4, blockingRun(release), nil)
	keep, _ := m.Spawn(context.Background(), "keep", "")
	parent, cancel := context.WithCancel(context.Background())
	c1, _ := m.Spawn(parent, "c1", "")
	c2, _ := m.Spawn(parent, "c2", "")
	cancel()
	waitStatus(t, m, c1, SubCancelled)
	waitStatus(t, m, c2, SubCancelled)
	if info, _ := m.Get(keep); info.Status != SubRunning {
		t.Fatalf("unrelated subagent status %q", info.Status)
	}
	m.CancelAll()
	waitStatus(t, m, keep, SubCancelled)
}

func TestWaitUnknown(t *testing.T) {
	m := NewManager(4, blockingRun(make(chan struct{})), nil)
	ctx, cancel := context.WithTimeout(context.Background(), subTimeout)
	defer cancel()
	start := time.Now()
	_, err := m.Wait(ctx, "a99")
	if err == nil || errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("Wait unknown = %v after %v", err, time.Since(start))
	}
	if err := m.Cancel("a99"); err == nil {
		t.Fatal("Cancel unknown must error")
	}
	if _, ok := m.Get("a99"); ok {
		t.Fatal("Get unknown ok")
	}
	if _, ok := m.Transcript("a99"); ok {
		t.Fatal("Transcript unknown ok")
	}
}

func TestSubagentTokensAndTranscript(t *testing.T) {
	var mu sync.Mutex
	var seenID string
	run := func(ctx context.Context, a *Subagent, task, model string) (string, error) {
		mu.Lock()
		seenID = a.ID()
		mu.Unlock()
		a.Log("line 1")
		a.AddTokens(5)
		a.Log("line 2")
		a.AddTokens(7)
		return "ok", nil
	}
	m := NewManager(2, run, func(SubInfo) {})
	id, _ := m.Spawn(context.Background(), "t", "")
	if _, err := waitBounded(t, m, id); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if seenID != id {
		t.Fatalf("a.ID() = %q, want %q", seenID, id)
	}
	mu.Unlock()
	info, _ := m.Get(id)
	if info.Tokens != 12 {
		t.Fatalf("Tokens = %d", info.Tokens)
	}
	tr, ok := m.Transcript(id)
	if !ok || len(tr) != 2 || tr[0] != "line 1" || tr[1] != "line 2" {
		t.Fatalf("Transcript = %q, %v", tr, ok)
	}
	tr[0] = "mutated"
	if tr2, _ := m.Transcript(id); tr2[0] != "line 1" {
		t.Fatal("Transcript must return a copy")
	}
}

func TestSubagentFailed(t *testing.T) {
	run := func(ctx context.Context, a *Subagent, task, model string) (string, error) {
		if task == "panic" {
			panic("kaboom")
		}
		return "", errors.New("boom")
	}
	m := NewManager(1, run, nil)
	id, _ := m.Spawn(context.Background(), "fail", "")
	if _, err := waitBounded(t, m, id); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Wait = %v", err)
	}
	info := waitStatus(t, m, id, SubFailed)
	if info.Err != "boom" {
		t.Fatalf("Err = %q", info.Err)
	}
	pid, err := m.Spawn(context.Background(), "panic", "")
	if err != nil {
		t.Fatalf("slot not freed: %v", err)
	}
	if _, err := waitBounded(t, m, pid); err == nil {
		t.Fatal("panicking run must fail")
	}
	info = waitStatus(t, m, pid, SubFailed)
	if !strings.Contains(info.Err, "kaboom") || m.Running() != 0 {
		t.Fatalf("panic info = %+v running=%d", info, m.Running())
	}
}
