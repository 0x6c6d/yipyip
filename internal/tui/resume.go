package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/agent"
	"yipyip/internal/session"
)

// resumeMaxRows is the most session rows the resume picker shows at once.
const resumeMaxRows = 12

var (
	resumeBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	resumeTitle  = lipgloss.NewStyle().Bold(true)
	resumeDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	resumeCursor = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
)

// resumePicker is the state of the session picker (`yip --resume` without
// id). It is separate from the /model picker and holds plain values only, so
// copies of the Model never share mutable state with it.
type resumePicker struct {
	open     bool
	sessions []session.SessionData // newest first, as Store.List returned them
	cursor   int
	top      int // index of the first visible row
}

// openResumePicker loads the saved sessions through the Store and opens the
// picker. With no saved session (or an unreadable store) nothing opens and
// the start stays a fresh session.
func (m *Model) openResumePicker() {
	store, err := session.NewStore(m.opts.StartDir)
	if err != nil {
		m.add(blockError, "could not list sessions: "+m.redact(err.Error()))
		return
	}
	list, err := store.List()
	if err != nil {
		m.add(blockError, "could not list sessions: "+m.redact(err.Error()))
		return
	}
	if len(list) == 0 {
		return
	}
	m.resume = resumePicker{open: true, sessions: list}
}

// resumeRows is the number of rows that fit in the viewport area.
func (m Model) resumeRows() int {
	n := (m.viewport.Height - 4) / 2 // border, title and footer take 4 lines
	return max(1, min(resumeMaxRows, n))
}

// resumeKey handles one key while the resume picker is open. Ctrl+C is not
// handled here (the caller keeps the normal rule).
func (m Model) resumeKey(k tea.KeyMsg) (Model, tea.Cmd) {
	r := &m.resume
	switch k.String() {
	case "up", "k":
		if r.cursor > 0 {
			r.cursor--
		}
	case "down", "j":
		if r.cursor < len(r.sessions)-1 {
			r.cursor++
		}
	case "esc", "q":
		// Leave the program without opening a session: nothing is saved.
		return m, m.quit()
	case "enter":
		if r.cursor >= 0 && r.cursor < len(r.sessions) {
			data := r.sessions[r.cursor]
			m.resume = resumePicker{}
			m.restore(m.cur, &data)
		}
		return m, nil
	}
	rows := m.resumeRows()
	if r.cursor < r.top {
		r.top = r.cursor
	}
	if r.cursor >= r.top+rows {
		r.top = r.cursor - rows + 1
	}
	return m, nil
}

// relativeTime formats the age of a session as "5m ago", "2h ago", "3d ago".
func relativeTime(now time.Time, stamp string) string {
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return "unknown"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	}
}

// resumeBox renders the picker box: title, two lines per visible session and
// a footer with the scroll indicators.
func (m Model) resumeBox() string {
	r := m.resume
	inner := max(10, min(70, m.width-borderCols-2))
	rows := m.resumeRows()
	end := min(len(r.sessions), r.top+rows)

	lines := []string{resumeTitle.Render("resume session")}
	for i := r.top; i < end; i++ {
		d := r.sessions[i]
		title := m.redact(d.Title)
		if title == "" {
			title = "(untitled)"
		}
		stamp := d.Updated
		if stamp == "" {
			stamp = d.Created
		}
		mark, markStyle := "  ", lipgloss.NewStyle()
		if i == r.cursor {
			mark, markStyle = "> ", resumeCursor
		}
		first := ansi.Truncate(title, inner-2, "…")
		detail := fmt.Sprintf("%s · %d toks · %s", relativeTime(m.now(), stamp), d.TotalTokens, m.redact(d.Model))
		lines = append(lines,
			markStyle.Render(mark)+resumeTitle.Render(first),
			"  "+resumeDim.Render(ansi.Truncate(detail, inner-2, "…")))
	}
	var foot []string
	if r.top > 0 {
		foot = append(foot, "▲ more")
	}
	if end < len(r.sessions) {
		foot = append(foot, "▼ more")
	}
	foot = append(foot, "enter resume · esc quit")
	lines = append(lines, resumeDim.Render(strings.Join(foot, " · ")))
	return resumeBorder.Width(inner).Render(strings.Join(lines, "\n"))
}

// overlayResume draws the picker box centered over the dimmed viewport view
// vp. The height stays that of vp.
func (m Model) overlayResume(vp string) string {
	return m.overlayCentered(vp, m.resumeBox())
}

// overlayCentered draws the box (a rendered multi-line string) centered over
// the dimmed viewport view vp. The height stays that of vp.
func (m Model) overlayCentered(vp, boxText string) string {
	box := strings.Split(boxText, "\n")
	lines := strings.Split(vp, "\n")
	w := m.width
	bw := lipgloss.Width(box[0])
	left := max(0, (w-bw)/2)
	top := max(0, (len(lines)-len(box))/2)
	dim := func(s string) string {
		if s == "" {
			return ""
		}
		return dimStyle.Render(s)
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		bg := ansi.Strip(l)
		bg += strings.Repeat(" ", max(0, w-ansi.StringWidth(bg)))
		if i < top || i-top >= len(box) {
			out[i] = dim(bg)
			continue
		}
		out[i] = dim(ansi.Cut(bg, 0, left)) + box[i-top] + dim(ansi.Cut(bg, left+bw, max(w, left+bw)))
	}
	if len(box) > len(lines) { // tiny terminal: the box wins over the frame
		return strings.Join(box[:len(lines)], "\n")
	}
	return strings.Join(out, "\n")
}

