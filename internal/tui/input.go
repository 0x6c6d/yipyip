package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var ghostStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

// suggestion returns the autosuggest ghost text for the input line. ghost is
// what is shown after the cursor; accept is the full input after accepting it
// ("" when the ghost is only a hint). Both are empty unless the input is one
// line starting with "/", the cursor is at its end, and no approval prompt or
// model picker is open.
func (m Model) suggestion() (ghost string, accept string) {
	if len(m.pending) > 0 || m.picker.open {
		return "", ""
	}
	v := m.input.Value()
	if !strings.HasPrefix(v, "/") || strings.Contains(v, "\n") {
		return "", ""
	}
	if li := m.input.LineInfo(); m.input.Line() != 0 || li.RowOffset != 0 || li.ColumnOffset != utf8.RuneCountInString(v) {
		return "", ""
	}

	name, arg, hasArg := strings.Cut(v, " ")
	if !hasArg {
		for _, c := range commands {
			if strings.HasPrefix(c.Name, v) && c.Name != v {
				return c.Name[len(v):], c.Name
			}
		}
		return "", ""
	}

	var values []string
	var hint string
	switch name {
	case "/effort":
		values, hint = []string{"low", "medium", "high"}, "[low|medium|high]"
	case "/mode":
		values, hint = []string{"auto", "manual"}, "[auto|manual]"
	case "/model":
		if sess := m.opts.Session; sess != nil {
			values = sess.Models()
		}
		hint = "[provider/model]"
	default:
		return "", ""
	}
	if arg == "" {
		return hint, ""
	}
	for _, c := range values {
		if strings.HasPrefix(c, arg) && c != arg {
			return c[len(arg):], name + " " + c
		}
	}
	return "", ""
}

// acceptSuggestion sets the input to the accepted suggestion, cursor at the
// end. It reports false when there is nothing acceptable.
func (m Model) acceptSuggestion() (Model, bool) {
	_, accept := m.suggestion()
	if accept == "" {
		return m, false
	}
	m.input.SetValue(accept)
	m.input.CursorEnd()
	return m, true
}

// withGhost draws the suggestion ghost text right after the typed text on the
// first line of the rendered input, without changing its size.
func (m Model) withGhost(view string) string {
	ghost, _ := m.suggestion()
	if ghost == "" {
		return view
	}
	first, rest, hasRest := strings.Cut(view, "\n")
	col := lipgloss.Width(m.input.Value())
	room := m.input.Width() - col - 1 // the cursor cell comes first
	if room <= 0 {
		return view
	}
	ghost = ansi.Truncate(ghost, room, "")
	gw := lipgloss.Width(ghost)
	out := ansi.Truncate(first, col+1, "") + ghostStyle.Render(ghost) + ansi.TruncateLeft(first, col+1+gw, "")
	if hasRest {
		out += "\n" + rest
	}
	return out
}

// maxHistory is the number of submitted inputs kept for Up/Down recall.
const maxHistory = 500

// inputHistory holds the submitted inputs, oldest first, for Up/Down recall.
// It lives in memory only and is shared (by pointer) by all copies of a Model,
// so /new and /clear keep it. pos is the entry shown while navigating;
// pos == len(entries) means not navigating. draft is the input saved by the
// first Up, restored by Down past the newest entry.
type inputHistory struct {
	entries []string
	pos     int
	draft   string
}

// push records a submitted input (already trimmed). A value equal to the
// newest entry is not stored again; the oldest entries are dropped beyond
// maxHistory. It ends any navigation.
func (h *inputHistory) push(s string) {
	if s != "" && (len(h.entries) == 0 || h.entries[len(h.entries)-1] != s) {
		h.entries = append(h.entries, s)
		if n := len(h.entries) - maxHistory; n > 0 {
			h.entries = append([]string(nil), h.entries[n:]...)
		}
	}
	h.pos = len(h.entries)
	h.draft = ""
}

// navigating reports whether an entry (not the draft) is being shown.
func (h *inputHistory) navigating() bool { return h.pos < len(h.entries) }

// setInput places s in the input with the cursor at its end.
func (m *Model) setInput(s string) {
	m.input.SetValue(s)
	m.input.CursorEnd()
}

// historyUp shows the next older history entry when the input is empty or the
// cursor is on its first line. It reports false when the key should move the
// cursor instead.
func (m Model) historyUp() (Model, bool) {
	h := m.hist
	if h == nil || len(h.entries) == 0 || (m.input.Value() != "" && m.input.Line() != 0) {
		return m, false
	}
	if !h.navigating() {
		h.pos = len(h.entries)
		h.draft = m.input.Value()
	}
	if h.pos > 0 {
		h.pos--
	}
	m.setInput(h.entries[h.pos])
	return m, true
}

// historyDown shows the next newer history entry, or the saved draft past the
// newest one, when navigating and the input is empty or the cursor is on its
// last line. It reports false when the key should move the cursor instead.
func (m Model) historyDown() (Model, bool) {
	h := m.hist
	if h == nil || !h.navigating() || (m.input.Value() != "" && m.input.Line() != m.input.LineCount()-1) {
		return m, false
	}
	h.pos++
	if h.navigating() {
		m.setInput(h.entries[h.pos])
	} else {
		m.setInput(h.draft)
		h.draft = ""
	}
	return m, true
}
