package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// stubNow freezes the store clock at t and returns the function that
// restores it. It replaces the package-level now the store stamps files
// with, so tests never depend on wall-clock timing or sleep.
func stubNow(t *testing.T, ts time.Time) {
	t.Helper()
	old := now
	now = func() time.Time { return ts }
	t.Cleanup(func() { now = old })
}

// fixture fills every field of a SessionData, including a conversation
// entry with a tool call, a tool result, a paste and a queued message.
func fixture() *SessionData {
	d := &SessionData{
		Version:  FileVersion,
		Created:  "2026-07-14T10:00:00Z",
		Updated:  "2026-07-14T10:05:00Z",
		StartDir: "/home/sa/work",
		Model:    "big",
		Effort:   "medium",
		Mode:     "manual",
		InTokens: 11, OutTokens: 7, TotalTokens: 18,
		LastContext: 11,
		Conversation: []Conversation{
			{Role: "system", Content: "system prompt"},
			{Role: "user", Content: "read the file"},
			{Role: "assistant", Content: "", ToolCalls: []ToolCall{{
				ID: "call_1", Type: "function",
				Function: Func{Name: "read_file", Arguments: `{"path":"x.go"}`},
			}}},
			// Multi-run weakly typed entry: arguments are a raw JSON string,
			// the fields a hand-edited or older file may carry.
			{Role: "tool", Content: "package main", ToolCallID: "call_1"},
		},
		History: []string{"read the file", "exit"},
		Pastes: Pastes{
			N: 1,
			Entries: map[string]string{
				"[Pasted text #1 +2 lines]": "line1\nline2\n",
			},
		},
		Queue: []string{"queued message"},
		Title: "read the file",
		Turn:  Turn{Active: "", Started: "2026-07-14T10:05:00Z", Interrupted: true},
	}
	return d
}

func TestSaveLoadRoundTrip(t *testing.T) {
	stubNow(t, time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC))
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := fixture()
	if err := st.Save(data); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(data.ID) {
		t.Fatalf("id = %q, want 12 lowercase hex chars", data.ID)
	}
	loaded, err := st.Load(data.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !equalData(data, loaded) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", loaded, data)
	}
	// Two saves of the same content keep the id (it derives only when empty).
	id := data.ID
	data2 := fixture()
	data2.ID = ""
	if err := st.Save(data2); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if data2.ID != id {
		t.Errorf("same content derived id %q, want kept id %q", data2.ID, id)
	}
	// A changed second save also keeps the id once it is set.
	data.Title = "changed"
	if err := st.Save(data); err != nil {
		t.Fatalf("changed Save: %v", err)
	}
	if data.ID != id {
		t.Errorf("changed save derived id %q, want kept id %q", data.ID, id)
	}
}

// equalConv compares two conversation entries field by field.
func equalConv(a, b Conversation) bool {
	if a.Role != b.Role || a.Content != b.Content || a.ToolCallID != b.ToolCallID ||
		len(a.ToolCalls) != len(b.ToolCalls) {
		return false
	}
	for i := range a.ToolCalls {
		if a.ToolCalls[i] != b.ToolCalls[i] {
			return false
		}
	}
	return true
}

// equalData compares every field of two SessionData, treating nil and empty
// pastes entries as equal.
func equalData(a, b *SessionData) bool {
	if a.Version != b.Version || a.ID != b.ID || a.Created != b.Created || a.Updated != b.Updated ||
		a.StartDir != b.StartDir || a.Model != b.Model || a.Effort != b.Effort || a.Mode != b.Mode ||
		a.InTokens != b.InTokens || a.OutTokens != b.OutTokens || a.TotalTokens != b.TotalTokens ||
		a.LastContext != b.LastContext || a.Title != b.Title || a.Turn != b.Turn {
		return false
	}
	if len(a.Conversation) != len(b.Conversation) {
		return false
	}
	for i := range a.Conversation {
		if !equalConv(a.Conversation[i], b.Conversation[i]) {
			return false
		}
	}
	if len(a.History) != len(b.History) {
		return false
	}
	for i := range a.History {
		if a.History[i] != b.History[i] {
			return false
		}
	}
	if len(a.Queue) != len(b.Queue) {
		return false
	}
	for i := range a.Queue {
		if a.Queue[i] != b.Queue[i] {
			return false
		}
	}
	if a.Pastes.N != b.Pastes.N || len(a.Pastes.Entries) != len(b.Pastes.Entries) {
		return false
	}
	for k, v := range a.Pastes.Entries {
		if b.Pastes.Entries[k] != v {
			return false
		}
	}
	return true
}

