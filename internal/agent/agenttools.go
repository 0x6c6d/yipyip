package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"yipyip/internal/provider"
	"yipyip/internal/tools"
)

// Agent tool names.
const (
	toolSpawnAgent  = "spawn_agent"
	toolWaitAgent   = "wait_agent"
	toolCancelAgent = "cancel_agent"
)

const (
	spawnAgentSchema  = `{"type":"object","properties":{"task":{"type":"string","description":"The complete, self-contained task for the subagent."},"model":{"type":"string","description":"Optional model id \"provider/model\"; default: the current model."}},"required":["task"]}`
	agentIDSchema     = `{"type":"object","properties":{"id":{"type":"string","description":"The subagent id, e.g. a1."}},"required":["id"]}`
	maxTranscriptLine = 200 // characters of a shortened transcript line
)

var (
	spawnAgentDef = provider.ToolDef{
		Name:        toolSpawnAgent,
		Description: "Start a subagent that works on a task in parallel. Returns its id at once; use wait_agent to get its final answer.",
		Parameters:  json.RawMessage(spawnAgentSchema),
	}
	waitAgentDef = provider.ToolDef{
		Name:        toolWaitAgent,
		Description: "Wait until a subagent has finished and return its final answer.",
		Parameters:  json.RawMessage(agentIDSchema),
	}
	cancelAgentDef = provider.ToolDef{
		Name:        toolCancelAgent,
		Description: "Cancel a running subagent.",
		Parameters:  json.RawMessage(agentIDSchema),
	}
)

// spawnedSubs records the subagents spawned during one turn.
type spawnedSubs struct {
	mu  sync.Mutex
	ids []string
}

func (p *spawnedSubs) add(id string) {
	p.mu.Lock()
	p.ids = append(p.ids, id)
	p.mu.Unlock()
}

// wait blocks until every recorded subagent of mgr has ended. The caller has
// cancelled their parent context, so this is bounded.
func (p *spawnedSubs) wait(mgr *Manager) {
	p.mu.Lock()
	ids := append([]string(nil), p.ids...)
	p.mu.Unlock()
	for _, id := range ids {
		_, _ = mgr.Wait(context.Background(), id)
	}
}

// newManager returns a subagent manager whose subagents belong to session
// generation gen. Each status change is reported as EvSubagent.
func (s *Session) newManager(gen uint64) *Manager {
	var (
		mu   sync.Mutex
		last = map[string]SubStatus{}
	)
	onChange := func(info SubInfo) {
		mu.Lock()
		changed := last[info.ID] != info.Status
		last[info.ID] = info.Status
		mu.Unlock()
		if changed {
			s.emit(Event{Kind: EvSubagent, AgentID: info.ID, Text: info.ID + " " + string(info.Status)})
		}
	}
	run := func(ctx context.Context, a *Subagent, task, model string) (string, error) {
		return s.runSubagent(ctx, gen, a, task, model)
	}
	return NewManager(s.cfg.MaxSubagents, s.redactPanics(run), onChange)
}

// redactPanics wraps run so that a panic is re-raised with its text redacted:
// the Manager records the panic text as the subagent's error.
func (s *Session) redactPanics(run RunFunc) RunFunc {
	return func(ctx context.Context, a *Subagent, task, model string) (string, error) {
		defer func() {
			if p := recover(); p != nil {
				panic(s.Redact(fmt.Sprint(p)))
			}
		}()
		return run(ctx, a, task, model)
	}
}

// runSubagent runs one subagent's loop on a fresh history.
func (s *Session) runSubagent(ctx context.Context, gen uint64, a *Subagent, task, model string) (string, error) {
	sys := s.SystemPrompt() + "\n\nYou are subagent " + a.ID() + "; finish with a short summary."
	hist := []provider.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: task},
	}
	ts := s.subTools(a)
	ts.model = model
	ts.sub = a
	_, final, err := s.runLoop(ctx, gen, a.ID(), hist, ts)
	if err != nil {
		s.subLog(a, "error: "+shortLine(err.Error()))
		if ctx.Err() == nil {
			err = errors.New(s.Redact(err.Error()))
		}
	}
	return s.Redact(final), err
}

// registryDefs returns the definitions of all registry tools.
func (s *Session) registryDefs() []provider.ToolDef {
	return s.mainTools().defs
}

// turnTools returns the main agent's tools for one turn: the registry tools
// plus spawn_agent, wait_agent and cancel_agent. Subagents spawned are
// recorded in spawned and run under the turn context.
func (s *Session) turnTools(mgr *Manager, spawned *spawnedSubs) toolSet {
	defs := append(s.registryDefs(), spawnAgentDef, waitAgentDef, cancelAgentDef)
	return toolSet{
		defs: defs,
		run: func(ctx context.Context, name, args string) tools.Result {
			switch name {
			case toolSpawnAgent:
				return s.spawnTool(ctx, mgr, spawned, args)
			case toolWaitAgent:
				return s.waitTool(ctx, mgr, MainAgentID, args)
			case toolCancelAgent:
				return s.cancelTool(ctx, mgr, args)
			}
			return s.registry.Run(ctx, name, json.RawMessage(args))
		},
	}
}

