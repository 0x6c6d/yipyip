package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// inputRow is one rendered (soft-wrapped) visual row of the input: the text
// line it belongs to, where it starts in that line (in runes) and its runes.
type inputRow struct {
	line, start int
	text        []rune
}

// inputVisualRows returns every visual row of the input text and the index of
// the row holding the cursor. It walks a copy of the textarea with
// CursorUp/CursorDown, so it matches the textarea's own wrapping.
func (m Model) inputVisualRows() ([]inputRow, int) {
	ta := m.input
	lines := strings.Split(ta.Value(), "\n")
	for {
		l, ro := ta.Line(), ta.LineInfo().RowOffset
		ta.CursorUp()
		if ta.Line() == l && ta.LineInfo().RowOffset == ro {
			break
		}
	}
	var rows []inputRow
	for {
		li := ta.LineInfo()
		runes := []rune(lines[ta.Line()])
		end := min(li.StartColumn+li.Width, len(runes))
		rows = append(rows, inputRow{line: ta.Line(), start: li.StartColumn, text: runes[min(li.StartColumn, end):end]})
		ta.CursorDown()
		if ta.Line() == rows[len(rows)-1].line && ta.LineInfo().StartColumn == li.StartColumn {
			break
		}
	}
	cur := 0
	for i, r := range rows {
		if r.line == m.input.Line() && r.start == m.input.LineInfo().StartColumn {
			cur = i
		}
	}
	return rows, cur
}

// inputTop returns the first visual row shown by the input. The textarea
// does not expose its scroll offset, so it is the offset whose rows match the
// rendered view, among those that keep the cursor visible.
func (m Model) inputTop(rows []inputRow, cur int) int {
	if len(rows) <= inputRows {
		return 0
	}
	shown := strings.Split(ansi.Strip(m.input.View()), "\n")
	lo, hi := max(0, cur-inputRows+1), min(cur, len(rows)-inputRows)
	for top := lo; top <= hi; top++ {
		ok := true
		for i := 0; i < inputRows && i < len(shown); i++ {
			if strings.TrimRight(shown[i], " ") != strings.TrimRight(string(rows[top+i].text), " ") {
				ok = false
				break
			}
		}
		if ok {
			return top
		}
	}
	return hi
}

// inputCellAt maps a screen position to an input position: the visual row
// (counted from the first row of the whole input text) and the rune column
// within that row. Positions outside the input text rows give false unless
// clamp is set, in which case they are clamped to the input text rows and
// columns. A position below the last text line, or past the end of a line,
// maps to the end of that row. It is the only place that knows the input
// geometry.
func (m Model) inputCellAt(x, y int, clamp bool) (row, col int, ok bool) {
	top := tabBarRows + m.viewport.Height + 1 // below the viewport and the border
	x--                                       // the left border
	if !clamp && (y < top || y >= top+inputRows || x < 0 || x >= m.input.Width()) {
		return 0, 0, false
	}
	rows, cur := m.inputVisualRows()
	r := m.inputTop(rows, cur) + min(max(y-top, 0), inputRows-1)
	if r >= len(rows) {
		return len(rows) - 1, len(rows[len(rows)-1].text), true
	}
	text := rows[r].text
	col, w := 0, 0
	for col < len(text) && w+runewidth.RuneWidth(text[col]) <= max(x, 0) {
		w += runewidth.RuneWidth(text[col])
		col++
	}
	if r < len(rows)-1 && rows[r+1].line == rows[r].line {
		col = min(col, max(len(text)-1, 0)) // a wrapped row ends before the next one
	}
	return r, col, true
}

// inputClick moves the input cursor to the visual row and column that
// inputCellAt returned.
func (m *Model) inputClick(row, col int) {
	rows, cur := m.inputVisualRows()
	for ; cur < row; cur++ {
		m.input.CursorDown()
	}
	for ; cur > row; cur-- {
		m.input.CursorUp()
	}
	m.input.SetCursor(rows[row].start + col)
}

// inputMouse handles motion and release while a drag over the input is in
// progress: motion extends the selection (clamped to the input rows), the
// release copies the selected text like the conversation copy-on-select.
func (m Model) inputMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Action {
	case tea.MouseActionMotion:
		if msg.Button != tea.MouseButtonLeft {
			return m, nil
		}
		if row, col, ok := m.inputCellAt(msg.X, msg.Y, true); ok {
			m.inSel.end = cell{line: row, col: col}
			m.inSel.moved = true
		}
	case tea.MouseActionRelease:
		sel := m.inSel
		m.inSel = selection{}
		if !sel.moved {
			return m, nil // a click only moves the cursor
		}
		if row, col, ok := m.inputCellAt(msg.X, msg.Y, true); ok {
			sel.end = cell{line: row, col: col}
		}
		return m.copyText(m.inputSelectedText(sel))
	}
	return m, nil
}

// inputSpanOf returns the rune columns [a, b) of visual row i (of rows) that
// the input selection from s to e covers.
func inputSpanOf(rows []inputRow, i int, s, e cell) (int, int) {
	a, b := 0, len(rows[i].text)
	if i == s.line {
		a = min(s.col, b)
	}
	if i == e.line {
		b = min(b, e.col+1)
	}
	return a, max(a, b)
}

// inputSelectedText returns the plain text of an input selection: the
// selected visual rows as the input shows them, trailing spaces trimmed per
// row. It is not redacted yet; copyText does that.
func (m Model) inputSelectedText(sel selection) string {
	rows, _ := m.inputVisualRows()
	s, e := sel.ordered()
	if s.line >= len(rows) {
		return ""
	}
	e.line = min(e.line, len(rows)-1)
	out := make([]string, 0, e.line-s.line+1)
	for i := s.line; i <= e.line; i++ {
		a, b := inputSpanOf(rows, i, s, e)
		out = append(out, strings.TrimRight(ansi.Strip(string(rows[i].text[a:b])), " "))
	}
	return strings.Join(out, "\n")
}

// inputView renders the input, with the selected cells in reverse video
// while a drag over it is in progress. The input text is not changed.
func (m Model) inputView() string {
	view := m.input.View()
	if !m.inSel.active || !m.inSel.moved {
		return view
	}
	rows, cur := m.inputVisualRows()
	top := m.inputTop(rows, cur)
	s, e := m.inSel.ordered()
	lines := strings.Split(view, "\n")
	for y := range lines {
		i := top + y
		if i < s.line || i > e.line || i >= len(rows) {
			continue
		}
		a, b := inputSpanOf(rows, i, s, e)
		if b <= a {
			continue
		}
		text := rows[i].text
		wa, wb := runewidth.StringWidth(string(text[:a])), runewidth.StringWidth(string(text[:b]))
		l := lines[y]
		lines[y] = ansi.Cut(l, 0, wa) + "\x1b[0m\x1b[7m" + ansi.Strip(ansi.Cut(l, wa, wb)) +
			"\x1b[0m" + ansi.Cut(l, wb, ansi.StringWidth(l))
	}
	return strings.Join(lines, "\n")
}
