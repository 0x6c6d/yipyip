package providertest

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const reqBody = `{"model":"m1","stream":true,"reasoning_effort":"low",
 "messages":[{"role":"user","content":"hi"},
  {"role":"assistant","content":null,"tool_calls":[{"id":"c0","type":"function","function":{"name":"glob","arguments":"{}"}}]},
  {"role":"tool","tool_call_id":"c0","content":"res"}],
 "tools":[{"type":"function","function":{"name":"read_file"}},{"type":"function","function":{"name":"bash"}}]}`

func post(t *testing.T, base, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", base+"/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer SECRET123")
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// events returns the data payloads and any read error.
func events(resp *http.Response) ([]string, error) {
	var out []string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if p, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			out = append(out, p)
		}
	}
	return out, sc.Err()
}

type chunk struct {
	Choices []struct {
		Delta struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		Prompt, Completion, Total int
	} `json:"-"`
}

func parse(t *testing.T, data []string) []chunk {
	t.Helper()
	var out []chunk
	for _, d := range data {
		if d == "[DONE]" {
			continue
		}
		var c chunk
		if err := json.Unmarshal([]byte(d), &c); err != nil {
			t.Fatalf("bad chunk %q: %v", d, err)
		}
		out = append(out, c)
	}
	return out
}

