package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"yipyip/internal/agent"
)

// Records of the headless --json trace. Type is the first field so that it is
// written first.

type textRecord struct {
	Type  string `json:"type"`
	Agent string `json:"agent"`
	Text  string `json:"text"`
}

type toolCallRecord struct {
	Type  string `json:"type"`
	Agent string `json:"agent"`
	Name  string `json:"name"`
	Args  string `json:"args"`
}

type toolResultRecord struct {
	Type   string `json:"type"`
	Agent  string `json:"agent"`
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Output string `json:"output"`
}

type subagentRecord struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	State string `json:"state"`
	Task  string `json:"task"`
	Model string `json:"model"`
}

type questionRecord struct {
	Type     string   `json:"type"`
	Agent    string   `json:"agent"`
	Question string   `json:"question"`
	Options  []string `json:"options"`
	Multiple bool     `json:"multiple"`
}

type approvalRecordT struct {
	Type     string `json:"type"`
	Agent    string `json:"agent"`
	Tool     string `json:"tool"`
	Summary  string `json:"summary"`
	Decision string `json:"decision"`
}

type retryRecord struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type errorRecord struct {
	Type  string `json:"type"`
	Agent string `json:"agent"`
	Text  string `json:"text"`
}

type tokensRecord struct {
	In  int `json:"in"`
	Out int `json:"out"`
}

// resultRecord is the last record of a headless run.
type resultRecord struct {
	Type         string         `json:"type"`
	SessionID    string         `json:"session_id"`
	ExitCode     int            `json:"exit_code"`
	Reason       string         `json:"reason"`
	FinalText    string         `json:"final_text"`
	ModelCalls   int            `json:"model_calls"`
	Subagents    int            `json:"subagents"`
	Tokens       tokensRecord   `json:"tokens"`
	DurationMS   int64          `json:"duration_ms"`
	Tools        map[string]int `json:"tools"`
	FilesChanged []string       `json:"files_changed"`
}

// mapper turns agent events into trace records.
type mapper struct {
	redact   func(string) string                  // every string field passes it
	truncate int                                  // max runes of tool_result output; 0 = untruncated
	sub      func(id string) (task, model string) // subagent lookup; may be nil
}

func (m mapper) red(s string) string {
	if m.redact == nil {
		return s
	}
	return m.redact(s)
}

// records maps one event to zero or more records.
func (m mapper) records(ev agent.Event) []any {
	switch ev.Kind {
	case agent.EvText:
		return []any{textRecord{"text", m.red(ev.AgentID), m.red(ev.Text)}}
	case agent.EvToolCall:
		return []any{toolCallRecord{"tool_call", m.red(ev.AgentID), m.red(ev.Tool), m.red(ev.Args)}}
	case agent.EvToolResult:
		out := m.red(ev.Text)
		if m.truncate > 0 {
			if r := []rune(out); len(r) > m.truncate {
				out = string(r[:m.truncate]) + "…[truncated]"
			}
		}
		return []any{toolResultRecord{"tool_result", m.red(ev.AgentID), m.red(ev.Tool), !ev.IsError, out}}
	case agent.EvSubagent:
		id, state := ev.AgentID, ""
		if i := strings.IndexByte(ev.Text, ' '); i >= 0 {
			state = ev.Text[i+1:]
		}
		var task, model string
		if m.sub != nil {
			task, model = m.sub(id)
		}
		return []any{subagentRecord{"subagent", m.red(id), m.red(state), m.red(task), m.red(model)}}
	case agent.EvQuestion:
		if ev.Question == nil {
			return nil
		}
		q := ev.Question
		opts := make([]string, 0, len(q.Options))
		for _, o := range q.Options {
			opts = append(opts, m.red(o))
		}
		agentID := q.AgentID
		if agentID == "" {
			agentID = ev.AgentID
		}
		return []any{questionRecord{"question", m.red(agentID), m.red(q.Question), opts, q.Multiple}}
	case agent.EvRetry:
		return []any{retryRecord{"retry", m.red(ev.Text)}}
	case agent.EvError:
		return []any{errorRecord{"error", m.red(ev.AgentID), m.red(ev.Text)}}
	}
	return nil
}

// approvalRecord builds an approval record (not redacted; the runner redacts).
func approvalRecord(agentID, tool, summary string, approved bool) any {
	d := "denied"
	if approved {
		d = "approved"
	}
	return approvalRecordT{"approval", agentID, tool, summary, d}
}

// encodeLine writes rec as one JSON line without HTML escaping.
func encodeLine(w io.Writer, rec any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rec); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}