func TestSaveAtomicNoTempLeftovers(t *testing.T) {
	stubNow(t, time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC))
	root := t.TempDir()
	st, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	data := fixture()
	for i := 0; i < 2; i++ {
		if err := st.Save(data); err != nil {
			t.Fatalf("Save #%d: %v", i+1, err)
		}
		dir := filepath.Join(root, ".yipyip", "sessions")
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(ents) != 1 {
			t.Fatalf("after Save #%d the sessions dir has %d entries (%v), want exactly 1", i+1, len(ents), ents)
		}
		for _, e := range ents {
			if !strings.HasSuffix(e.Name(), ".json") {
				t.Errorf("leftover non-json entry %q", e.Name())
			}
			if strings.HasPrefix(e.Name(), ".") {
				t.Errorf("leftover temp entry %q", e.Name())
			}
		}
	}
}

func TestListNewestFirst(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Three saves with different updated stamps, set explicitly through the
	// package-level now stub (no sleeping): now advances between saves.
	base := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	wantOrder := []string{}
	titles := []string{"oldest", "middle", "newest"}
	for i, title := range titles {
		ts := base.Add(time.Duration(i) * time.Hour)
		stubNow(t, ts)
		d := fixture()
		d.Title = title
		if err := st.Save(d); err != nil {
			t.Fatalf("Save %q: %v", title, err)
		}
		wantOrder = append(wantOrder, d.ID)
	}
	list, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List returned %d sessions, want 3", len(list))
	}
	for i, d := range list {
		if d.ID != wantOrder[2-i] {
			t.Errorf("list[%d] = %s (%q), want %s (newest first)", i, d.ID, d.Title, wantOrder[2-i])
		}
	}
}

func TestDelete(t *testing.T) {
	stubNow(t, time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC))
	root := t.TempDir()
	st, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	d := fixture()
	if err := st.Save(d); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := st.List(); err != nil {
		t.Fatalf("List before delete: %v", err)
	}
	if err := st.Delete(d.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".yipyip", "sessions", d.ID+".json")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("session file still present after Delete (stat err = %v)", err)
	}
	// Deleting the removed id again errors, naming the file.
	err = st.Delete(d.ID)
	if err == nil {
		t.Fatal("Delete of a missing id succeeded, want an error")
	}
	if !strings.Contains(err.Error(), filepath.Join(root, ".yipyip", "sessions", d.ID+".json")) {
		t.Errorf("Delete of a missing id named no file path: %v", err)
	}
}

