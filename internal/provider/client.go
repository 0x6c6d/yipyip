package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"

	"yipyip/internal/config"
	"yipyip/internal/crashlog"
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
	// EventRetry is a non-final status event: an attempt failed before the
	// first SSE byte and the request is sent again after Retry.Wait.
	EventRetry
)

// Event is one item of a streamed response.
type Event struct {
	Kind     EventKind
	Text     string   // EventText
	ToolCall ToolCall // EventToolCall (complete call)
	Usage    Usage    // EventUsage
	Err      error    // EventError
	Retry    Retry    // EventRetry
}

// Retry describes one scheduled retry ("retrying (Attempt/Max) in Wait").
type Retry struct {
	Attempt int           // number of the coming attempt, 2..MaxAttempts
	Max     int           // MaxAttempts
	Wait    time.Duration // wait before that attempt
	Reason  string        // redacted text of the failure that caused it
}

// MaxAttempts is the total number of attempts for a request whose attempts
// fail before the first SSE byte with a retryable error.
const MaxAttempts = 10

// retryDelays[i] is the wait after the (i+1)-th failed attempt.
var retryDelays = [MaxAttempts - 1]time.Duration{
	1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
	16 * time.Second, 32 * time.Second, 60 * time.Second, 60 * time.Second, 60 * time.Second,
}

// retryDelay returns the wait after failed attempt n (1-based).
func retryDelay(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	if n > len(retryDelays) {
		n = len(retryDelays)
	}
	return retryDelays[n-1]
}

// retryWait waits d or until ctx ends, whichever is first; it returns ctx's
// error when ctx ended. Tests replace it so that they never sleep.
var retryWait = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Client is a streaming client for one OpenAI-compatible endpoint.
type Client struct {
	baseURL string
	apiKey  string
	session string // x-opencode-session value, "" = omit
	hc      *http.Client
	red     *config.Redactor
	own     *config.Redactor                                 // masks apiKey even when red is nil
	wait    func(ctx context.Context, d time.Duration) error // retry wait; nil = retryWait
}

// NewClient returns a client for baseURL. hc must come from NewHTTPClient.
func NewClient(baseURL, apiKey string, hc *http.Client, red *config.Redactor) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, hc: hc, red: red, own: config.NewRedactor(apiKey)}
}

// SetSession sets the stable session id sent with every request in the
// x-opencode-session header. Gateways such as the OpenCode Console require it
// to route a conversation and reuse its prompt cache.
func (c *Client) SetSession(id string) { c.session = id }

// SetRetryWait replaces the wait before a retry for this client (nil restores
// the default). It is a seam for tests that must not sleep.
func (c *Client) SetRetryWait(fn func(ctx context.Context, d time.Duration) error) { c.wait = fn }

// Stream sends req and streams the reply. The channel always ends with exactly
// one EventDone or EventError and is then closed.
func (c *Client) Stream(ctx context.Context, req Request) <-chan Event {
	out := make(chan Event)
	go func() {
		defer close(out)
		defer func() {
			if p := recover(); p != nil {
				crashlog.Recovered("stream", p)
				msg := c.redact(fmt.Sprintf("internal error: %v", p))
				final(ctx, out, Event{Kind: EventError, Err: &redactedError{msg: msg, err: errors.New(msg)}})
			}
		}()
		c.run(ctx, req, out)
	}()
	return out
}

// sseEventHook, when set (tests only), is called with every SSE payload before
// it is handled.
var sseEventHook func(data string)

// finalGrace bounds how long the final event waits for a reader once ctx is
// cancelled, so an abandoned stream never leaves its goroutine blocked.
const finalGrace = 2 * time.Second

// send delivers a non-final ev unless ctx is cancelled first.
func send(ctx context.Context, out chan<- Event, ev Event) bool {
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// final delivers the one final event. While ctx is live it waits for the
// reader; after a cancel it waits at most finalGrace.
func final(ctx context.Context, out chan<- Event, ev Event) {
	select {
	case out <- ev:
		return
	case <-ctx.Done():
	}
	t := time.NewTimer(finalGrace)
	defer t.Stop()
	select {
	case out <- ev:
	case <-t.C:
	}
}

// redactedError carries a redacted message but keeps the original error for
// errors.Is / errors.As.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// redact masks the configured secrets and always the client's own API key.
func (c *Client) redact(s string) string {
	return c.own.Redact(c.red.Redact(s))
}

// fail emits the final EventError for err. It is the single place that turns
// failures into events: the text is always redacted, and a cancelled ctx is
// always reported as such (errors.Is(err, ctx.Err())).
func (c *Client) fail(ctx context.Context, out chan<- Event, err error) {
	if cerr := ctx.Err(); cerr != nil && !errors.Is(err, cerr) {
		if err == nil {
			err = cerr
		} else {
			err = fmt.Errorf("%w (%w)", cerr, err)
		}
	}
	if err == nil {
		err = errors.New("unknown error")
	}
	final(ctx, out, Event{Kind: EventError, Err: &redactedError{msg: c.redact(err.Error()), err: err}})
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
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// errStreamInterrupted marks a body that ended before [DONE] and before any
// finish_reason.
var errStreamInterrupted = errors.New("stream interrupted")

// errInvalidChunk marks a data payload that is not valid JSON.
var errInvalidChunk = errors.New("invalid stream chunk")

// maxErrorBody is how much of a non-2xx body is read for the excerpt.
const maxErrorBody = 64 << 10

// excerptLen is the excerpt length in characters.
const excerptLen = 500

// httpError builds "HTTP <status>: <excerpt>" from a non-2xx response. The
// body is redacted before it is cut so that no secret fragment survives.
func (c *Client) httpError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody+1))
	if len(raw) > maxErrorBody {
		// Drop the trailing token that the read limit may have cut, so
		// that a secret straddling the limit cannot leave a fragment.
		raw = raw[:maxErrorBody]
		if i := strings.LastIndexFunc(string(raw), unicode.IsSpace); i >= 0 {
			raw = raw[:i]
		}
	}
	text := strings.Join(strings.Fields(c.redact(string(raw))), " ")
	if r := []rune(text); len(r) > excerptLen {
		text = string(r[:excerptLen])
	}
	text = c.redact(text)
	if text == "" {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, text)
}

