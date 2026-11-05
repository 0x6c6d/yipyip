package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"yipyip/internal/provider/providertest"
	"yipyip/internal/session"
)

// resultSessionID returns the session_id of the result record of res.
func resultSessionID(t *testing.T, res cliResult) string {
	t.Helper()
	r := lastResult(t, jsonLines(t, res.stdout))
	id, ok := r["session_id"].(string)
	if !ok {
		t.Fatalf("session_id missing in %v", r)
	}
	return id
}

// sessionsSnapshot maps each file name of <dir>/.yipyip/sessions to its
// bytes; nil when the directory is absent.
func sessionsSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	sdir := filepath.Join(dir, ".yipyip", "sessions")
	ents, err := os.ReadDir(sdir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(sdir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = string(b)
	}
	return out
}

func TestHeadlessResume(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "one"}, providertest.Response{Text: "two"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "first", "--json")
	if res.code != 0 {
		t.Fatalf("run 1: code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	id := resultSessionID(t, res)
	if !hexID.MatchString(id) {
		t.Fatalf("session_id = %q, want 12 hex chars", id)
	}
	if _, err := os.Stat(filepath.Join(dir, ".yipyip", "sessions", id+".json")); err != nil {
		t.Fatalf("session file: %v", err)
	}

	res = runYip(t, home, dir, "", headlessEnv, "--resume", id, "-p", "second", "--json")
	if res.code != 0 {
		t.Fatalf("run 2: code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	var got []string
	for _, m := range reqs[1].Messages {
		if m.Role == "user" || m.Role == "assistant" {
			got = append(got, m.Role+":"+m.Content)
		}
	}
	want := []string{"user:first", "assistant:one", "user:second"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("run 2 messages = %q, want %q", got, want)
	}
	if id2 := resultSessionID(t, res); id2 != id {
		t.Errorf("run 2 session_id = %q, want %q", id2, id)
	}
	if ids := sessionIDs(t, dir); len(ids) != 1 || ids[0] != id {
		t.Errorf("session files = %v, want [%s]", ids, id)
	}
	checkNoSecret(t, res)

	// Unknown id: exit 1 before any model call.
	res = runYip(t, home, dir, "", headlessEnv, "--resume", "abcdefabcdef", "-p", "x")
	if res.code != 1 {
		t.Fatalf("unknown: code %d stderr %q", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "unknown session abcdefabcdef") {
		t.Errorf("unknown: stderr = %q", res.stderr)
	}
	if strings.Contains(res.stderr, ".yipyip") {
		t.Errorf("unknown: stderr names a path: %q", res.stderr)
	}
	if n := len(srv.Requests()); n != 2 {
		t.Errorf("unknown: requests = %d, want still 2", n)
	}
}

func TestHeadlessNoSave(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "one"}, providertest.Response{Text: "two"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "first", "--json")
	if res.code != 0 {
		t.Fatalf("run 1: code %d stderr %q", res.code, res.stderr)
	}
	before := sessionsSnapshot(t, dir)
	if len(before) != 1 {
		t.Fatalf("sessions before = %d, want 1", len(before))
	}
	res = runYip(t, home, dir, "", headlessEnv, "-p", "again", "--no-save", "--json")
	if res.code != 0 {
		t.Fatalf("no-save: code %d stderr %q", res.code, res.stderr)
	}
	if id := resultSessionID(t, res); id != "" {
		t.Errorf("no-save session_id = %q, want empty", id)
	}
	if after := sessionsSnapshot(t, dir); !reflect.DeepEqual(before, after) {
		t.Errorf("sessions changed by --no-save")
	}

	srv2 := providertest.New(providertest.Response{Text: "fresh"})
	defer srv2.Close()
	home2, dir2 := headlessDirs(t, srv2)
	res = runYip(t, home2, dir2, "", headlessEnv, "-p", "hi", "--no-save", "--json")
	if res.code != 0 {
		t.Fatalf("fresh no-save: code %d stderr %q", res.code, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir2, ".yipyip", "sessions")); !os.IsNotExist(err) {
		t.Errorf("fresh no-save: sessions dir exists (err %v)", err)
	}
}

func TestHeadlessSaveOnQuestion(t *testing.T) {
	srv := providertest.New(
		headlessQuestion("q1", "Pick?", []string{"A", "B"}),
		providertest.Response{Text: "never"},
	)
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--json")
	if res.code != 3 {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	id := resultSessionID(t, res)
	if !hexID.MatchString(id) {
		t.Fatalf("session_id = %q, want 12 hex chars", id)
	}
	store, err := session.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := store.Load(id)
	if err != nil {
		t.Fatalf("load %s: %v", id, err)
	}
	if len(data.Conversation) == 0 {
		t.Errorf("saved conversation is empty")
	}
}

func TestHeadlessPrune(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	sdir := filepath.Join(dir, ".yipyip", "sessions")
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		d := session.NewSessionData()
		d.ID = fmt.Sprintf("%012x", 0xb00+i)
		d.StartDir = dir
		d.Mode = "auto"
		stamp := base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		d.Created, d.Updated = stamp, stamp
		d.Conversation = []session.Conversation{{Role: "user", Content: fmt.Sprintf("hello %d", i)}}
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sdir, d.ID+".json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--json")
	if res.code != 0 {
		t.Fatalf("code %d stderr %q", res.code, res.stderr)
	}
	id := resultSessionID(t, res)
	ids := sessionIDs(t, dir)
	if len(ids) != 25 {
		t.Fatalf("session files = %d, want 25", len(ids))
	}
	found := false
	for _, x := range ids {
		if x == id {
			found = true
		}
	}
	if !found {
		t.Errorf("new session %q not among %v", id, ids)
	}
}