func TestLoadVersionMismatch(t *testing.T) {
	root := t.TempDir()
	st, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".yipyip", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, body string }{
		{"version 0", `{"version":0,"id":"aaaaaaaaaaaa"}`},
		{"version 2", `{"version":2,"id":"aaaaaaaaaaaa"}`},
		{"version 99", `{"version":99,"id":"aaaaaaaaaaaa"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, "aaaaaaaaaaaa.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := st.Load("aaaaaaaaaaaa")
			if err == nil {
				t.Fatalf("Load of %s succeeded, want an error", tc.body)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error names no file path: %v", err)
			}
		})
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	root := t.TempDir()
	st, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".yipyip", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "bbbbbbbbbbbb.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Load("bbbbbbbbbbbb"); err == nil {
		t.Fatal("Load of invalid JSON succeeded, want an error")
	} else if !strings.Contains(err.Error(), path) {
		t.Errorf("invalid JSON error names no file path: %v", err)
	}
	// A missing file errors too, naming the file.
	missing := filepath.Join(dir, "cccccccccccc.json")
	_, err = st.Load("cccccccccccc")
	if err == nil {
		t.Fatal("Load of a missing file succeeded, want an error")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("missing file error names no file path: %v", err)
	}
}

func TestLoadUnknownFieldsIgnored(t *testing.T) {
	root := t.TempDir()
	st, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".yipyip", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"version":1,"id":"dddddddddddd","created":"2026-07-14T10:00:00Z",
		"updated":"2026-07-14T10:05:00Z","startdir":"/x","model":"m","effort":"medium","mode":"manual",
		"in_tokens":1,"out_tokens":2,"total_tokens":3,"last_context":1,
		"conversation":[{"role":"user","content":"hi"}],"history":["hi"],
		"pastes":{"n":0,"entries":{}},"queue":[],"title":"hi",
		"turn":{"active":"","started":"","interrupted":false},
		"future_field":{"nested":[1,2]},"another":"x"}`
	if err := os.WriteFile(filepath.Join(dir, "dddddddddddd.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := st.Load("dddddddddddd")
	if err != nil {
		t.Fatalf("Load with unknown fields: %v", err)
	}
	if data.ID != "dddddddddddd" || data.Title != "hi" || len(data.Conversation) != 1 || data.Conversation[0].Content != "hi" {
		t.Errorf("loaded data = %+v", data)
	}
}

func TestSymlinkedRefused(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	for _, d := range []string{a, b} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(b, filepath.Join(a, ".yipyip")); err != nil {
		t.Fatal(err)
	}
	st, err := NewStore(a)
	if err != nil {
		t.Fatal(err)
	}
	bFiles := func() []string {
		ents, _ := os.ReadDir(b)
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		return names
	}
	before := bFiles()
	data := fixture()
	data.StartDir = a
	for name, fn := range map[string]func() error{
		"Save":   func() error { return st.Save(data) },
		"Load":   func() error { _, err := st.Load("aaaaaaaaaaaa"); return err },
		"List":   func() error { _, err := st.List(); return err },
		"Delete": func() error { return st.Delete("aaaaaaaaaaaa") },
	} {
		if err := fn(); err == nil {
			t.Errorf("%s on a symlinked .yipyip succeeded, want an error", name)
		}
	}
	if got := bFiles(); !equalStrings(before, got) {
		t.Errorf("something was created under the symlink target: %v", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestNoSecretInFile(t *testing.T) {
	stubNow(t, time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC))
	root := t.TempDir()
	st, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	data := fixture()
	// A tool result containing the secret IS stored (R33: un-redacted, as
	// sent to the model) — but no API key or config material is: the struct
	// has no field for one.
	data.Conversation = append(data.Conversation, Conversation{
		Role: "tool", ToolCallID: "call_1", Content: "cat config.json → SECRET123",
	})
	if err := st.Save(data); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".yipyip", "sessions", data.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("SECRET123")) {
		t.Error("the tool result secret is missing from the raw file (R33: stored un-redacted)")
	}
	// The JSON top-level keys are exactly the R29 set: no key, no config.
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"version": false, "id": false, "created": false, "updated": false,
		"startdir": false, "model": false, "effort": false, "mode": false,
		"in_tokens": false, "out_tokens": false, "total_tokens": false,
		"last_context": false, "conversation": false, "history": false,
		"pastes": false, "queue": false, "title": false, "turn": false,
	}
	if len(m) != len(want) {
		t.Errorf("top-level JSON keys = %d, want exactly the %d R29 keys", len(m), len(want))
	}
	for k := range m {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected top-level JSON key %q (no API key or config material may be stored)", k)
		}
		want[k] = true
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("missing top-level JSON key %q", k)
		}
	}
	for _, secret := range []string{"api_key", "apiKey", "sk-", "Authorization", "{env:}"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Errorf("raw session file contains %q", secret)
		}
	}
}

func TestTitle(t *testing.T) {
	// 61 runes total: the first 60 survive.
	sixtyOne := strings.Repeat("x", 60) + "y"
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"hello", "hello"},
		{"line one\nline two", "line one line two"},
		{"a\r\nb\rc", "a b c"},
		{sixtyOne, strings.Repeat("x", 60)},
		{"日本語\nのタイトル", "日本語 のタイトル"},
		{"  spaces  ", "  spaces  "}, // only newlines collapse, spaces stay
	} {
		if got := Title(tc.in); got != tc.want {
			t.Errorf("Title(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
