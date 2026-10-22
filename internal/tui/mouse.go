package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// wheelLines is the number of lines one mouse wheel event scrolls.
const wheelLines = 3

// noticeDuration is how long the "N characters copied" notice stays.
const noticeDuration = 3 * time.Second

var noticeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#B1B9F9"))

// noticeMsg is sent when the copy notice expires, so the program redraws.
type noticeMsg struct{}

// cell is a position in the rendered conversation: a content line and a
// display column.
type cell struct{ line, col int }

// before reports whether c comes before d in reading order.
func (c cell) before(d cell) bool {
	return c.line < d.line || (c.line == d.line && c.col < d.col)
}

// selection is a left-button drag over the conversation.
type selection struct {
	active     bool // the button is down
	moved      bool // the selection was extended after the press
	start, end cell
}

// ordered returns the selection's cells in reading order.
func (s selection) ordered() (cell, cell) {
	if s.end.before(s.start) {
		return s.end, s.start
	}
	return s.start, s.end
}

// mouse handles a mouse event. The wheel scrolls the conversation; a drag
// with the left button selects text and copies it on release.
func (m Model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if msg.Action == tea.MouseActionPress {
			m.viewport.ScrollUp(wheelLines)
		}
		return m, nil
	case tea.MouseButtonWheelDown:
		if msg.Action == tea.MouseActionPress {
			m.viewport.ScrollDown(wheelLines)
		}
		return m, nil
	}

	if msg.Action == tea.MouseActionPress {
		m.inSel = selection{}
		if msg.Button == tea.MouseButtonLeft {
			if row, col, ok := m.inputCellAt(msg.X, msg.Y, false); ok {
				m.sel = selection{}
				m.inputClick(row, col)
				c := cell{line: row, col: col}
				m.inSel = selection{active: true, start: c, end: c}
				return m, nil
			}
		}
	}
	if m.inSel.active {
		return m.inputMouse(msg)
	}

	msg.Y -= tabBarRows // the tab bar is the top screen row
	switch msg.Action {
	case tea.MouseActionPress:
		m.sel = selection{}
		if msg.Button != tea.MouseButtonLeft {
			return m, nil
		}
		if c, ok := m.cellAt(msg.X, msg.Y, false); ok {
			m.sel = selection{active: true, start: c, end: c}
		}
	case tea.MouseActionMotion:
		if !m.sel.active || msg.Button != tea.MouseButtonLeft {
			return m, nil
		}
		if c, ok := m.cellAt(msg.X, msg.Y, true); ok {
			m.sel.end = c
			m.sel.moved = true
		}
	case tea.MouseActionRelease:
		// Terminals in legacy mouse mode report no button on release.
		if !m.sel.active {
			return m, nil
		}
		sel := m.sel
		m.sel = selection{}
		if !sel.moved {
			return m, nil // a click copies nothing
		}
		if c, ok := m.cellAt(msg.X, msg.Y, true); ok {
			sel.end = c
		}
		return m.copyText(m.selectedText(sel))
	}
	return m, nil
}

// copyText redacts text, writes it to the clipboard and shows the copy
// notice. Both selections copy through it, so only redacted text ever
// reaches m.clip. Empty text copies nothing.
func (m Model) copyText(text string) (tea.Model, tea.Cmd) {
	text = m.redact(text)
	if text == "" {
		return m, nil
	}
	_ = m.clip(text)
	m.notice = fmt.Sprintf("%d characters copied", utf8.RuneCountInString(text))
	m.noticeUntil = m.now().Add(noticeDuration)
	return m, tea.Tick(noticeDuration, func(time.Time) tea.Msg { return noticeMsg{} })
}