// statusError is a non-2xx response; its text is the httpError excerpt.
type statusError struct {
	code int
	err  error
}

func (e *statusError) Error() string { return e.err.Error() }
func (e *statusError) Unwrap() error { return e.err }

// retryable reports whether err, a failure before the first SSE byte, is
// worth another attempt: timeouts, connection resets (including a server that
// closed the connection with nothing received), HTTP 429 and HTTP 5xx. Other
// 4xx statuses, refused hosts and a cancelled ctx are final.
func retryable(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	var se *statusError
	if errors.As(err, &se) {
		return se.code == http.StatusTooManyRequests || (se.code >= 500 && se.code <= 599)
	}
	if errors.Is(err, ErrHostNotAllowed) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

// run sends the request, retrying failures before the first SSE byte, and
// streams the reply of the attempt that got one.
func (c *Client) run(ctx context.Context, req Request, out chan<- Event) {
	payload, err := c.body(req)
	if err != nil {
		c.fail(ctx, out, err)
		return
	}
	for attempt := 1; ; attempt++ {
		err := c.attempt(ctx, payload, out)
		if err == nil {
			return // the attempt emitted the final event
		}
		if attempt >= MaxAttempts || !retryable(ctx, err) {
			c.fail(ctx, out, err)
			return
		}
		wait := retryDelay(attempt)
		ev := Event{Kind: EventRetry, Retry: Retry{Attempt: attempt + 1, Max: MaxAttempts, Wait: wait, Reason: c.redact(err.Error())}}
		if !send(ctx, out, ev) {
			c.fail(ctx, out, ctx.Err())
			return
		}
		waitFn := retryWait
		if c.wait != nil {
			waitFn = c.wait
		}
		if werr := waitFn(ctx, wait); werr != nil || ctx.Err() != nil {
			if werr == nil {
				werr = ctx.Err()
			}
			c.fail(ctx, out, werr)
			return
		}
	}
}

// countingReader counts the bytes read, marking the first SSE byte boundary.
type countingReader struct {
	r io.Reader
	n int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}

// attempt makes one attempt with the identical payload, ctx and http.Client.
// A failure before the first SSE byte is returned (and nothing is emitted);
// once the first byte has arrived the attempt emits the final event itself
// and returns nil, so a broken stream is never retried.
func (c *Client) attempt(ctx context.Context, payload []byte, out chan<- Event) error {
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		hreq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if c.session != "" {
		hreq.Header.Set("x-opencode-session", c.session)
	}
	resp, err := c.hc.Do(hreq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Unblock a pending body read as soon as ctx is cancelled, even if the
	// server keeps the connection open.
	stop := context.AfterFunc(ctx, func() { resp.Body.Close() })
	defer stop()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &statusError{code: resp.StatusCode, err: c.httpError(resp)}
	}
	body := &countingReader{r: resp.Body}

	type acc struct {
		id, name string
		args     strings.Builder
	}
	calls := map[int]*acc{}
	var usage *Usage
	alive, done, finished := true, false, false
	var chunkErr error
	err = readSSE(body, func(data string) bool {
		if sseEventHook != nil {
			sseEventHook(data)
		}
		if strings.TrimSpace(data) == "[DONE]" {
			done = true
			return false
		}
		var ch chunk
		if jerr := json.Unmarshal([]byte(data), &ch); jerr != nil {
			chunkErr = fmt.Errorf("%w: %w", errInvalidChunk, jerr)
			return false
		}
		if ch.Usage != nil {
			usage = &Usage{ch.Usage.PromptTokens, ch.Usage.CompletionTokens, ch.Usage.TotalTokens}
		}
		if len(ch.Choices) == 0 {
			return true
		}
		if fr := ch.Choices[0].FinishReason; fr != nil && *fr != "" {
			finished = true
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
	switch {
	case !alive || ctx.Err() != nil:
		c.fail(ctx, out, ctx.Err())
		return nil
	case chunkErr != nil:
		c.fail(ctx, out, chunkErr)
		return nil
	case !done && !finished:
		if err != nil && body.n == 0 && retryable(ctx, err) {
			// Broken before the first SSE byte: still retryable.
			return err
		}
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		c.fail(ctx, out, fmt.Errorf("%w: %w", errStreamInterrupted, err))
		return nil
	}
	// [DONE], or a finish_reason followed by EOF or a read error: the reply
	// is complete; partial tool calls of an interrupted stream never get here.

	idx := make([]int, 0, len(calls))
	for i := range calls {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for _, i := range idx {
		a := calls[i]
		tc := ToolCall{ID: a.id, Name: a.name, Arguments: a.args.String()}
		if !send(ctx, out, Event{Kind: EventToolCall, ToolCall: tc}) {
			c.fail(ctx, out, ctx.Err())
			return nil
		}
	}
	if usage != nil {
		if !send(ctx, out, Event{Kind: EventUsage, Usage: *usage}) {
			c.fail(ctx, out, ctx.Err())
			return nil
		}
	}
	final(ctx, out, Event{Kind: EventDone})
	return nil
}
