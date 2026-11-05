package session

import "yipyip/internal/agent"

// FromAgent builds the session data of sess: ID = id, Created = created,
// StartDir, Model, Effort, Mode, token counts, LastContext, Conversation
// (every history entry incl. tool calls with Type "function"), Title (Title
// of the first user message) and Turn. A turn still running is stored as
// finished and interrupted, never resumable. TUI-only fields stay empty but
// non-nil: History, Pastes (empty Entries map) and Queue. ok is false when
// the history has no user message (nothing to save).
func FromAgent(sess *agent.Session, id, created string) (d *SessionData, ok bool) {
	if sess == nil {
		return nil, false
	}
	hist := sess.History()
	// The history starts with the system prompt: a conversation is empty
	// until the first user message.
	title, hasUser := "", false
	for _, h := range hist {
		if h.Role == "user" {
			title, hasUser = Title(h.Content), true
			break
		}
	}
	if !hasUser {
		return nil, false
	}

	d = NewSessionData()
	d.ID = id
	d.Created = created
	d.StartDir = sess.StartDir()
	d.Model = sess.ModelName()
	d.Effort = sess.Effort()
	d.Mode = string(sess.Mode())
	d.InTokens, d.OutTokens, d.TotalTokens = sess.TokenCounts()
	d.LastContext = sess.LastContextTokens()

	d.Conversation = make([]Conversation, len(hist))
	for i, h := range hist {
		c := Conversation{Role: h.Role, Content: h.Content, ToolCallID: h.ToolCallID}
		for _, tc := range h.ToolCalls {
			c.ToolCalls = append(c.ToolCalls, ToolCall{
				ID:       tc.ID,
				Type:     "function",
				Function: Func{Name: tc.Name, Arguments: tc.Arguments},
			})
		}
		d.Conversation[i] = c
	}
	d.Title = title

	d.History = []string{}
	d.Pastes = Pastes{Entries: map[string]string{}}
	d.Queue = []string{}

	active, started, interrupted := sess.TurnState()
	d.Turn = Turn{Started: started, Interrupted: interrupted || active}
	return d, true
}

// ApplyTo restores d into sess: the conversation (RestoreHistory) and the
// token counts (RestoreTokens). It does not change model, effort or mode:
// callers decide.
func ApplyTo(d *SessionData, sess *agent.Session) {
	if d == nil || sess == nil {
		return
	}
	hist := make([]agent.HistoryEntry, len(d.Conversation))
	for i, c := range d.Conversation {
		h := agent.HistoryEntry{Role: c.Role, Content: c.Content, ToolCallID: c.ToolCallID}
		for _, tc := range c.ToolCalls {
			h.ToolCalls = append(h.ToolCalls, agent.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
		}
		hist[i] = h
	}
	sess.RestoreHistory(hist)
	sess.RestoreTokens(d.InTokens, d.OutTokens, d.TotalTokens, d.LastContext)
}
