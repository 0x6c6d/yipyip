package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// pruneID returns the 12-hex session id of seeded file i.
func pruneID(i int) string { return fmt.Sprintf("%012x", i+1) }

// pruneStart creates a start directory with an empty sessions directory and
// returns the start directory, the sessions directory and a store for it.
func pruneStart(t *testing.T) (string, string, *Store) {
	t.Helper()
	start, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sdir := filepath.Join(start, ".yipyip", "sessions")
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := NewStore(start)
	if err != nil {
		t.Fatal(err)
	}
	return start, sdir, st
}

// pruneSeed writes n session files into dir; file i has updated = base + i
// seconds, so a higher i is newer.
func pruneSeed(t *testing.T, dir string, n int) {
	t.Helper()
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		ts := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339)
		body := fmt.Sprintf(`{"version":1,"id":%q,"created":%q,"updated":%q}`, pruneID(i), ts, ts)
		if err := os.WriteFile(filepath.Join(dir, pruneID(i)+".json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// pruneExists reports whether path exists.
func pruneExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// pruneCheck asserts that seeded file i exists exactly when want(i).
func pruneCheck(t *testing.T, dir string, n int, want func(int) bool) {
	t.Helper()
	for i := 0; i < n; i++ {
		if got := pruneExists(filepath.Join(dir, pruneID(i)+".json")); got != want(i) {
			t.Errorf("file %d exists=%v, want %v", i, got, want(i))
		}
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	_, sdir, st := pruneStart(t)
	pruneSeed(t, sdir, 30)
	if err := st.Prune(25); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	pruneCheck(t, sdir, 30, func(i int) bool { return i >= 5 })
}

func TestPruneProtect(t *testing.T) {
	_, sdir, st := pruneStart(t)
	pruneSeed(t, sdir, 30)
	if err := st.Prune(25, pruneID(0), pruneID(1)); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	pruneCheck(t, sdir, 30, func(i int) bool { return i >= 5 || i < 2 })
	ents, _ := os.ReadDir(sdir)
	if len(ents) != 27 {
		t.Errorf("%d files left, want 27", len(ents))
	}
}

func TestPruneStrayFilesUntouched(t *testing.T) {
	_, sdir, st := pruneStart(t)
	pruneSeed(t, sdir, 30)
	strays := []string{"notes.txt", "abc.json", ".tmp-12345"}
	for _, s := range strays {
		if err := os.WriteFile(filepath.Join(sdir, s), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sub := filepath.Join(sdir, "aaaaaaaaaaaa.json")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := st.Prune(25); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	for _, s := range append(strays, filepath.Base(sub)) {
		if !pruneExists(filepath.Join(sdir, s)) {
			t.Errorf("stray %s was deleted", s)
		}
	}
	pruneCheck(t, sdir, 30, func(i int) bool { return i >= 5 })
}

func TestPruneInvalidJSONNotCounted(t *testing.T) {
	_, sdir, st := pruneStart(t)
	pruneSeed(t, sdir, 30)
	bad := filepath.Join(sdir, "ffffffffffff.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(sdir, "eeeeeeeeeeee.json")
	if err := os.WriteFile(old, []byte(`{"version":2,"id":"eeeeeeeeeeee"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := st.Prune(25); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	for _, p := range []string{bad, old} {
		if !pruneExists(p) {
			t.Errorf("%s was deleted", p)
		}
	}
	pruneCheck(t, sdir, 30, func(i int) bool { return i >= 5 })
}

func TestPruneMissingDir(t *testing.T) {
	start, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st, err := NewStore(start)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Prune(25); err != nil {
		t.Errorf("Prune on a missing sessions dir: %v, want nil", err)
	}
	if pruneExists(filepath.Join(start, ".yipyip")) {
		t.Error("Prune created .yipyip")
	}
}

func TestPruneKeepNotPositive(t *testing.T) {
	_, sdir, st := pruneStart(t)
	pruneSeed(t, sdir, 30)
	for _, keep := range []int{0, -1} {
		if err := st.Prune(keep); err == nil {
			t.Errorf("Prune(%d) returned nil, want an error", keep)
		}
	}
	pruneCheck(t, sdir, 30, func(int) bool { return true })
}

func TestPruneSymlinkEscape(t *testing.T) {
	start, sdir, st := pruneStart(t)
	outside := t.TempDir()
	pruneSeed(t, outside, 30)
	if err := os.Remove(sdir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, sdir); err != nil {
		t.Fatal(err)
	}
	if err := st.Prune(25); err == nil {
		t.Error("Prune through an escaping sessions symlink returned nil, want an error")
	}
	pruneCheck(t, outside, 30, func(int) bool { return true })

	// A .yipyip symlink escaping the start directory is refused the same way.
	if err := os.RemoveAll(filepath.Join(start, ".yipyip")); err != nil {
		t.Fatal(err)
	}
	outer := t.TempDir()
	if err := os.Mkdir(filepath.Join(outer, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	pruneSeed(t, filepath.Join(outer, "sessions"), 30)
	if err := os.Symlink(outer, filepath.Join(start, ".yipyip")); err != nil {
		t.Fatal(err)
	}
	if err := st.Prune(25); err == nil {
		t.Error("Prune through an escaping .yipyip symlink returned nil, want an error")
	}
	pruneCheck(t, filepath.Join(outer, "sessions"), 30, func(int) bool { return true })
}

func TestPruneUnderKeepNoop(t *testing.T) {
	_, sdir, st := pruneStart(t)
	pruneSeed(t, sdir, 10)
	if err := st.Prune(25); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	pruneCheck(t, sdir, 10, func(int) bool { return true })
	if err := st.Prune(10); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	pruneCheck(t, sdir, 10, func(int) bool { return true })
}
