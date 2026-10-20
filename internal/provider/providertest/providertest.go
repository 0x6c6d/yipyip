// Package providertest is an offline fake OpenAI-compatible chat provider
// (httptest on 127.0.0.1) for tests. It imports no yipyip package.
package providertest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// ToolCall is one function call, as streamed by the fake or found in a request.
type ToolCall struct{ ID, Name, Arguments string }

// Usage holds token counts; the total is Prompt + Completion.
type Usage struct{ Prompt, Completion int }

// Response scripts one reply of the fake provider.
type Response struct {
	Status    int           // 0 or 200 = SSE stream; anything else = plain error response
	Body      string        // body for an error response
	Text      string        // streamed as content deltas
	ToolCalls []ToolCall    // streamed as tool_calls deltas
	Usage     *Usage        // final usage chunk when non-nil
	Raw       []string      // if non-nil: sent verbatim as "data: <p>\n\n" lines instead of the above
	Drop      bool          // abort the connection after the content (no finish chunk, no [DONE])
	Delay     time.Duration // wait before the first byte (ends early if the request is cancelled)
}

// Message is one decoded entry of the request's "messages".
type Message struct {
	Role, Content, ToolCallID string
	ToolCalls                 []ToolCall
}

// Request is a recorded request.
type Request struct {
	Method, Path           string
	Header                 http.Header
	Body                   []byte
	JSON                   map[string]any // decoded body (nil if not JSON)
	Model, ReasoningEffort string         // "model", "reasoning_effort" ("" when absent)
	Messages               []Message      // decoded "messages" (tool_calls/tool_call_id included)
	ToolNames              []string       // tools[].function.name in order
}

// Server is a running fake provider.
type Server struct {
	URL string // base URL to configure: "http://127.0.0.1:<port>/v1"

	ts   *httptest.Server
	fn   func(n int, req Request) Response
	mu   sync.Mutex
	reqs []Request
}

// New starts a fake provider: request i gets responses[i]; afterwards Response{Text: "ok"}.
func New(responses ...Response) *Server {
	rs := append([]Response(nil), responses...)
	return NewFunc(func(n int, _ Request) Response {
		if n < len(rs) {
			return rs[n]
		}
		return Response{Text: "ok"}
	})
}

// NewFunc starts a fake provider whose reply to the n-th (0-based) request is fn(n, req).
func NewFunc(fn func(n int, req Request) Response) *Server {
	s := &Server{fn: fn}
	s.ts = httptest.NewServer(http.HandlerFunc(s.handle))
	s.URL = s.ts.URL + "/v1"
	return s
}

// Requests returns a copy of the recorded requests in arrival order.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.reqs...)
}

// Close shuts the server down.
func (s *Server) Close() {
	s.ts.CloseClientConnections()
	s.ts.Close()
}

// ConfigJSON returns a yipyip config using the fake provider with models m1 and m2.
func ConfigJSON(baseURL, apiKey string, contextLimit int) string {
	cfg := map[string]any{
		"default_model": "fake/m1",
		"provider": map[string]any{
			"fake": map[string]any{
				"name": "Fake",
				"options": map[string]any{
					"baseURL": baseURL,
					"apiKey":  apiKey,
				},
				"models": map[string]any{
					"m1": map[string]any{
						"name":  "m1",
						"limit": map[string]any{"context": contextLimit},
					},
					"m2": map[string]any{
						"name":      "m2",
						"reasoning": false,
					},
				},
			},
		},
	}
	b, _ := json.Marshal(cfg)
	return string(b)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	req := decodeRequest(r, body)
	s.mu.Lock()
	n := len(s.reqs)
	s.reqs = append(s.reqs, req)
	s.mu.Unlock()

	resp := s.fn(n, req)
	if resp.Delay > 0 {
		t := time.NewTimer(resp.Delay)
		select {
		case <-t.C:
		case <-r.Context().Done():
			t.Stop()
			return
		}
	}
	if resp.Status != 0 && resp.Status != http.StatusOK {
		w.WriteHeader(resp.Status)
		_, _ = io.WriteString(w, resp.Body)
		return
	}
	writeSSE(w, resp)
}

func decodeRequest(r *http.Request, body []byte) Request {
	req := Request{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: body}
	var m map[string]any
	if json.Unmarshal(body, &m) != nil || m == nil {
		return req
	}
	req.JSON = m
	var typed struct {
		Model           string `json:"model"`
		ReasoningEffort string `json:"reasoning_effort"`
		Messages        []struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			ToolCallID string          `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	// Decode leniently: a field of an unexpected type must not hide the others.
	_ = json.Unmarshal(body, &typed)
	req.Model, req.ReasoningEffort = typed.Model, typed.ReasoningEffort
	for _, tm := range typed.Messages {
		msg := Message{Role: tm.Role, ToolCallID: tm.ToolCallID}
		var s string
		if json.Unmarshal(tm.Content, &s) == nil {
			msg.Content = s
		}
		for _, tc := range tm.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
		}
		req.Messages = append(req.Messages, msg)
	}
	for _, t := range typed.Tools {
		req.ToolNames = append(req.ToolNames, t.Function.Name)
	}
	return req
}

func writeSSE(w http.ResponseWriter, resp Response) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	send := func(payload string) {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		if fl != nil {
			fl.Flush()
		}
	}
	sendJSON := func(v any) {
		b, _ := json.Marshal(v)
		send(string(b))
	}
	delta := func(d map[string]any, finish any) map[string]any {
		return map[string]any{"choices": []any{map[string]any{"index": 0, "delta": d, "finish_reason": finish}}}
	}

	if resp.Raw != nil {
		for _, p := range resp.Raw {
			send(p)
		}
		if resp.Drop {
			panic(http.ErrAbortHandler)
		}
		return
	}

	first := true
	for _, piece := range splitRunes(resp.Text, 4) {
		d := map[string]any{"content": piece}
		if first {
			d["role"] = "assistant"
			first = false
		}
		sendJSON(delta(d, nil))
	}
	for i, tc := range resp.ToolCalls {
		d := map[string]any{"tool_calls": []any{map[string]any{
			"index": i, "id": tc.ID, "type": "function",
			"function": map[string]any{"name": tc.Name, "arguments": ""},
		}}}
		if first {
			d["role"] = "assistant"
			first = false
		}
		sendJSON(delta(d, nil))
		runes := []rune(tc.Arguments)
		size := 8
		if h := (len(runes) + 1) / 2; h < size {
			size = h
		}
		for _, piece := range splitRunes(tc.Arguments, size) {
			sendJSON(delta(map[string]any{"tool_calls": []any{map[string]any{
				"index": i, "function": map[string]any{"arguments": piece},
			}}}, nil))
		}
	}
	if resp.Drop {
		panic(http.ErrAbortHandler)
	}
	finish := "stop"
	if len(resp.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	sendJSON(delta(map[string]any{}, finish))
	if u := resp.Usage; u != nil {
		sendJSON(map[string]any{"choices": []any{}, "usage": map[string]any{
			"prompt_tokens": u.Prompt, "completion_tokens": u.Completion, "total_tokens": u.Prompt + u.Completion,
		}})
	}
	send("[DONE]")
}

// splitRunes cuts s into pieces of at most n runes (n < 1 is treated as 1).
func splitRunes(s string, n int) []string {
	if n < 1 {
		n = 1
	}
	var out []string
	r := []rune(s)
	for len(r) > 0 {
		k := min(n, len(r))
		out = append(out, string(r[:k]))
		r = r[k:]
	}
	return out
}
