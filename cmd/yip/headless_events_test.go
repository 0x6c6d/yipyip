package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"yipyip/internal/agent"
)

func allRecords(m mapper) []any {
	evs := []agent.Event{
		{Kind: agent.EvText, AgentID: "main", Text: "hi SECRET123"},
		{Kind: agent.EvToolCall, AgentID: "main", Tool: "bash", Args: `{"c":"SECRET123"}`},
		{Kind: agent.EvToolResult, AgentID: "main", Tool: "bash", Text: "out SECRET123"},
		{Kind: agent.EvSubagent, AgentID: "sub1", Text: "sub1 running"},
		{Kind: agent.EvQuestion, AgentID: "main", Question: &agent.QuestionRequest{AgentID: "main", Question: "q SECRET123", Options: []string{"a SECRET123"}}},
		{Kind: agent.EvRetry, Text: "retrying SECRET123"},
		{Kind: agent.EvError, AgentID: "main", Text: "bad SECRET123"},
	}
	var recs []any
	for _, ev := range evs {
		recs = append(recs, m.records(ev)...)
	}
	recs = append(recs, approvalRecord("main", "bash", "run SECRET123", true))
	recs = append(recs, resultRecord{Type: "result", Reason: "SECRET123", FinalText: "SECRET123", Tools: map[string]int{}, FilesChanged: []string{}})
	return recs
}

func TestRecordTypeFirst(t *testing.T) {
	m := mapper{sub: func(string) (string, string) { return "t", "m" }}
	kinds := map[string]bool{}
	for _, rec := range allRecords(m) {
		var b bytes.Buffer
		if err := encodeLine(&b, rec); err != nil {
			t.Fatal(err)
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(b.Bytes(), &probe); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(b.String(), `{"type":"`+probe.Type+`"`) {
			t.Errorf("line %q does not start with type", b.String())
		}
		kinds[probe.Type] = true
	}
	if len(kinds) != 9 {
		t.Errorf("kinds = %v, want 9", kinds)
	}
}

func TestToolResultTruncated(t *testing.T) {
	ev := agent.Event{Kind: agent.EvToolResult, Tool: "bash", Text: strings.Repeat("é", 5000)}
	got := mapper{truncate: 2000}.records(ev)[0].(toolResultRecord)
	if got.Output != strings.Repeat("é", 2000)+"…[truncated]" {
		t.Errorf("truncated output wrong, len %d", len([]rune(got.Output)))
	}
	got = mapper{}.records(ev)[0].(toolResultRecord)
	if len([]rune(got.Output)) != 5000 {
		t.Errorf("untruncated len = %d", len([]rune(got.Output)))
	}
	if !got.OK {
		t.Error("ok should be true")
	}
}

func TestRecordRedacted(t *testing.T) {
	m := mapper{redact: func(s string) string { return strings.ReplaceAll(s, "SECRET123", "[redacted]") }}
	for _, rec := range allRecords(m) {
		// the runner redacts approval/result records; apply the same here
		if a, ok := rec.(approvalRecordT); ok {
			a.Summary = m.redact(a.Summary)
			rec = a
		}
		if r, ok := rec.(resultRecord); ok {
			r.Reason, r.FinalText = m.redact(r.Reason), m.redact(r.FinalText)
			rec = r
		}
		var b bytes.Buffer
		if err := encodeLine(&b, rec); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(b.String(), "SECRET123") {
			t.Errorf("secret in %q", b.String())
		}
	}
}

func TestResultFields(t *testing.T) {
	var b bytes.Buffer
	if err := encodeLine(&b, resultRecord{Type: "result", Tools: map[string]int{}, FilesChanged: []string{}}); err != nil {
		t.Fatal(err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b.Bytes(), &obj); err != nil {
		t.Fatal(err)
	}
	want := "type session_id exit_code reason final_text model_calls subagents tokens duration_ms tools files_changed"
	if len(obj) != 11 {
		t.Errorf("got %d keys", len(obj))
	}
	for _, k := range strings.Fields(want) {
		if _, ok := obj[k]; !ok {
			t.Errorf("missing key %s", k)
		}
	}
	if string(obj["files_changed"]) != "[]" {
		t.Errorf("files_changed = %s", obj["files_changed"])
	}
}
