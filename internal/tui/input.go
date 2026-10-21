package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/agent"
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

// pasteMaxRunes is the longest single-line paste inserted verbatim.
const pasteMaxRunes = 80

// pasteStore maps each paste placeholder to the full pasted text. It lives in
// memory only and is shared (by pointer) by all copies of a Model, so the
// counter is never reset, also not by /new.
type pasteStore struct {
	n     int
	texts map[string]string
}

// add stores text (already normalised) under a new placeholder and returns
// the placeholder.
func (p *pasteStore) add(text string) string {
	p.n++
	body := strings.TrimSuffix(text, "\n")
	var ph string
	if lines := strings.Count(body, "\n") + 1; lines > 1 {
		ph = fmt.Sprintf("[Pasted text #%d +%d lines]", p.n, lines)
	} else {
		ph = fmt.Sprintf("[Pasted text #%d · %d chars]", p.n, utf8.RuneCountInString(body))
	}
	if p.texts == nil {
		p.texts = map[string]string{}
	}
	p.texts[ph] = text
	return ph
}

// expand replaces every exact placeholder in s that exists in the store by its
// full text. Edited placeholders are not in the store and stay as they are.
func (p *pasteStore) expand(s string) string {
	if p == nil || len(p.texts) == 0 || !strings.Contains(s, "[Pasted text #") {
		return s
	}
	pairs := make([]string, 0, 2*len(p.texts))
	for ph, text := range p.texts {
		pairs = append(pairs, ph, text)
	}
	return strings.NewReplacer(pairs...).Replace(s)
}

// normalizePaste turns \r\n and lone \r into \n.
func normalizePaste(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// paste handles a bracketed paste: a large one (a newline or more than
// pasteMaxRunes runes) is inserted as a placeholder at the cursor. It reports
// false when the paste should be inserted verbatim.
func (m Model) paste(k tea.KeyMsg) (Model, bool) {
	if !k.Paste || m.pastes == nil {
		return m, false
	}
	text := normalizePaste(string(k.Runes))
	if !strings.Contains(text, "\n") && utf8.RuneCountInString(text) <= pasteMaxRunes {
		return m, false
	}
	m.input.InsertString(m.pastes.add(text))
	return m, true
}

// busyLine is the info line shown when a message cannot be sent because a
// turn is still running.
const busyLine = "busy: press Esc to cancel the running turn"

// maxQueueLines is the number of queued messages shown above the input box.
const maxQueueLines = 5

// busy reports whether a turn is running: messages typed now are queued.
func (m Model) busy() bool {
	return m.turnActive || (m.opts.Session != nil && m.opts.Session.Busy())
}

// enqueue appends a message (already trimmed) to the queue sent at the end of
// the running turn. The slice is copied so Model value copies never share it.
func (m *Model) enqueue(text string) {
	m.queue = append(slices.Clone(m.queue), text)
}

// queueLines renders the queue block: one line per queued message, at most
// maxQueueLines of them, then "… +N more"; nil while the queue is empty.
func (m Model) queueLines() []string {
	var lines []string
	for i, q := range m.queue {
		if i == maxQueueLines {
			lines = append(lines, fmt.Sprintf("… +%d more", len(m.queue)-maxQueueLines))
			break
		}
		first, _, multi := strings.Cut(m.redact(q), "\n")
		if multi {
			first += "…"
		}
		lines = append(lines, "> "+first)
	}
	for i, l := range lines {
		lines[i] = lipgloss.NewStyle().MaxWidth(m.width).Render(ghostStyle.Render(l))
	}
	return lines
}

// withQueue puts the queue block below the viewport view vp (and its panel
// and busy line). It takes its lines from the top of the viewport area
// (keeping at least one line), so the total height does not change.
func (m Model) withQueue(vp string) string {
	rows := m.queueLines()
	if len(rows) == 0 {
		return vp
	}
	lines := strings.Split(vp, "\n")
	if room := max(len(lines)-1, 0); len(rows) > room {
		rows = rows[len(rows)-room:]
	}
	lines = lines[min(len(rows), len(lines)):]
	return strings.Join(append(lines, rows...), "\n")
}

// sendQueue submits the queued messages as one new turn (joined by "\n",
// placeholders expanded), adds one user block per message and starts the turn
// clock. On an error the queue is kept for the next turn end.
func (m *Model) sendQueue() tea.Cmd {
	sess := m.opts.Session
	if len(m.queue) == 0 || sess == nil {
		return nil
	}
	if sess.Busy() {
		awaitIdle(sess)
	}
	switch err := sess.Submit(m.pastes.expand(strings.Join(m.queue, "\n"))); {
	case err == nil:
	case errors.Is(err, agent.ErrBusy):
		m.add(blockInfo, busyLine)
		return nil
	default:
		m.add(blockError, "error: "+err.Error())
		return nil
	}
	start := len(m.blocks)
	for _, q := range m.queue {
		m.add(blockPlain, "> "+q)
	}
	m.queue = nil
	m.pushTurnStart(start)
	return m.startTurn()
}

// awaitIdle waits briefly for the session to mark its finished turn as done:
// the turn-done event is emitted just before the session clears its busy
// flag. It gives up after a second (another turn may be running).
func awaitIdle(sess *agent.Session) {
	done := make(chan struct{})
	go func() { sess.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
}