// subTools returns a subagent's tools: the registry tools plus wait_agent
// and cancel_agent. Subagents cannot spawn subagents.
func (s *Session) subTools(a *Subagent) toolSet {
	defs := append(s.registryDefs(), waitAgentDef, cancelAgentDef)
	return toolSet{
		defs: defs,
		run: func(ctx context.Context, name, args string) tools.Result {
			switch name {
			case toolSpawnAgent:
				return errorResult("subagents cannot spawn subagents")
			case toolWaitAgent:
				return s.waitTool(ctx, a.m, a.ID(), args)
			case toolCancelAgent:
				return s.cancelTool(ctx, a.m, args)
			}
			return s.registry.Run(ctx, name, json.RawMessage(args))
		},
	}
}

// spawnTool runs spawn_agent: it validates the arguments and the model id,
// starts the subagent under ctx (the turn context) and returns at once.
func (s *Session) spawnTool(ctx context.Context, mgr *Manager, spawned *spawnedSubs, args string) tools.Result {
	var a struct {
		Task  *string `json:"task"`
		Model *string `json:"model"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return errorResult("invalid arguments: %v", err)
	}
	if a.Task == nil || strings.TrimSpace(*a.Task) == "" {
		return errorResult("invalid arguments: missing required argument \"task\"")
	}
	model := ""
	if a.Model != nil {
		model = strings.TrimSpace(*a.Model)
	}
	if model == "" {
		model = s.ModelID()
	} else if _, _, ok := s.cfg.Lookup(model); !ok {
		return errorResult("unknown model %q (configured: %s)", model, strings.Join(s.Models(), ", "))
	}
	id, err := mgr.Spawn(ctx, *a.Task, model)
	if err != nil {
		return errorResult("%v", err)
	}
	spawned.add(id)
	return tools.Result{Output: "subagent " + id + " started"}
}

// waitTool runs wait_agent for the agent self: it blocks until the subagent
// has ended or ctx is done and returns its final text.
func (s *Session) waitTool(ctx context.Context, mgr *Manager, self, args string) tools.Result {
	id, res, ok := idArg(args)
	if !ok {
		return res
	}
	if id == self {
		return errorResult("a subagent cannot wait for itself")
	}
	final, err := mgr.Wait(ctx, id)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return errorResult("%s", CancelledMessage)
		}
		return errorResult("%s", s.Redact(err.Error()))
	}
	if final == "" {
		return tools.Result{Output: "subagent " + id + " finished without a final text"}
	}
	return tools.Result{Output: s.Redact(final)}
}

// cancelTool runs cancel_agent: it cancels the subagent and waits (bounded
// by ctx) until it has ended.
func (s *Session) cancelTool(ctx context.Context, mgr *Manager, args string) tools.Result {
	id, res, ok := idArg(args)
	if !ok {
		return res
	}
	if err := mgr.Cancel(id); err != nil {
		return errorResult("%s", s.Redact(err.Error()))
	}
	_, _ = mgr.Wait(ctx, id)
	return tools.Result{Output: "cancelled " + id}
}

// idArg extracts the required "id" argument.
func idArg(args string) (string, tools.Result, bool) {
	var a struct {
		ID *string `json:"id"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return "", errorResult("invalid arguments: %v", err), false
	}
	if a.ID == nil || strings.TrimSpace(*a.ID) == "" {
		return "", errorResult("invalid arguments: missing required argument \"id\""), false
	}
	return strings.TrimSpace(*a.ID), tools.Result{}, true
}

// decodeArgs decodes JSON object arguments into v.
func decodeArgs(args string, v any) error {
	if strings.TrimSpace(args) == "" {
		return errors.New("expected a JSON object, got none")
	}
	if err := json.Unmarshal([]byte(args), v); err != nil {
		return fmt.Errorf("expected a JSON object: %w", err)
	}
	return nil
}

func errorResult(format string, a ...any) tools.Result {
	return tools.Result{Output: fmt.Sprintf(format, a...), IsError: true}
}

// subLog appends a redacted transcript line to sub (no-op when nil).
func (s *Session) subLog(sub *Subagent, line string) {
	if sub != nil {
		sub.Log(s.Redact(line))
	}
}

// shortLine returns text on one line, shortened to maxTranscriptLine characters.
func shortLine(text string) string {
	line := strings.Join(strings.Fields(text), " ")
	if short := truncateChars(line, maxTranscriptLine); short != line {
		return short + "…"
	}
	return line
}

// currentSubs returns the subagent manager of the current generation.
func (s *Session) currentSubs() *Manager {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.subs
}

// Subagents returns snapshots of the subagents of this conversation in spawn
// order.
func (s *Session) Subagents() []SubInfo {
	return s.currentSubs().List()
}

// SubagentTranscript returns the transcript lines of subagent id.
func (s *Session) SubagentTranscript(id string) ([]string, bool) {
	return s.currentSubs().Transcript(id)
}

// CancelSubagent cancels subagent id. Unknown ids are an error.
func (s *Session) CancelSubagent(id string) error {
	return s.currentSubs().Cancel(id)
}
