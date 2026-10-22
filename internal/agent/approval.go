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
	// web_search sends the query to the search host, so it needs approval.
	"web_search": true,
}

// IsGated reports whether tool needs approval in manual mode.
func IsGated(tool string) bool {
	return gatedTools[tool]
}

// Gate decides whether a tool call may run. It is the only place where
// approvals are decided and is shared by the main agent and all subagents.
type Gate struct {
	notify func(*ApprovalRequest)

	mu     sync.Mutex
	always map[string]bool
}

// NewGate returns a Gate that reports each prompt to notify. notify must not
// block; it may call Respond at once or later from another goroutine. A nil
// notify denies every prompt.
func NewGate(notify func(*ApprovalRequest)) *Gate {
	return &Gate{notify: notify, always: map[string]bool{}}
}

// Check reports whether the call of tool by agentID with args may run.
// It fails closed: anything other than an explicit Allow or AllowAlways for a
// gated tool in a non-auto mode yields false.
func (g *Gate) Check(ctx context.Context, mode Mode, agentID, tool, args string) (bool, error) {
	if !IsGated(tool) {
		return true, nil
	}
	if g == nil {
		return false, nil
	}
	if mode == ModeAuto {
		return true, nil
	}
	if g.isAlways(tool) {
		return true, nil
	}
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
