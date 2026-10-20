package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"yipyip/internal/config"
)

// ToolCall is one complete function call requested by the model.
type ToolCall struct{ ID, Name, Arguments string }

// Message is one chat message.
type Message struct {
	Role, Content string // "system" | "user" | "assistant" | "tool"
	ToolCalls     []ToolCall
	ToolCallID    string
}

// ToolDef describes one tool offered to the model.
type ToolDef struct {
	Name, Description string
	Parameters        json.RawMessage // JSON schema object
}

// Request is one chat-completion request.
type Request struct {
	Model      string // model key sent as "model"
	Messages   []Message
	Tools      []ToolDef
	Effort     string // low|medium|high
	SendEffort bool   // false: omit reasoning_effort
}

// Usage holds the token counts reported by the provider.
type Usage struct{ PromptTokens, CompletionTokens, TotalTokens int }

// EventKind tells which field of an Event is set.
type EventKind int

const (
	EventText EventKind = iota + 1
	EventToolCall
	EventUsage
	EventDone
	EventError
)

// Event is one item of a streamed response.
type Event struct {
	Kind     EventKind
	Text     string   // EventText
	ToolCall ToolCall // EventToolCall (complete call)
	Usage    Usage    // EventUsage
	Err      error    // EventError
}

// Client is a streaming client for one OpenAI-compatible endpoint.
type Client struct {
	baseURL string
	apiKey  string
	hc      *http.Client
	red     *config.Redactor
}

// NewClient returns a client for baseURL. hc must come from NewHTTPClient.
func NewClient(baseURL, apiKey string, hc *http.Client, red *config.Redactor) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, hc: hc, red: red}
}

// Stream sends req and streams the reply. The channel always ends with exactly
// one EventDone or EventError and is then closed.
func (c *Client) Stream(ctx context.Context, req Request) <-chan Event {
	out := make(chan Event)
	go func() {
		defer close(out)
		c.run(ctx, req, out)
	}()
	return out
}

// send delivers ev unless ctx is cancelled first.
func send(ctx context.Context, out chan<- Event, ev Event) bool {
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// fail emits the final EventError for err (redacted). It is the single place
// that turns failures into events.
func (c *Client) fail(ctx context.Context, out chan<- Event, err error) {
	if err == nil {
		err = fmt.Errorf("unknown error")
	}
	msg := c.red.Redact(err.Error())
	send(ctx, out, Event{Kind: EventError, Err: fmt.Errorf("%s", msg)})
}

type wireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

func (c *Client) body(req Request) ([]byte, error) {
	msgs := make([]wireMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		wm := wireMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			w := wireToolCall{ID: tc.ID, Type: "function"}
			w.Function.Name, w.Function.Arguments = tc.Name, tc.Arguments
			wm.ToolCalls = append(wm.ToolCalls, w)
		}
		msgs = append(msgs, wm)
	}
	body := map[string]any{
		"model":          req.Model,
		"messages":       msgs,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			params := t.Parameters
			if len(params) == 0 {
				params = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{
				"name": t.Name, "description": t.Description, "parameters": params,
			}})
		}
		body["tools"] = tools
	}
	if req.SendEffort && req.Effort != "" {
		body["reasoning_effort"] = req.Effort
	}
	return json.Marshal(body)
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
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (c *Client) run(ctx context.Context, req Request, out chan<- Event) {
	payload, err := c.body(req)
	if err != nil {
		c.fail(ctx, out, err)
		return
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		c.fail(ctx, out, err)
		return
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		hreq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.hc.Do(hreq)
	if err != nil {
		c.fail(ctx, out, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		c.fail(ctx, out, fmt.Errorf("HTTP %d", resp.StatusCode))
		return
	}

	type acc struct {
		id, name string
		args     strings.Builder
	}
	calls := map[int]*acc{}
	var usage *Usage
	alive := true
	err = readSSE(resp.Body, func(data string) bool {
		if strings.TrimSpace(data) == "[DONE]" {
			return false
		}
		var ch chunk
		if json.Unmarshal([]byte(data), &ch) != nil {
			return true // ignore malformed chunks
		}
		if ch.Usage != nil {
			usage = &Usage{ch.Usage.PromptTokens, ch.Usage.CompletionTokens, ch.Usage.TotalTokens}
		}
		if len(ch.Choices) == 0 {
			return true
		}
		d := ch.Choices[0].Delta
		if d.Content != "" {
			if alive = send(ctx, out, Event{Kind: EventText, Text: d.Content}); !alive {
				return false
			}
		}
		for _, tc := range d.ToolCalls {
			a := calls[tc.Index]
			if a == nil {
				a = &acc{}
				calls[tc.Index] = a
			}
			if tc.ID != "" {
				a.id = tc.ID
			}
			if tc.Function.Name != "" {
				a.name = tc.Function.Name
			}
			a.args.WriteString(tc.Function.Arguments)
		}
		return true
	})
	if !alive {
		return
	}
	if err != nil {
		c.fail(ctx, out, err)
		return
	}

	idx := make([]int, 0, len(calls))
	for i := range calls {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for _, i := range idx {
		a := calls[i]
		tc := ToolCall{ID: a.id, Name: a.name, Arguments: a.args.String()}
		if !send(ctx, out, Event{Kind: EventToolCall, ToolCall: tc}) {
			return
		}
	}
	if usage != nil {
		if !send(ctx, out, Event{Kind: EventUsage, Usage: *usage}) {
			return
		}
	}
	send(ctx, out, Event{Kind: EventDone})
}
