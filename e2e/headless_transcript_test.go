package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

// transcriptLines reads path and checks that every line is a JSON object;
// it returns the decoded lines.
func transcriptLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading transcript: %v", err)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("transcript line is not JSON: %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func toolOutputs(recs []map[string]any, name string) []string {
	var out []string
	for _, r := range recs {
		if r["type"] == "tool_result" && r["name"] == name {
			s, _ := r["output"].(string)
			out = append(out, s)
		}
	}
	return out
}

func TestHeadlessTranscript(t *testing.T) {
	big := strings.Repeat("x", 3000)
	srv := providertest.New(
		providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "c1", Name: "read_file", Arguments: `{"path":"big.txt"}`}}},
		providertest.Response{Text: "done"},
	)
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--json", "--transcript", "out/t.jsonl")
	if res.code != 0 {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	checkNoSecret(t, res)
	path := filepath.Join(dir, "out", "t.jsonl")
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("transcript missing: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("transcript mode %v, want 0600", st.Mode().Perm())
	}
	trecs := transcriptLines(t, path)
	r := lastResult(t, trecs)
	if r["final_text"] != "done" || r["exit_code"] != float64(0) {
		t.Errorf("transcript result %v", r)
	}
	touts := toolOutputs(trecs, "read_file")
	if len(touts) != 1 || !strings.Contains(touts[0], big) || strings.Contains(touts[0], "…[truncated]") {
		t.Errorf("transcript tool output not complete: %d outputs", len(touts))
	}
	jouts := toolOutputs(jsonLines(t, res.stdout), "read_file")
	if len(jouts) != 1 || !strings.HasSuffix(jouts[0], "…[truncated]") || strings.Contains(jouts[0], big) {
		t.Errorf("--json tool output not truncated: %q", jouts)
	}

	// Without --json the transcript is written too.
	srv2 := providertest.New(providertest.Response{Text: "plain"})
	defer srv2.Close()
	home2, dir2 := headlessDirs(t, srv2)
	res = runYip(t, home2, dir2, "", headlessEnv, "-p", "go", "--transcript", "t.jsonl")
	if res.code != 0 || res.stdout != "plain\n" {
		t.Fatalf("plain: code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	if r := lastResult(t, transcriptLines(t, filepath.Join(dir2, "t.jsonl"))); r["final_text"] != "plain" {
		t.Errorf("plain transcript result %v", r)
	}
}

func TestHeadlessTranscriptOutside(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "never"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(outside, "x.jsonl")
	cases := []struct{ arg, created string }{
		{"../t.jsonl", filepath.Join(filepath.Dir(dir), "t.jsonl")},
		{abs, abs},
		{"link/t.jsonl", filepath.Join(outside, "t.jsonl")},
	}
	for _, c := range cases {
		res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--transcript", c.arg)
		if res.code != 2 {
			t.Errorf("%s: code %d stdout %q stderr %q, want 2", c.arg, res.code, res.stdout, res.stderr)
		}
		if !strings.Contains(res.stderr, "yip: transcript path outside the start directory") {
			t.Errorf("%s: stderr %q", c.arg, res.stderr)
		}
		if _, err := os.Lstat(c.created); err == nil {
			t.Errorf("%s: file %s was created", c.arg, c.created)
		}
		checkNoSecret(t, res)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("provider saw %d requests, want 0", n)
	}
}

func TestHeadlessSecret(t *testing.T) {
	for _, jsonMode := range []bool{true, false} {
		srv := providertest.New(
			providertest.Response{Text: "key SECRET123", ToolCalls: []providertest.ToolCall{{ID: "c1", Name: "bash", Arguments: `{"command":"echo SECRET123"}`}}},
			providertest.Response{Text: "final SECRET123"},
		)
		home, dir := headlessDirs(t, srv)
		args := []string{"-p", "go", "--transcript", "t.jsonl"}
		if jsonMode {
			args = append(args, "--json")
		}
		res := runYip(t, home, dir, "", headlessEnv, args...)
		srv.Close()
		if res.code != 0 {
			t.Fatalf("json=%v: code %d stdout %q stderr %q", jsonMode, res.code, res.stdout, res.stderr)
		}
		checkNoSecret(t, res)
		data, err := os.ReadFile(filepath.Join(dir, "t.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "SECRET123") {
			t.Errorf("json=%v: secret in transcript: %q", jsonMode, data)
		}
		trecs := transcriptLines(t, filepath.Join(dir, "t.jsonl"))
		lastResult(t, trecs)
		if n := typesOf(trecs)["tool_call"]; n != 1 {
			t.Errorf("json=%v: %d tool_call records, want 1", jsonMode, n)
		}
	}
}
