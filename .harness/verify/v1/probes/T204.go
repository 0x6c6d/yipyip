//go:build ignore

// Supervisor probe for T204 (providertest fake SSE server). Workers never edit it.
// Exit codes: 0 = OK, 1 = FAIL, 125 = setup error or crash.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"

	"yipyip/internal/provider/providertest"
)

func setup(f string, a ...any) { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)  { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)  { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }

const body = `{"model":"m1","stream":true,"reasoning_effort":"low",
 "messages":[{"role":"system","content":"sys"},{"role":"user","content":"hi"},
  {"role":"assistant","content":"","tool_calls":[{"id":"c0","type":"function","function":{"name":"glob","arguments":"{}"}}]},
  {"role":"tool","tool_call_id":"c0","content":"res"}],
 "tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object"}}}]}`

// post sends one chat request and returns the status, the data payloads and the read error.
func post(url string) (int, []string, string, error) {
	req, _ := http.NewRequest("POST", url+"/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer k")
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		setup("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, nil, string(b), nil
	}
	var data []string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if p, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			data = append(data, p)
		}
	}
	return resp.StatusCode, data, "", sc.Err()
}

type chunk struct {
	Choices []struct {
		Delta struct {
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
		Prompt     int `json:"prompt_tokens"`
		Completion int `json:"completion_tokens"`
		Total      int `json:"total_tokens"`
	} `json:"usage"`
}

func parse(data []string) []chunk {
	var out []chunk
	for _, d := range data {
		if d == "[DONE]" {
			continue
		}
		var c chunk
		if err := json.Unmarshal([]byte(d), &c); err != nil {
			fail("chunk is not JSON: %q", d)
		}
		out = append(out, c)
	}
	return out
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("PANIC:", r)
			os.Exit(125)
		}
	}()
	if len(os.Args) != 2 {
		setup("usage: probe <case>")
	}
	switch os.Args[1] {
	case "text":
		s := providertest.New(providertest.Response{Text: "hello world", Usage: &providertest.Usage{Prompt: 100, Completion: 20}})
		defer s.Close()
		if !strings.HasSuffix(s.URL, "/v1") {
			fail("URL must end in /v1: %q", s.URL)
		}
		_, data, _, err := post(s.URL)
		if err != nil || len(data) == 0 || data[len(data)-1] != "[DONE]" {
			fail("stream must end with [DONE]: err=%v data=%q", err, data)
		}
		text, deltas, usage := "", 0, false
		for _, c := range parse(data) {
			for _, ch := range c.Choices {
				if ch.Delta.Content != "" {
					text += ch.Delta.Content
					deltas++
				}
			}
			if c.Usage != nil && len(c.Choices) == 0 && c.Usage.Prompt == 100 && c.Usage.Completion == 20 && c.Usage.Total == 120 {
				usage = true
			}
		}
		if text != "hello world" || deltas < 2 || !usage {
			fail("text=%q deltas=%d usage chunk=%v", text, deltas, usage)
		}
		reqs := s.Requests()
		if len(reqs) != 1 {
			fail("recorded %d requests", len(reqs))
		}
		r := reqs[0]
		if r.Method != "POST" || r.Path != "/v1/chat/completions" || r.Model != "m1" || r.ReasoningEffort != "low" ||
			r.Header.Get("Authorization") != "Bearer k" || r.JSON["stream"] != true || len(r.Body) == 0 {
			fail("recorded request = %s %s model=%q effort=%q", r.Method, r.Path, r.Model, r.ReasoningEffort)
		}
		if !reflect.DeepEqual(r.ToolNames, []string{"read_file"}) || len(r.Messages) != 4 || r.Messages[1].Content != "hi" ||
			r.Messages[3].Role != "tool" || r.Messages[3].ToolCallID != "c0" || len(r.Messages[2].ToolCalls) != 1 || r.Messages[2].ToolCalls[0].Name != "glob" {
			fail("decoded tools/messages wrong: %v %+v", r.ToolNames, r.Messages)
		}
		pass("text stream, usage, recorded request")
	case "toolsplit":
		args := `{"path":"a.txt","old_string":"x","new_string":"y"}`
		s := providertest.New(providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "c1", Name: "edit_file", Arguments: args}}})
		defer s.Close()
		_, data, _, _ := post(s.URL)
		id, name, got, frags, finish := "", "", "", 0, ""
		for _, c := range parse(data) {
			for _, ch := range c.Choices {
				if ch.FinishReason != "" {
					finish = ch.FinishReason
				}
				for _, tc := range ch.Delta.ToolCalls {
					if tc.ID != "" {
						id = tc.ID
					}
					if tc.Function.Name != "" {
						name = tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						got += tc.Function.Arguments
						frags++
					}
				}
			}
		}
		if id != "c1" || name != "edit_file" || got != args || frags < 2 || finish != "tool_calls" {
			fail("id=%q name=%q args=%q fragments=%d finish=%q", id, name, got, frags, finish)
		}
		pass("tool call arguments split across %d chunks", frags)
	case "status":
		s := providertest.New(providertest.Response{Status: 401, Body: "bad key"})
		defer s.Close()
		code, _, b, _ := post(s.URL)
		if code != 401 || !strings.Contains(b, "bad key") {
			fail("status=%d body=%q", code, b)
		}
		pass("error status")
	case "default":
		s := providertest.New()
		defer s.Close()
		_, data, _, _ := post(s.URL)
		text := ""
		for _, c := range parse(data) {
			for _, ch := range c.Choices {
				text += ch.Delta.Content
			}
		}
		if text != "ok" {
			fail("default reply = %q", text)
		}
		pass("default reply after the script")
	case "drop":
		s := providertest.New(providertest.Response{Text: "partial", Drop: true})
		defer s.Close()
		_, data, _, _ := post(s.URL)
		for _, d := range data {
			if d == "[DONE]" {
				fail("dropped stream sent [DONE]")
			}
		}
		pass("dropped stream")
	case "func":
		s := providertest.NewFunc(func(n int, req providertest.Request) providertest.Response {
			return providertest.Response{Text: fmt.Sprintf("n=%d %s", n, req.Model)}
		})
		defer s.Close()
		for i := 0; i < 2; i++ {
			_, data, _, _ := post(s.URL)
			text := ""
			for _, c := range parse(data) {
				for _, ch := range c.Choices {
					text += ch.Delta.Content
				}
			}
			if text != fmt.Sprintf("n=%d m1", i) {
				fail("NewFunc reply %d = %q", i, text)
			}
		}
		pass("NewFunc")
	case "config":
		var c struct {
			DefaultModel string `json:"default_model"`
			Provider     map[string]struct {
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
		if err := json.Unmarshal([]byte(providertest.ConfigJSON("http://127.0.0.1:1/v1", "{env:FAKE_KEY}", 8000)), &c); err != nil {
			fail("ConfigJSON is not JSON: %v", err)
		}
		f := c.Provider["fake"]
		m2 := f.Models["m2"]
		if c.DefaultModel != "fake/m1" || f.Options.BaseURL != "http://127.0.0.1:1/v1" || f.Options.APIKey != "{env:FAKE_KEY}" ||
			f.Models["m1"].Limit.Context != 8000 || m2.Reasoning == nil || *m2.Reasoning {
			fail("ConfigJSON content wrong: %+v", c)
		}
		pass("ConfigJSON")
	default:
		setup("unknown case %q", os.Args[1])
	}
}
