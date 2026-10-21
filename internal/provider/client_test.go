package provider_test

import (
	"context"
	"testing"
	"time"

	"yipyip/internal/config"
	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

func newTestClient(t *testing.T, srv *providertest.Server, apiKey string) *provider.Client {
	t.Helper()
	hc, err := provider.NewHTTPClient([]string{srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return provider.NewClient(srv.URL+"/", apiKey, hc, config.NewRedactor(apiKey))
}

func collect(t *testing.T, ch <-chan provider.Event) []provider.Event {
	t.Helper()
	var evs []provider.Event
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return evs
			}
			evs = append(evs, ev)
		case <-timeout:
			t.Fatal("timed out waiting for events")
		}
	}
}

func userReq() provider.Request {
	return provider.Request{Model: "m1", Messages: []provider.Message{{Role: "user", Content: "hi"}}}
}

func TestRequestPathAndHeaders(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "hello"})
	defer srv.Close()

	evs := collect(t, newTestClient(t, srv, "SECRET123").Stream(context.Background(), userReq()))
	if last := evs[len(evs)-1]; last.Kind != provider.EventDone {
		t.Fatalf("last event = %+v", last)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests", len(reqs))
	}
	r := reqs[0]
	if r.Method != "POST" || r.Path != "/v1/chat/completions" {
		t.Errorf("method/path = %s %s", r.Method, r.Path)
	}
	if got := r.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := r.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q", got)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer SECRET123" {
		t.Errorf("Authorization = %q", got)
	}

	srv2 := providertest.New()
	defer srv2.Close()
	collect(t, newTestClient(t, srv2, "").Stream(context.Background(), userReq()))
	if _, ok := srv2.Requests()[0].Header["Authorization"]; ok {
		t.Error("Authorization header sent with empty key")
	}
}

func TestSessionHeader(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "hello"})
	defer srv.Close()

	hc, err := provider.NewHTTPClient([]string{srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	c := provider.NewClient(srv.URL+"/", "SECRET123", hc, config.NewRedactor("SECRET123"))
	c.SetSession("ses_abc123")
	evs := collect(t, c.Stream(context.Background(), userReq()))
	if last := evs[len(evs)-1]; last.Kind != provider.EventDone {
		t.Fatalf("last event = %+v", last)
	}
	if got := srv.Requests()[0].Header.Get("x-opencode-session"); got != "ses_abc123" {
		t.Errorf("x-opencode-session = %q", got)
	}

	// Unset session ids are never sent.
	srv2 := providertest.New()
	defer srv2.Close()
	collect(t, newTestClient(t, srv2, "").Stream(context.Background(), userReq()))
	if _, ok := srv2.Requests()[0].Header["X-Opencode-Session"]; ok {
		t.Error("x-opencode-session sent without a session id")
	}
}

func TestRequestBodyFields(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	c := newTestClient(t, srv, "")

	req := provider.Request{
		Model: "m1",
		Messages: []provider.Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "hi"},
			{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "c1", Name: "bash", Arguments: `{"command":"ls"}`}}},
			{Role: "tool", ToolCallID: "c1", Content: "out"},
		},
		Tools:  []provider.ToolDef{{Name: "bash", Description: "run", Parameters: []byte(`{"type":"object"}`)}},
		Effort: "high", SendEffort: true,
	}
	collect(t, c.Stream(context.Background(), req))
	req.SendEffort = false
	req.Tools = nil
	collect(t, c.Stream(context.Background(), req))

	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("got %d requests", len(reqs))
	}
	a, b := reqs[0], reqs[1]
	if a.Model != "m1" || a.JSON["stream"] != true {
		t.Errorf("model/stream = %q %v", a.Model, a.JSON["stream"])
	}
	so, _ := a.JSON["stream_options"].(map[string]any)
	if so["include_usage"] != true {
		t.Errorf("stream_options = %v", a.JSON["stream_options"])
	}
	if a.ReasoningEffort != "high" {
		t.Errorf("reasoning_effort = %q", a.ReasoningEffort)
	}
	if len(a.ToolNames) != 1 || a.ToolNames[0] != "bash" {
		t.Errorf("tools = %v", a.ToolNames)
	}
	tools, _ := a.JSON["tools"].([]any)
	tool, _ := tools[0].(map[string]any)
	fn, _ := tool["function"].(map[string]any)
	if tool["type"] != "function" || fn["description"] != "run" || fn["parameters"] == nil {
		t.Errorf("tool = %v", tool)
	}
	if len(a.Messages) != 4 {
		t.Fatalf("messages = %+v", a.Messages)
	}
	if tc := a.Messages[2].ToolCalls; len(tc) != 1 || tc[0].ID != "c1" || tc[0].Name != "bash" || tc[0].Arguments != `{"command":"ls"}` {
		t.Errorf("assistant tool_calls = %+v", tc)
	}
	if a.Messages[3].ToolCallID != "c1" || a.Messages[3].Content != "out" {
		t.Errorf("tool message = %+v", a.Messages[3])
	}
	msgs, _ := a.JSON["messages"].([]any)
	if m2, _ := msgs[2].(map[string]any); m2["content"] != "" {
		t.Errorf("assistant content = %#v, want empty string present", m2["content"])
	}

	if _, ok := b.JSON["reasoning_effort"]; ok {
		t.Error("reasoning_effort present although SendEffort=false")
	}
	if _, ok := b.JSON["tools"]; ok {
		t.Error("tools present although empty")
	}
}

