package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

func TestHeadlessDir(t *testing.T) {
	srv := providertest.New(
		providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{"path":"a.txt","content":"x"}`}}},
		providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "c2", Name: "write_file", Arguments: `{"path":"../escape.txt","content":"x"}`}}},
		providertest.Response{Text: "done"},
	)
	defer srv.Close()
	home, work := headlessDirs(t, srv)
	cwd := filepath.Join(filepath.Dir(work), "cwd")
	if err := os.Mkdir(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "yipyip.json"), []byte(`{"default_model":"fake/m2"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runYip(t, home, cwd, "", headlessEnv, "-p", "go", "--json", "--dir", work)
	if res.code != 0 {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(work, "a.txt")); err != nil {
		t.Errorf("work/a.txt: %v", err)
	}
	for _, p := range []string{filepath.Join(filepath.Dir(work), "escape.txt"), filepath.Join(cwd, "escape.txt"), filepath.Join(cwd, "a.txt")} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s exists", p)
		}
	}
	var results []map[string]any
	for _, r := range jsonLines(t, res.stdout) {
		if r["type"] == "tool_result" {
			results = append(results, r)
		}
	}
	if len(results) != 2 || results[0]["ok"] != true || results[1]["ok"] != false {
		t.Fatalf("tool_result records = %v", results)
	}
	reqs := srv.Requests()
	if len(reqs) == 0 || reqs[0].Model != "m2" {
		t.Fatalf("requests = %d, first model %q, want m2", len(reqs), func() string {
			if len(reqs) == 0 {
				return ""
			}
			return reqs[0].Model
		}())
	}
	checkNoSecret(t, res)
}

func TestHeadlessDirMissing(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--dir", "/nonexistent/x")
	if res.code != 1 {
		t.Fatalf("code %d, want 1; stderr %q", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "not a directory") {
		t.Errorf("stderr = %q", res.stderr)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("requests = %d, want 0", n)
	}
	// A file is not a directory either.
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = runYip(t, home, dir, "", headlessEnv, "-p", "go", "--dir", file)
	if res.code != 1 || len(srv.Requests()) != 0 {
		t.Fatalf("file --dir: code %d requests %d stderr %q", res.code, len(srv.Requests()), res.stderr)
	}
}

func TestHeadlessOverride(t *testing.T) {
	srv := providertest.New(
		providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "c1", Name: "bash", Arguments: `{"command":"echo hi > ran.txt"}`}}},
		providertest.Response{Text: "ok"},
	)
	defer srv.Close()
	home, dir := cliDirs(t)
	var cfg map[string]any
	if err := json.Unmarshal([]byte(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 100000)), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["effort"], cfg["mode"] = "high", "manual"
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := writeCLIGlobalConfig(t, home, string(b))
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	checkUnchanged := func(label string) {
		t.Helper()
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, b) || !st.ModTime().Equal(before.ModTime()) {
			t.Errorf("%s: config.json changed", label)
		}
	}

	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--json", "--model", "fake/m2", "--effort", "low")
	if res.code != 0 {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	reqs := srv.Requests()
	if len(reqs) != 2 || reqs[0].Model != "m2" {
		t.Fatalf("requests = %d, want 2 with model m2", len(reqs))
	}
	if aps := approvalRecords(jsonLines(t, res.stdout)); len(aps) != 0 {
		t.Errorf("approval records = %v, want none", aps)
	}
	if !ranExists(dir) {
		t.Errorf("bash did not run")
	}
	checkUnchanged("override run")
	checkNoSecret(t, res)

	res = runYip(t, home, dir, "", headlessEnv, "-p", "go", "--model", "nope/x")
	if res.code != 2 {
		t.Fatalf("--model nope/x: code %d stderr %q", res.code, res.stderr)
	}
	checkUnchanged("--model nope/x")
	res = runYip(t, home, dir, "", headlessEnv, "-p", "go", "--effort", "extreme")
	if res.code != 2 {
		t.Fatalf("--effort extreme: code %d stderr %q", res.code, res.stderr)
	}
	checkUnchanged("--effort extreme")
	if n := len(srv.Requests()); n != 2 {
		t.Errorf("requests = %d after the failed runs, want 2", n)
	}
	checkNoSecret(t, res)
}

// systemHas reports whether a system message of req contains s.
func systemHas(req providertest.Request, s string) bool {
	for _, m := range req.Messages {
		if m.Role == "system" && strings.Contains(m.Content, s) {
			return true
		}
	}
	return false
}

func TestHeadlessSystemAppend(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "one"}, providertest.Response{Text: "two"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--no-save", "--system-append", "APPEND-XYZ-9")
	if res.code != 0 {
		t.Fatalf("run 1: code %d stderr %q", res.code, res.stderr)
	}
	res = runYip(t, home, dir, "", headlessEnv, "-p", "go", "--no-save")
	if res.code != 0 {
		t.Fatalf("run 2: code %d stderr %q", res.code, res.stderr)
	}
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	if !systemHas(reqs[0], "APPEND-XYZ-9") {
		t.Errorf("first request has no system message with the appended text")
	}
	if systemHas(reqs[1], "APPEND-XYZ-9") {
		t.Errorf("second request still has the appended text")
	}
}
