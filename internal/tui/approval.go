package tui

import (
	"encoding/json"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yipyip/internal/agent"
)

const (
	approvalHint     = "y / n / a (always for this tool this session)"
	approvalMaxRunes = 200 // longest args summary
	approvalMaxLines = 4   // most lines of a multi-line command shown
)

var approvalStyle = lipgloss.NewStyle().Bold(true)

// enqueueApproval appends req to the pending queue. The slice is copied so
// earlier copies of the Model do not see the new entry.
func (m *Model) enqueueApproval(req *agent.ApprovalRequest) {
	next := make([]*agent.ApprovalRequest, 0, len(m.pending)+1)
	next = append(next, m.pending...)
	m.pending = append(next, req)
}

// denyPending answers Deny to every pending request and empties the queue.
// It is only called in direct reaction to a key press of the user.
func (m *Model) denyPending() {
	for _, r := range m.pending {
		r.Respond(agent.Deny)
	}
	m.pending = nil
}

// approvalKey handles one key press while a request is pending. Ctrl+C is not
// handled here (the caller keeps the normal rule); every other key is
// swallowed, so nothing reaches the input.
func (m Model) approvalKey(k tea.KeyMsg) (Model, tea.Cmd) {
	var d agent.Decision
	switch k.String() {
	case "y":
		d = agent.Allow
	case "n":
		d = agent.Deny
	case "a":
		d = agent.AllowAlways
	case "esc":
		m.denyPending()
		if sess := m.sess; sess != nil {
			sess.Cancel()
		}
		return m, nil
	default:
		return m, nil
	}
	first := m.pending[0]
	m.pending = append([]*agent.ApprovalRequest(nil), m.pending[1:]...)
	if len(m.pending) == 0 {
		m.pending = nil
	}
	first.Respond(d)
	return m, nil
}

// shortenText limits s to maxLines lines and maxRunes runes in total, marking
// a cut with an ellipsis.
func shortenText(s string, maxLines, maxRunes int) string {
	s = strings.TrimSpace(s)
	cut := false
	if lines := strings.Split(s, "\n"); len(lines) > maxLines {
		s = strings.Join(lines[:maxLines], "\n")
		cut = true
	}
	if r := []rune(s); len(r) > maxRunes {
		s = string(r[:maxRunes])
		cut = true
	}
	if cut {
		s += "…"
	}
	return s
}

// argsSummary returns the text shown for a request: the command for bash,
// the path for file tools, otherwise the raw arguments.
func argsSummary(tool, args string) string {
	var fields map[string]any
	if json.Unmarshal([]byte(args), &fields) == nil {
		key := ""
		switch tool {
		case "bash":
			key = "command"
		case "write_file", "edit_file", "read_file":
			key = "path"
		}
		if s, ok := fields[key].(string); ok && key != "" {
			return s
		}
	}
	return args
}

// approvalLines renders the prompt block for the first pending request:
// header (possibly several lines) and the hint line. All text is redacted
// before it is shortened.
func (m Model) approvalLines() []string {
	if len(m.pending) == 0 {
		return nil
	}
	r := m.pending[0]
	summary := shortenText(m.redact(argsSummary(r.Tool, r.Args)), approvalMaxLines, approvalMaxRunes)
	head := "[" + m.redact(r.AgentID) + "] " + m.redact(r.Tool) + ": " + summary
	var rows []string
	for _, l := range strings.Split(head, "\n") {
		rows = append(rows, approvalStyle.MaxWidth(m.width).Render(l))
	}
	hint := approvalHint
	if n := len(m.pending) - 1; n > 0 {
		hint += " [+" + strconv.Itoa(n) + " pending]"
	}
	return append(rows, infoStyle.MaxWidth(m.width).Render(hint))
}

// overlayApproval replaces the bottom lines of the viewport view vp with the
// prompt block (keeping at least one viewport line).
func (m Model) overlayApproval(vp string) string {
	rows := m.approvalLines()
	lines := strings.Split(vp, "\n")
	if max := len(lines) - 1; len(rows) > max {
		if max < 0 {
			max = 0
		}
		rows = rows[len(rows)-max:]
	}
	keep := len(lines) - len(rows)
	if keep < 0 {
		keep = 0
	}
	return strings.Join(append(append([]string(nil), lines[:keep]...), rows...), "\n")
}
