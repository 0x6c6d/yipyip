package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

// jsonLines checks that every stdout line is a JSON object whose first key
// is "type" and returns the decoded lines.
func jsonLines(t *testing.T, stdout string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		if !strings.HasPrefix(line, "{\"type\":") {
			t.Fatalf("stdout line does not start with the type key: %q", line)
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("stdout line is not JSON: %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func typesOf(recs []map[string]any) map[string]int {
	n := map[string]int{}
	for _, r := range recs {
		n[r["type"].(string)]++
	}
	return n
}

func lastResult(t *testing.T, recs []map[string]any) map[string]any {
	t.Helper()
	if len(recs) == 0 {
		t.Fatal("no records")
	}
	last := recs[len(recs)-1]
	if last["type"] != "result" {
		t.Fatalf("last record %v is not a result", last)
	}
	if n := typesOf(recs)["result"]; n != 1 {
		t.Fatalf("%d result records, want 1", n)
	}
	return last
}

func TestHeadlessJSON(t *testing.T) {
	srv := providertest.New(
		providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{"path":"a.txt","content":"x"}`}}},
		providertest.Response{Text: "done"},
	)
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--json")
	if res.code != 0 {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	if res.stderr != "" {
		t.Errorf("stderr not empty with --json: %q", res.stderr)
	}
	recs := jsonLines(t, res.stdout)
	types := typesOf(recs)
	if types["tool_call"] == 0 || types["tool_result"] == 0 {
		t.Errorf("trace lacks tool_call/tool_result: %v", types)
	}
	r := lastResult(t, recs)
	for _, k := range []string{"type", "session_id", "exit_code", "reason", "final_text", "model_calls", "subagents", "tokens", "duration_ms", "tools", "files_changed"} {
		if _, ok := r[k]; !ok {
			t.Errorf("result lacks key %q: %v", k, r)
		}
	}
	if r["reason"] != "done" || r["final_text"] != "done" || r["exit_code"] != float64(0) || r["model_calls"] != float64(2) {
		t.Errorf("result %v", r)
	}
	if !reflect.DeepEqual(r["files_changed"], []any{"a.txt"}) {
		t.Errorf("files_changed %v, want [a.txt]", r["files_changed"])
	}
	if !reflect.DeepEqual(r["tools"], map[string]any{"write_file": float64(1)}) {
		t.Errorf("tools %v, want {write_file:1}", r["tools"])
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Errorf("a.txt not written: %v", err)
	}
	checkNoSecret(t, res)
}

// TestHeadlessJSONRedaction: a secret in a written file's path and in the
// final text never reaches the --json stdout.
func TestHeadlessJSONRedaction(t *testing.T) {
	srv := providertest.New(
		providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{"path":"SECRET123.txt","content":"x"}`}}},
		providertest.Response{Text: "done SECRET123"},
	)
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--json")
	if res.code != 0 {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	recs := jsonLines(t, res.stdout)
	r := lastResult(t, recs)
	if fc, _ := r["files_changed"].([]any); len(fc) != 1 {
		t.Errorf("files_changed %v, want one entry", r["files_changed"])
	}
	if ft, _ := r["final_text"].(string); !strings.HasPrefix(ft, "done ") {
		t.Errorf("final_text %q", ft)
	}
	checkNoSecret(t, res)
}

func TestHeadlessSubagent(t *testing.T) {
	srv := providertest.NewFunc(func(n int, req providertest.Request) providertest.Response {
		firstUser, last := "", providertest.Message{}
		for _, m := range req.Messages {
			if m.Role == "user" && firstUser == "" {
				firstUser = m.Content
			}
			last = m
		}
		if firstUser == "sub" {
			return providertest.Response{Text: "sub done"}
		}
		switch {
		case last.Role == "user":
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "s1", Name: "spawn_agent", Arguments: `{"task":"sub"}`}}}
		case last.Role == "tool" && strings.HasPrefix(last.Content, "subagent ") && strings.HasSuffix(last.Content, " started"):
			id := strings.TrimSuffix(strings.TrimPrefix(last.Content, "subagent "), " started")
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "w1", Name: "wait_agent", Arguments: `{"id":"` + id + `"}`}}}
		default:
			return providertest.Response{Text: "main done"}
		}
	})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)

	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--json")
	if res.code != 0 {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	recs := jsonLines(t, res.stdout)
	subID := ""
	for _, rec := range recs {
		if rec["type"] == "tool_result" && rec["name"] == "spawn_agent" {
			out, _ := rec["output"].(string)
			subID = strings.TrimSuffix(strings.TrimPrefix(out, "subagent "), " started")
		}
	}
	if subID == "" {
		t.Fatalf("no spawn_agent result in %q", res.stdout)
	}
	found := false
	for _, rec := range recs {
		if rec["type"] == "subagent" && rec["id"] == subID {
			found = true
		}
	}
	if !found {
		t.Errorf("no subagent record with id %q in %q", subID, res.stdout)
	}
	r := lastResult(t, recs)
	if r["subagents"] != float64(1) || r["final_text"] != "main done" {
		t.Errorf("result %v", r)
	}
	checkNoSecret(t, res)

	res = runYip(t, home, dir, "", headlessEnv, "-p", "go")
	if res.code != 0 || res.stdout != "main done\n" {
		t.Fatalf("plain: code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "subagent ") {
		t.Errorf("stderr %q lacks a subagent line", res.stderr)
	}
	checkNoSecret(t, res)
}