func TestFakeTextStream(t *testing.T) {
	s := New(Response{Text: "héllo wörld!", Usage: &Usage{Prompt: 7, Completion: 3}})
	defer s.Close()
	resp := post(t, s.URL, reqBody)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status=%d type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	data, err := events(resp)
	if err != nil || len(data) == 0 || data[len(data)-1] != "[DONE]" {
		t.Fatalf("err=%v data=%q", err, data)
	}
	text, deltas, finish := "", 0, ""
	for i, c := range parse(t, data) {
		for _, ch := range c.Choices {
			if ch.Delta.Content != "" {
				if n := len([]rune(ch.Delta.Content)); n > 4 {
					t.Errorf("delta %q has %d runes", ch.Delta.Content, n)
				}
				if (deltas == 0) != (ch.Delta.Role == "assistant") {
					t.Errorf("role %q on delta %d", ch.Delta.Role, deltas)
				}
				text += ch.Delta.Content
				deltas++
			}
			if ch.FinishReason != "" {
				finish = ch.FinishReason
			}
		}
		_ = i
	}
	if text != "héllo wörld!" || deltas < 3 || finish != "stop" {
		t.Errorf("text=%q deltas=%d finish=%q", text, deltas, finish)
	}
	usage := data[len(data)-2]
	var u struct {
		Choices []any `json:"choices"`
		Usage   struct {
			P int `json:"prompt_tokens"`
			C int `json:"completion_tokens"`
			T int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(usage), &u); err != nil || len(u.Choices) != 0 || u.Usage.P != 7 || u.Usage.C != 3 || u.Usage.T != 10 {
		t.Errorf("usage chunk %q: %v %+v", usage, err, u)
	}
}

func TestFakeToolCallSplit(t *testing.T) {
	args := `{"path":"a.txt","content":"hello"}`
	s := New(Response{ToolCalls: []ToolCall{
		{ID: "c1", Name: "write_file", Arguments: args},
		{ID: "c2", Name: "glob", Arguments: "{}"},
	}})
	defer s.Close()
	data, _ := events(post(t, s.URL, reqBody))
	ids, names, got, frags := map[int]string{}, map[int]string{}, map[int]string{}, map[int]int{}
	finish := ""
	for _, c := range parse(t, data) {
		for _, ch := range c.Choices {
			if ch.FinishReason != "" {
				finish = ch.FinishReason
			}
			for _, tc := range ch.Delta.ToolCalls {
				if tc.ID != "" {
					ids[tc.Index] = tc.ID
				}
				if tc.Function.Name != "" {
					names[tc.Index] = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					got[tc.Index] += tc.Function.Arguments
					frags[tc.Index]++
				}
			}
		}
	}
	if ids[0] != "c1" || names[0] != "write_file" || got[0] != args || frags[0] < 2 {
		t.Errorf("call 0: id=%q name=%q args=%q frags=%d", ids[0], names[0], got[0], frags[0])
	}
	if ids[1] != "c2" || names[1] != "glob" || got[1] != "{}" || frags[1] < 2 {
		t.Errorf("call 1: id=%q name=%q args=%q frags=%d", ids[1], names[1], got[1], frags[1])
	}
	if finish != "tool_calls" {
		t.Errorf("finish=%q", finish)
	}
}

func TestFakeRecordsRequests(t *testing.T) {
	s := New()
	defer s.Close()
	_, _ = events(post(t, s.URL, reqBody))
	_, _ = events(post(t, s.URL, "not json"))
	reqs := s.Requests()
	if len(reqs) != 2 {
		t.Fatalf("recorded %d requests", len(reqs))
	}
	r := reqs[0]
	if r.Method != "POST" || r.Path != "/v1/chat/completions" || r.Model != "m1" || r.ReasoningEffort != "low" ||
		r.Header.Get("Authorization") != "Bearer SECRET123" || r.JSON["stream"] != true || len(r.Body) == 0 {
		t.Errorf("request 0 = %+v", r)
	}
	if len(r.ToolNames) != 2 || r.ToolNames[0] != "read_file" || r.ToolNames[1] != "bash" {
		t.Errorf("tool names = %v", r.ToolNames)
	}
	if len(r.Messages) != 3 || r.Messages[0].Content != "hi" || r.Messages[1].ToolCalls[0] != (ToolCall{ID: "c0", Name: "glob", Arguments: "{}"}) ||
		r.Messages[2].Role != "tool" || r.Messages[2].ToolCallID != "c0" || r.Messages[2].Content != "res" {
		t.Errorf("messages = %+v", r.Messages)
	}
	if reqs[1].JSON != nil || reqs[1].Model != "" || string(reqs[1].Body) != "not json" {
		t.Errorf("undecodable request = %+v", reqs[1])
	}
	reqs[0].Model = "mutated"
	if s.Requests()[0].Model != "m1" {
		t.Error("Requests must return a copy")
	}
}

func TestFakeErrorStatus(t *testing.T) {
	s := New(Response{Status: 429, Body: `{"error":"slow down"}`})
	defer s.Close()
	resp := post(t, s.URL, reqBody)
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 429 || string(b) != `{"error":"slow down"}` || resp.Header.Get("Content-Type") == "text/event-stream" {
		t.Errorf("status=%d body=%q", resp.StatusCode, b)
	}
}

func TestFakeDrop(t *testing.T) {
	s := New(Response{Text: "partial text", Drop: true})
	defer s.Close()
	data, _ := events(post(t, s.URL, reqBody))
	text := ""
	for _, c := range parse(t, data) {
		for _, ch := range c.Choices {
			text += ch.Delta.Content
			if ch.FinishReason != "" {
				t.Errorf("dropped stream has finish_reason %q", ch.FinishReason)
			}
		}
	}
	for _, d := range data {
		if d == "[DONE]" {
			t.Error("dropped stream sent [DONE]")
		}
	}
	if text != "partial text" {
		t.Errorf("content before the drop = %q", text)
	}
}

func TestFakeDefaultAfterScript(t *testing.T) {
	s := New(Response{Text: "first"})
	defer s.Close()
	var got []string
	for i := 0; i < 3; i++ {
		data, _ := events(post(t, s.URL, reqBody))
		text := ""
		for _, c := range parse(t, data) {
			for _, ch := range c.Choices {
				text += ch.Delta.Content
			}
		}
		got = append(got, text)
	}
	if strings.Join(got, ",") != "first,ok,ok" {
		t.Errorf("replies = %q", got)
	}
}

func TestConfigJSON(t *testing.T) {
	var c struct {
		DefaultModel string `json:"default_model"`
		Provider     map[string]struct {
			Name    string `json:"name"`
			Options struct {
				BaseURL string `json:"baseURL"`
				APIKey  string `json:"apiKey"`
			} `json:"options"`
			Models map[string]struct {
				Reasoning *bool `json:"reasoning"`
				Limit     struct {
					Context int `json:"context"`
				} `json:"limit"`
			} `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal([]byte(ConfigJSON("http://127.0.0.1:9/v1", "{env:FAKE_KEY}", 4096)), &c); err != nil {
		t.Fatal(err)
	}
	f := c.Provider["fake"]
	m1, m2 := f.Models["m1"], f.Models["m2"]
	if c.DefaultModel != "fake/m1" || f.Name == "" || f.Options.BaseURL != "http://127.0.0.1:9/v1" || f.Options.APIKey != "{env:FAKE_KEY}" ||
		m1.Limit.Context != 4096 || m2.Reasoning == nil || *m2.Reasoning {
		t.Errorf("config = %+v", c)
	}
}