// restore installs a saved session into the live run: agent state through the
// T102 restore setters, TUI state (history, pastes, queue as draft) from the
// file, and the conversation blocks rebuilt from the history. The turn is
// restored as finished: no Thinking line, no finished marker and no other
// live turn state comes back (an interrupted turn stays stored-finished).
//
// tab is the index of the target tab; an index out of range changes
// nothing.
func (m *Model) restore(tab int, data *session.SessionData) {
	restored := false
	m.inTab(tab, func(m *Model) {
		m.restoreTab(data)
		restored = m.sess != nil
	})
	// The restored model, effort and mode are saved once, after the
	// restore; a failure line goes to the current tab (see settings.go).
	if restored {
		m.saveSettings()
	}
}

// restoreTab is restore for the live tab.
func (m *Model) restoreTab(data *session.SessionData) {
	m.restoreInto(m.sess, data)

	// TUI-side state.
	if m.hist == nil {
		m.hist = &inputHistory{}
	}
	m.hist.entries = slices.Clone(data.History)
	m.hist.pos, m.hist.draft = len(m.hist.entries), ""
	if m.pastes == nil {
		m.pastes = &pasteStore{}
	}
	m.pastes.n = data.Pastes.N
	m.pastes.texts = map[string]string{}
	for ph, text := range data.Pastes.Entries {
		m.pastes.texts[ph] = text
	}
	m.queue = nil
	if m.background {
		// A tab that is not current keeps the queue as its parked draft;
		// the input box belongs to the current tab.
		m.draft = strings.Join(data.Queue, "\n")
	} else {
		m.input.Reset()
		if len(data.Queue) > 0 {
			m.setInput(strings.Join(data.Queue, "\n"))
		}
	}

	// Later saves overwrite the opened file.
	if m.saved == nil {
		m.saved = &saveState{}
	}
	m.saved.id, m.saved.created = data.ID, data.Created

	// Live turn state: none.
	m.turnActive, m.interrupted, m.openCalls, m.streaming = false, false, 0, false
	m.pending, m.turnStarts, m.ctrlCAt = nil, nil, time.Time{}
	m.cancelQuestions()

	m.blocks = restoreBlocks(data.Conversation)
	m.refreshTo(true)
}

// restoreInto pushes the stored conversation, tokens, model, effort and mode
// into the agent session (nil = none). The stored model, effort and mode
// become the global selection: they are set on the sessions of all tabs.
func (m *Model) restoreInto(sess *agent.Session, data *session.SessionData) {
	if sess == nil {
		return
	}
	hist := make([]agent.HistoryEntry, len(data.Conversation))
	for i, c := range data.Conversation {
		h := agent.HistoryEntry{Role: c.Role, Content: c.Content, ToolCallID: c.ToolCallID}
		for _, tc := range c.ToolCalls {
			h.ToolCalls = append(h.ToolCalls, agent.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
		}
		hist[i] = h
	}
	sess.RestoreHistory(hist)
	sess.RestoreTokens(data.InTokens, data.OutTokens, data.TotalTokens, data.LastContext)

	mode := agent.Mode(data.Mode)
	if mode != agent.ModeAuto {
		mode = agent.ModeManual
	}
	effort := data.Effort
	if effort == "" {
		effort = sess.Effort()
	}
	m.setGlobals(data.Model, effort, string(mode))
}

// restoreBlocks rebuilds the conversation blocks from stored messages. Lines
// that look like live turn markers ("Thinking…", "✻ Worked for ...") are
// turn state, not conversation, and are dropped.
func restoreBlocks(conv []session.Conversation) []block {
	var blocks []block
	calls := map[string]*toolCall{}
	for _, c := range conv {
		switch c.Role {
		case "user":
			if text := dropLiveMarkers(c.Content); strings.TrimSpace(text) != "" {
				blocks = append(blocks, block{kind: blockPlain, text: "> " + text})
			}
		case "assistant":
			if text := dropLiveMarkers(c.Content); strings.TrimSpace(text) != "" {
				blocks = append(blocks, block{kind: blockAssistant, text: text})
			}
			for _, tc := range c.ToolCalls {
				if tc.Function.Name == "spawn_agent" {
					continue
				}
				call := &toolCall{tool: tc.Function.Name, args: tc.Function.Arguments, closed: true}
				calls[tc.ID] = call
				blocks = append(blocks, block{kind: blockTool, call: call})
			}
		case "tool":
			if call := calls[c.ToolCallID]; call != nil && !call.hasRes {
				call.result, call.hasRes = c.Content, true
			}
		}
	}
	return blocks
}

// dropLiveMarkers removes the lines of s that are a busy line or a finished
// marker.
func dropLiveMarkers(s string) string {
	lines := strings.Split(s, "\n")
	kept := lines[:0:0]
	for _, l := range lines {
		t := strings.TrimSpace(l)
		t = strings.TrimSpace(strings.TrimPrefix(t, "✻"))
		if strings.HasPrefix(t, "Thinking…") || strings.HasPrefix(t, "Working…") ||
			strings.HasPrefix(t, "Worked for ") || strings.HasPrefix(t, "Interrupted after ") {
			continue
		}
		kept = append(kept, l)
	}
	return strings.Join(kept, "\n")
}
