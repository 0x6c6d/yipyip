package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yipyip/internal/session"
)

// writePruneSessions writes n valid session files to the sessions directory
// of dir; session i has id pruneSessionID(i) and is i minutes newer than
// session 0, so the last one is the newest.
func writePruneSessions(t *testing.T, dir string, n int) {
	t.Helper()
	sdir := filepath.Join(dir, ".yipyip", "sessions")
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		data := session.NewSessionData()
		data.ID = pruneSessionID(i)
		data.StartDir = dir
		data.Mode = "auto"
		stamp := base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		data.Created, data.Updated = stamp, stamp
		data.Title = fmt.Sprintf("session %d", i)
		data.Conversation = []session.Conversation{{Role: "user", Content: fmt.Sprintf("hello %d", i)}}
		b, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sdir, data.ID+".json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// pruneSessionID is the id of the i-th session written by writePruneSessions.
func pruneSessionID(i int) string { return fmt.Sprintf("%012x", 0xa00+i) }

// sessionFileExists reports whether dir holds the session file of id.
func sessionFileExists(t *testing.T, dir, id string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, ".yipyip", "sessions", id+".json"))
	return err == nil
}

// countSessionFiles returns the number of valid session files in dir.
func countSessionFiles(t *testing.T, dir string) int {
	t.Helper()
	st, err := session.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	list, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	return len(list)
}

func TestStartupPruneKeeps25(t *testing.T) {
	dir := resumeStartDir(t)
	writePruneSessions(t, dir, 30)
	_, launched, code, stderr := runResume(t, nil, false)
	if code != 0 || !launched {
		t.Fatalf("code = %d, launched = %v (stderr %q)", code, launched, stderr)
	}
	if n := countSessionFiles(t, dir); n != 25 {
		t.Fatalf("%d sessions remain, want 25", n)
	}
	for i := 0; i < 30; i++ {
		if got, want := sessionFileExists(t, dir, pruneSessionID(i)), i >= 5; got != want {
			t.Errorf("session %d exists = %v, want %v", i, got, want)
		}
	}
}

func TestStartupPruneProtectsResumed(t *testing.T) {
	dir := resumeStartDir(t)
	writePruneSessions(t, dir, 30)
	old := pruneSessionID(2) // the 28th newest of 30
	opts, launched, code, stderr := runResume(t, []string{"--resume", old}, false)
	if code != 0 || !launched {
		t.Fatalf("code = %d, launched = %v (stderr %q)", code, launched, stderr)
	}
	if opts.ResumeData == nil || opts.ResumeData.ID != old {
		t.Fatalf("ResumeData = %+v, want id %s", opts.ResumeData, old)
	}
	if !sessionFileExists(t, dir, old) {
		t.Fatalf("resumed session %s was pruned", old)
	}
	st, err := session.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Load(old); err != nil {
		t.Fatalf("resumed session does not load after the prune: %v", err)
	}
	if n := countSessionFiles(t, dir); n != 26 {
		t.Fatalf("%d sessions remain, want 26 (25 newest + the resumed one)", n)
	}
	for _, i := range []int{0, 1, 3, 4} {
		if sessionFileExists(t, dir, pruneSessionID(i)) {
			t.Errorf("old session %d survived", i)
		}
	}
}

func TestStartupPruneIgnoresStrayFiles(t *testing.T) {
	dir := resumeStartDir(t)
	writePruneSessions(t, dir, 30)
	sdir := filepath.Join(dir, ".yipyip", "sessions")
	stray := map[string]string{
		"notes.txt":         "keep me",
		"zzz.json":          "{}",
		".tmp-123":          "partial",
		"0123456789ab.json": `{"version": 2}`,
		"deadbeefdead.json": "not json",
	}
	for name, content := range stray {
		if err := os.WriteFile(filepath.Join(sdir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(sdir, "abcdefabcdef.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, launched, code, stderr := runResume(t, nil, false)
	if code != 0 || !launched {
		t.Fatalf("code = %d, launched = %v (stderr %q)", code, launched, stderr)
	}
	for name, content := range stray {
		b, err := os.ReadFile(filepath.Join(sdir, name))
		if err != nil || string(b) != content {
			t.Errorf("stray file %s changed: %q, %v", name, b, err)
		}
	}
	if fi, err := os.Stat(filepath.Join(sdir, "abcdefabcdef.json")); err != nil || !fi.IsDir() {
		t.Errorf("stray directory touched: %v", err)
	}
	if n := countSessionFiles(t, dir); n != 25 {
		t.Fatalf("%d sessions remain, want 25", n)
	}
}

func TestStartupPruneNoSessionsDir(t *testing.T) {
	dir := resumeStartDir(t)
	_, launched, code, stderr := runResume(t, nil, false)
	if code != 0 || !launched || stderr != "" {
		t.Fatalf("code = %d, launched = %v, stderr %q", code, launched, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".yipyip", "sessions")); !os.IsNotExist(err) {
		t.Fatalf("sessions directory created by the startup prune: %v", err)
	}
}