// viewRows maps each row of the viewport area, as View() shows it after the
// overlays, to its content line; rows covered by an overlay map to -1. It
// runs the same overlays as View() over numbered marker lines, so the mapping
// always matches the screen.
func (m Model) viewRows() []int {
	h := m.viewport.Height
	markers := make([]string, h)
	for i := range markers {
		markers[i] = "\x00" + strconv.Itoa(i)
	}
	shown := strings.Split(m.overlays(strings.Join(markers, "\n")), "\n")
	rows := make([]int, len(shown))
	for y, l := range shown {
		rows[y] = -1
		if rest, ok := strings.CutPrefix(strings.TrimRight(l, " "), "\x00"); ok {
			if i, err := strconv.Atoi(rest); err == nil {
				rows[y] = m.viewport.YOffset + i
			}
		}
	}
	return rows
}

// cellAt maps a screen position to a conversation cell. Positions outside the
// conversation area give false unless clamp is set, in which case the row is
// clamped to the visible conversation rows. Lines and columns are clamped to
// the rendered content.
func (m Model) cellAt(x, y int, clamp bool) (cell, bool) {
	if len(m.lines) == 0 {
		return cell{}, false
	}
	rows := m.viewRows()
	n := 0 // conversation rows are the top rows of the viewport area
	for n < len(rows) && rows[n] >= 0 {
		n++
	}
	if n == 0 {
		return cell{}, false
	}
	if y < 0 || y >= n {
		if !clamp {
			return cell{}, false
		}
		y = min(max(y, 0), n-1)
	}
	line := min(rows[y], len(m.lines)-1)
	w := ansi.StringWidth(m.lines[line])
	col := min(max(x, 0), max(w-1, 0))
	return cell{line: line, col: col}, true
}

// spanOf returns the display columns [a, b) of content line i that the
// selection from s to e covers.
func (m Model) spanOf(i int, s, e cell) (int, int) {
	a, b := 0, ansi.StringWidth(m.lines[i])
	if i == s.line {
		a = s.col
	}
	if i == e.line {
		b = min(b, e.col+1)
	}
	return a, b
}

// selectedText returns the plain text of the selection: the rendered
// (redacted) lines cut by display cells, trailing spaces trimmed per line.
func (m Model) selectedText(sel selection) string {
	s, e := sel.ordered()
	if len(m.lines) == 0 || s.line >= len(m.lines) {
		return ""
	}
	e.line = min(e.line, len(m.lines)-1)
	out := make([]string, 0, e.line-s.line+1)
	for i := s.line; i <= e.line; i++ {
		a, b := m.spanOf(i, s, e)
		out = append(out, strings.TrimRight(ansi.Cut(ansi.Strip(m.lines[i]), a, b), " "))
	}
	return strings.Join(out, "\n")
}

// viewportView renders the viewport, with the selected cells in reverse video
// while a drag is in progress.
func (m Model) viewportView() string {
	if !m.sel.active || !m.sel.moved || len(m.lines) == 0 {
		return m.viewport.View()
	}
	s, e := m.sel.ordered()
	lines := append([]string(nil), m.lines...)
	for i := s.line; i <= min(e.line, len(lines)-1); i++ {
		a, b := m.spanOf(i, s, e)
		if b <= a {
			continue
		}
		l := lines[i]
		lines[i] = ansi.Cut(l, 0, a) + "\x1b[0m\x1b[7m" + ansi.Strip(ansi.Cut(l, a, b)) +
			"\x1b[0m" + ansi.Cut(l, b, ansi.StringWidth(l))
	}
	vp := m.viewport // copy: SetContent replaces the copy's lines only
	vp.SetContent(strings.Join(lines, "\n"))
	return vp.View()
}

// withNotice puts the copy notice below the viewport view vp (and its
// overlays) while it is current. It takes its line from the top of the
// viewport area (keeping at least one line), so the total height does not
// change.
func (m Model) withNotice(vp string) string {
	if m.notice == "" || !m.now().Before(m.noticeUntil) {
		return vp
	}
	lines := strings.Split(vp, "\n")
	if len(lines) > 1 {
		lines = lines[1:]
	}
	line := lipgloss.NewStyle().MaxWidth(m.width).Render(noticeStyle.Render(m.notice))
	return strings.Join(append(lines, line), "\n")
}
