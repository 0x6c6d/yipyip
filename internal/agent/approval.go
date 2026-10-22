// Package agent implements the agent loop, modes and approvals, subagents,
// compaction and undo.
package agent

import (
	"context"
	"sync"
)

// Mode is the approval mode of a session.
type Mode string

// Approval modes. Any value other than ModeAuto is treated like ModeManual.
const (
	ModeManual Mode = "manual"
	ModeAuto   Mode = "auto"
)

// Decision is the user's answer to an approval request.
type Decision int

// Decisions. Any value outside this set is treated as Deny.
const (
	Deny        Decision = iota // "n"
	Allow                       // "y"
	AllowAlways                 // "a": always for this tool this session
)

// DeniedMessage is the tool result reported to the model for a denied call.
const DeniedMessage = "denied by user"

// MainAgentID labels requests coming from the main agent.
const MainAgentID = "main"

// ApprovalRequest is one pending approval prompt for a gated tool call.
type ApprovalRequest struct {
	AgentID string // MainAgentID or a subagent id like "a1"
	Tool    string
	Args    string // raw JSON arguments as sent by the model

	reply chan Decision // buffered 1
	once  sync.Once
}

// Respond answers the request. It never blocks; the first call wins and later
// calls are ignored. Responding to a request not created by a Gate is a no-op.
func (r *ApprovalRequest) Respond(d Decision) {
	if r == nil || r.reply == nil {
		return
	}
	r.once.Do(func() {
		select {
		case r.reply <- d:
		default:
		}
	})
}

// gatedTools are the tools that need approval in manual mode.
var gatedTools = map[string]bool{
	"bash":        true,
	"write_file":  true,
	"edit_file":   true,
	"spawn_agent": true,
}

// IsGated reports whether tool needs approval in manual mode.
func IsGated(tool string) bool {
	return gatedTools[tool]
}

// riskScreened are the tools whose auto-mode calls the risk screen may turn
// into a prompt.
var riskScreened = map[string]bool{
	"bash":       true,
	"write_file": true,
	"edit_file":  true,
}

// Gate decides whether a tool call may run. It is the only place where
// approvals are decided and is shared by the main agent and all subagents.
type Gate struct {
	notify func(*ApprovalRequest)

	mu     sync.Mutex
	always map[string]bool
	risk   func(ctx context.Context, tool, args string) bool
}

// NewGate returns a Gate that reports each prompt to notify. notify must not
// block; it may call Respond at once or later from another goroutine. A nil
// notify denies every prompt.
func NewGate(notify func(*ApprovalRequest)) *Gate {
	return &Gate{notify: notify, always: map[string]bool{}}
}

// SetRiskScreen installs fn (nil removes it). fn is consulted only in auto mode,
// only for bash, write_file and edit_file, and only to ADD a prompt.
// When fn reports true the call is prompted like in manual mode, except that
// AllowAlways counts as Allow and is never remembered. A false result or a
// panic in fn leaves the auto-mode approval unchanged.
func (g *Gate) SetRiskScreen(fn func(ctx context.Context, tool, args string) bool) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.risk = fn
	g.mu.Unlock()
}

// Check reports whether the call of tool by agentID with args may run.
// It fails closed: anything other than an explicit Allow or AllowAlways for a
// gated tool in a non-auto mode yields false. In auto mode a gated call runs
// unless the risk screen (see SetRiskScreen) flags it; a flagged call then
// needs an explicit Allow or AllowAlways, and its AllowAlways is not stored.
func (g *Gate) Check(ctx context.Context, mode Mode, agentID, tool, args string) (bool, error) {
	if !IsGated(tool) {
		return true, nil
	}
	if g == nil {
		return false, nil
	}
	if mode == ModeAuto {
		if !g.risky(ctx, tool, args) {
			return true, nil
		}
		return g.prompt(ctx, agentID, tool, args, false)
	}
	if g.isAlways(tool) {
		return true, nil
	}
	return g.prompt(ctx, agentID, tool, args, true)
}

// risky reports whether the risk screen flags an auto-mode call of tool. It
// is false for tools outside riskScreened, without a screen, and when the
// screen panics. The mutex is not held while the screen runs.
func (g *Gate) risky(ctx context.Context, tool, args string) (flagged bool) {
	if !riskScreened[tool] {
		return false
	}
	g.mu.Lock()
	fn := g.risk
	g.mu.Unlock()
	if fn == nil {
		return false
	}
	defer func() {
		if recover() != nil {
			flagged = false
		}
	}()
	return fn(ctx, tool, args)
}

// prompt asks notify about the call and waits for the answer or ctx. It fails
// closed. AllowAlways is remembered for tool only when storeAlways is set.
func (g *Gate) prompt(ctx context.Context, agentID, tool, args string, storeAlways bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if g.notify == nil {
		return false, nil
	}
	req := &ApprovalRequest{
		AgentID: agentID,
		Tool:    tool,
		Args:    args,
		reply:   make(chan Decision, 1),
	}
	g.notify(req)

	var d Decision
	select {
	case d = <-req.reply:
	case <-ctx.Done():
		req.Respond(Deny) // close the request so a late answer is ignored
		return false, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	switch d {
	case Allow:
		return true, nil
	case AllowAlways:
		if !storeAlways {
			return true, nil
		}
		g.mu.Lock()
		if g.always == nil {
			g.always = map[string]bool{}
		}
		g.always[tool] = true
		g.mu.Unlock()
		return true, nil
	default:
		return false, nil
	}
}

func (g *Gate) isAlways(tool string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.always[tool]
}

// Reset forgets every "always" answer (used by /new).
func (g *Gate) Reset() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.always = map[string]bool{}
	g.mu.Unlock()
}
