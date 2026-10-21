package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"yipyip/internal/provider"
	"yipyip/internal/tools"
)

// MaxIterations bounds the model calls of one turn.
const MaxIterations = 50

// CancelledMessage is the tool result recorded for a tool call that was
// interrupted by a cancel, and the info message of a cancelled turn.
const CancelledMessage = "cancelled"

// errIterationLimit ends a turn whose model keeps requesting tools.
var errIterationLimit = fmt.Errorf("iteration limit (%d) reached", MaxIterations)

// toolSet is the set of tools offered to one agent loop: the definitions sent
// to the model and the function that runs a call. Subagents get a restricted
// set.
type toolSet struct {
	defs []provider.ToolDef
	run  func(ctx context.Context, name, args string) tools.Result

	model string    // model id for the requests; "" = the session's current model
	sub   *Subagent // non-nil for a subagent: receives its tokens and transcript
}

// offers reports whether the tool set offers the named tool to the model.
func (ts toolSet) offers(name string) bool {
	for _, d := range ts.defs {
		if d.Name == name {
			return true
		}
	}
	return false
}

// mainTools returns all registry tools.
func (s *Session) mainTools() toolSet {
	var defs []provider.ToolDef
	for _, t := range s.registry.Tools() {
		defs = append(defs, provider.ToolDef{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	return toolSet{
		defs: defs,
		run: func(ctx context.Context, name, args string) tools.Result {
			return s.registry.Run(ctx, name, json.RawMessage(args))
		},
	}
}

// Submit appends a user message and starts a turn in a goroutine. It returns
// at once; ErrNoModel when no model is configured, ErrBusy while a turn runs.
func (s *Session) Submit(text string) error {
	if s.ModelID() == "" {
		return ErrNoModel
	}
	if _, _, err := s.newClient(); err != nil {
		return err
	}
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.busy = true
	s.turnStarted = time.Now().UTC().Format(time.RFC3339)
	s.turnInterrupted = false
	s.cancelTurn = cancel
	s.turnDone = done
	gen := s.gen
	mgr := s.subs
	s.openTurnRecord() // records len(history) before the user message
	s.history = append(s.history, provider.Message{Role: "user", Content: s.Redact(text)})
	hist := copyHistory(s.history)
	s.mu.Unlock()

	go s.runTurn(ctx, cancel, gen, mgr, hist, done)
	return nil
}

// Cancel cancels the running turn; it is a no-op when idle.
func (s *Session) Cancel() {
	s.mu.Lock()
	cancel := s.cancelTurn
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Wait blocks until no turn is running.
func (s *Session) Wait() {
	s.mu.Lock()
	done := s.turnDone
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}

// Busy reports whether a turn is running.
func (s *Session) Busy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy
}

// runTurn runs one main-agent turn and always ends with EvTurnDone. The
// subagents spawned in the turn run under its context: when the turn ends
// (normally or cancelled) they are cancelled and waited for. All events of the
// turn and its subagents are queued before the turn is marked as finished.
func (s *Session) runTurn(ctx context.Context, cancel context.CancelFunc, gen uint64, mgr *Manager, hist []provider.Message, done chan struct{}) {
	defer func() {
		cancel()
		s.mu.Lock()
		s.busy = false
		if ctx.Err() != nil {
			s.turnInterrupted = true
		}
		s.cancelTurn = nil
		s.turnDone = nil
		s.mu.Unlock()
		close(done)
	}()
	var (
		newHist []provider.Message
		err     error
		spawned = &spawnedSubs{}
	)
	func() {
		defer func() {
			if p := recover(); p != nil {
				newHist = nil
				err = fmt.Errorf("internal error: %v", p)
			}
		}()
		hist = s.autoCompact(ctx, gen, hist)
		newHist, _, err = s.runLoop(ctx, gen, MainAgentID, hist, s.turnTools(mgr, spawned))
	}()
	if newHist != nil {
		s.mu.Lock()
		if s.gen == gen {
			s.history = newHist
		}
		s.mu.Unlock()
	}
	switch {
	case err == nil:
	case ctx.Err() != nil && errors.Is(err, ctx.Err()):
		s.emit(Event{Kind: EvInfo, AgentID: MainAgentID, Text: CancelledMessage})
	default:
		s.emit(Event{Kind: EvError, AgentID: MainAgentID, Text: err.Error()})
	}
	cancel()
	spawned.wait(mgr)
	s.emit(Event{Kind: EvTurnDone, AgentID: MainAgentID})
}

// runLoop runs the agent loop for agentID on hist with the tools of ts: up to
// MaxIterations model calls, executing the requested tools in between. It
// returns the extended history (always valid for a next request: every tool
// call has a result), the final answer text and an error (ctx.Err() when
// cancelled). gen is the session generation the token counts belong to.
func (s *Session) runLoop(ctx context.Context, gen uint64, agentID string, hist []provider.Message, ts toolSet) (out []provider.Message, final string, err error) {
	defer func() {
		if err != nil {
			out = closeOpenCalls(out)
		}
	}()
	out = hist
	client, m, err := s.clientFor(ts.model)
	if err != nil {
		return out, "", err
	}
	for i := 0; i < MaxIterations; i++ {
		if err := ctx.Err(); err != nil {
			return out, "", err
		}
		req := provider.Request{
			Model:      m.Key,
			Messages:   out,
			Tools:      ts.defs,
			Effort:     s.Effort(),
			SendEffort: m.Reasoning,
		}
		text, calls, usage, err := s.streamReply(ctx, client, req, agentID)
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {
				return out, "", cerr
			}
			return out, "", err
		}
		total := s.account(gen, agentID, req.Messages, text, calls, usage)
		if ts.sub != nil {
			ts.sub.AddTokens(total)
		}
		out = append(out, provider.Message{Role: "assistant", Content: text, ToolCalls: calls})
		if len(calls) == 0 {
			s.subLog(ts.sub, "final: "+text)
			return out, text, nil
		}
		for _, tc := range calls {
			s.emit(Event{Kind: EvToolCall, AgentID: agentID, Tool: tc.Name, Args: tc.Arguments})
			s.subLog(ts.sub, "tool "+tc.Name+" "+shortLine(tc.Arguments))
			var res tools.Result
			if IsGated(tc.Name) && !ts.offers(tc.Name) {
				// A gated tool this agent is not offered is refused without
				// asking (e.g. spawn_agent invented by a subagent).
				res = tools.Result{Output: "tool " + tc.Name + " is not available to this agent", IsError: true}
			} else {
				ok, err := s.gate.Check(ctx, s.Mode(), agentID, tc.Name, tc.Arguments)
				if err != nil {
					return out, "", err
				}
				if err := ctx.Err(); err != nil {
					return out, "", err
				}
				if ok {
					res = ts.run(ctx, tc.Name, tc.Arguments)
					if tc.Name == "bash" {
						s.recordBash(gen, tc.Arguments)
					}
				} else {
					res = tools.Result{Output: DeniedMessage, IsError: true}
				}
			}
			// Registry results are redacted already; the agent tools'
			// results (spawn/wait/cancel) are redacted here as well.
			res.Output = s.Redact(res.Output)
			s.emit(Event{Kind: EvToolResult, AgentID: agentID, Tool: tc.Name, Text: res.Output, IsError: res.IsError})
			if res.IsError {
				s.subLog(ts.sub, "error: "+shortLine(res.Output))
			} else {
				s.subLog(ts.sub, "result: "+shortLine(res.Output))
			}
			out = append(out, provider.Message{Role: "tool", Content: res.Output, ToolCallID: tc.ID})
			if err := ctx.Err(); err != nil {
				return out, "", err
			}
		}
	}
	return out, "", errIterationLimit
}

// streamReply sends req and collects the reply, emitting text deltas. It
// always reads the stream to its end so the producer goroutine finishes.
//
// The reply enters yipyip here, so it is redacted here: the deltas pass
// through a streaming redactor (a secret split across deltas is masked too;
// text that may still become a secret is held back until the stream ends),
// and the returned text and tool-call arguments are redacted as a whole, so
// the history, events, approvals and tool runs never see a secret.
func (s *Session) streamReply(ctx context.Context, c *provider.Client, req provider.Request, agentID string) (string, []provider.ToolCall, *provider.Usage, error) {
	sr := s.cfg.Redactor.Stream()
	send := func(text string) {
		if text != "" {
			s.emit(Event{Kind: EvText, AgentID: agentID, Text: text})
		}
	}
	text, calls, usage, err := collectReply(ctx, c, req, func(delta string) { send(sr.Write(delta)) })
	send(sr.Flush())
	return s.Redact(text), s.redactCalls(calls), usage, err
}

// redactCalls returns calls with every secret in the arguments masked.
func (s *Session) redactCalls(calls []provider.ToolCall) []provider.ToolCall {
	for i := range calls {
		calls[i].Arguments = s.Redact(calls[i].Arguments)
	}
	return calls
}

// collectReply sends req and collects the reply, calling onText (when
// non-nil) for every text delta. It always reads the stream to its end.
func collectReply(ctx context.Context, c *provider.Client, req provider.Request, onText func(string)) (string, []provider.ToolCall, *provider.Usage, error) {
	var (
		text  []byte
		calls []provider.ToolCall
		usage *provider.Usage
		err   error
	)
	for ev := range c.Stream(ctx, req) {
		switch ev.Kind {
		case provider.EventText:
			text = append(text, ev.Text...)
			if onText != nil {
				onText(ev.Text)
			}
		case provider.EventToolCall:
			calls = append(calls, ev.ToolCall)
		case provider.EventUsage:
			u := ev.Usage
			usage = &u
		case provider.EventError:
			err = ev.Err
			if err == nil {
				err = errors.New("unknown provider error")
			}
		}
	}
	return string(text), calls, usage, err
}

// account adds the token counts of one response of agentID to the session
// totals and returns the response's total; without reported usage it
// estimates characters/4. Counts of a stale generation are dropped. Only the
// main agent's requests set the context size.
func (s *Session) account(gen uint64, agentID string, msgs []provider.Message, text string, calls []provider.ToolCall, usage *provider.Usage) int {
	var in, out, total int
	if usage != nil {
		in, out, total = usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens
		if total == 0 {
			total = in + out
		}
	} else {
		in = estimateTokens(messageChars(msgs))
		n := utf8.RuneCountInString(text)
		for _, tc := range calls {
			n += utf8.RuneCountInString(tc.Arguments)
		}
		out = estimateTokens(n)
		total = in + out
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != gen {
		return total
	}
	s.tokens.In += in
	s.tokens.Out += out
	s.tokens.Total += total
	if agentID == MainAgentID {
		s.lastPromptTokens = in
	}
	return total
}

// messageChars counts the characters of message contents and tool arguments.
func messageChars(msgs []provider.Message) int {
	n := 0
	for _, m := range msgs {
		n += utf8.RuneCountInString(m.Content)
		for _, tc := range m.ToolCalls {
			n += utf8.RuneCountInString(tc.Arguments)
		}
	}
	return n
}

// estimateTokens estimates the tokens of n characters (rounded up).
func estimateTokens(n int) int {
	return (n + 3) / 4
}

// closeOpenCalls appends a cancelled tool result for every tool call of the
// last assistant message that has no result yet, so the history stays valid.
func closeOpenCalls(hist []provider.Message) []provider.Message {
	last := -1
	for i := len(hist) - 1; i >= 0; i-- {
		if hist[i].Role == "assistant" {
			last = i
			break
		}
	}
	if last < 0 || len(hist[last].ToolCalls) == 0 {
		return hist
	}
	answered := map[string]bool{}
	for _, m := range hist[last+1:] {
		if m.Role == "tool" {
			answered[m.ToolCallID] = true
		}
	}
	for _, tc := range hist[last].ToolCalls {
		if !answered[tc.ID] {
			hist = append(hist, provider.Message{Role: "tool", Content: CancelledMessage, ToolCallID: tc.ID})
			answered[tc.ID] = true
		}
	}
	return hist
}