func TestTextDeltas(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "Hello, wörld! Longer text."})
	defer srv.Close()

	evs := collect(t, newTestClient(t, srv, "").Stream(context.Background(), userReq()))
	var text string
	n := 0
	for i, ev := range evs {
		switch ev.Kind {
		case provider.EventText:
			text += ev.Text
			n++
		case provider.EventDone:
			if i != len(evs)-1 {
				t.Errorf("EventDone at %d of %d", i, len(evs))
			}
		default:
			t.Errorf("unexpected event %+v", ev)
		}
	}
	if text != "Hello, wörld! Longer text." || n < 2 {
		t.Errorf("text = %q in %d deltas", text, n)
	}
}

func TestToolCallArgsSplit(t *testing.T) {
	calls := []providertest.ToolCall{
		{ID: "call_a", Name: "bash", Arguments: `{"command":"echo hello world"}`},
		{ID: "call_b", Name: "read_file", Arguments: `{"path":"a/b/c.txt"}`},
	}
	srv := providertest.New(providertest.Response{Text: "x", ToolCalls: calls})
	defer srv.Close()

	evs := collect(t, newTestClient(t, srv, "").Stream(context.Background(), userReq()))
	var got []provider.ToolCall
	for _, ev := range evs {
		if ev.Kind == provider.EventToolCall {
			got = append(got, ev.ToolCall)
		}
	}
	if len(got) != 2 {
		t.Fatalf("tool calls = %+v", got)
	}
	for i, w := range calls {
		if got[i] != (provider.ToolCall{ID: w.ID, Name: w.Name, Arguments: w.Arguments}) {
			t.Errorf("call %d = %+v, want %+v", i, got[i], w)
		}
	}
	if evs[len(evs)-1].Kind != provider.EventDone {
		t.Errorf("last event = %+v", evs[len(evs)-1])
	}
}

func TestUsageParsing(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "hi", Usage: &providertest.Usage{Prompt: 12, Completion: 5}})
	defer srv.Close()

	evs := collect(t, newTestClient(t, srv, "").Stream(context.Background(), userReq()))
	if len(evs) < 3 {
		t.Fatalf("events = %+v", evs)
	}
	u, d := evs[len(evs)-2], evs[len(evs)-1]
	if u.Kind != provider.EventUsage || u.Usage != (provider.Usage{PromptTokens: 12, CompletionTokens: 5, TotalTokens: 17}) {
		t.Errorf("usage event = %+v", u)
	}
	if d.Kind != provider.EventDone {
		t.Errorf("last = %+v", d)
	}

	// No usage chunk: no EventUsage; non-retryable non-2xx: single EventError.
	srv2 := providertest.New(providertest.Response{Status: 400, Body: "boom"})
	defer srv2.Close()
	evs = collect(t, newTestClient(t, srv2, "").Stream(context.Background(), userReq()))
	if len(evs) != 1 || evs[0].Kind != provider.EventError || evs[0].Err.Error() != "HTTP 400: boom" {
		t.Errorf("error events = %+v", evs)
	}
}
