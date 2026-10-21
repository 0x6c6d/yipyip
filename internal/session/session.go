// Package session persists conversations to per-start-directory session
// files under <startdir>/.yipyip/sessions/<id>.json (v3, R29).
package session

import (
	"strings"
	"time"
)

// FileVersion is the session file format version; it is always 1 in v3.
const FileVersion = 1

// now is the clock the store stamps files with; tests stub it. Package-level
// so Save can be driven without sleeping.
var now = time.Now

// Conversation is one entry of the saved conversation: the message as sent
// to the model, un-redacted (R33). It is weakly typed so that every field of
// the agent history (role, content, tool calls and tool results) round-trips
// and old or hand-edited files load without a crash.
type Conversation struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is one complete function call the model requested.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type,omitempty"`
	Function Func   `json:"function"`
}

// Func is the function part of a tool call (name and raw JSON arguments).
type Func struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Pastes is the persisted paste store: the next paste number and every
// placeholder mapped to its pasted text ({"n": N, "entries": {...}}).
type Pastes struct {
	N       int               `json:"n"`
	Entries map[string]string `json:"entries"`
}

// Turn is the stored state of the running or last turn. An interrupted turn
// is stored with Interrupted true and is never resumed (its tool calls may
// have written files, A5).
type Turn struct {
	Active      string `json:"active"`
	Started     string `json:"started"`
	Interrupted bool   `json:"interrupted"`
}

// SessionData is one persisted session: exactly the R29 field set. Mode,
// effort and model are stored as strings (the agent values are stringable);
// Created, Updated and Turn.Started are time-ish strings (RFC 3339).
type SessionData struct {
	Version      int            `json:"version"`
	ID           string         `json:"id"`
	Created      string         `json:"created"`
	Updated      string         `json:"updated"`
	StartDir     string         `json:"startdir"`
	Model        string         `json:"model"`
	Effort       string         `json:"effort"`
	Mode         string         `json:"mode"`
	InTokens     int64          `json:"in_tokens"`
	OutTokens    int64          `json:"out_tokens"`
	TotalTokens  int64          `json:"total_tokens"`
	LastContext  int            `json:"last_context"`
	Conversation []Conversation `json:"conversation"`
	History      []string       `json:"history"`
	Pastes       Pastes         `json:"pastes"`
	Queue        []string       `json:"queue"`
	Title        string         `json:"title"`
	Turn         Turn           `json:"turn"`
}

// NewSessionData returns SessionData with Version set and Created/Updated
// stamped now (the Store re-stamps Updated on every Save).
func NewSessionData() *SessionData {
	ts := now().UTC().Format(time.RFC3339)
	return &SessionData{Version: FileVersion, Created: ts, Updated: ts}
}

// Title derives a session title from the first user message: newlines
// collapsed to spaces, then the first 60 runes. Empty input gives an empty
// title.
func Title(firstUserMessage string) string {
	oneLine := strings.Join(strings.FieldsFunc(firstUserMessage, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\v' || r == '\f'
	}), " ")
	return truncateRunes(oneLine, 60)
}

// truncateRunes cuts s to at most max runes (max < 0 means 0).
func truncateRunes(s string, max int) string {
	if max < 0 {
		max = 0
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
