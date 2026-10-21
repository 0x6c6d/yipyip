package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yipyip/internal/agent"
)

const (
	questionTitle   = "Question"
	questionFreeRow = "type your own answer"
	questionHint    = "↑/↓ move · Space select · Enter confirm · Esc cancel"
	questionMaxW    = 64 // widest inner box width
)

var (
	questionBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	questionBold   = lipgloss.NewStyle().Bold(true)
	questionCursor = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
)

// questionState is the UI state of the first pending question: the row
// cursor (0..len(options); len(options) is the free-text row), the marked
// options and the free-text buffer. The marks slice is copy-on-write.
type questionState struct {
	row   int
	marks []bool
	text  []rune
}

// enqueueQuestion appends req to the pending queue (the slice is copied so
// earlier copies of the Model do not see the new entry).
func (m *Model) enqueueQuestion(req *agent.QuestionRequest) {
	next := make([]*agent.QuestionRequest, 0, len(m.questions)+1)
	next = append(next, m.questions...)
	if len(next) == 0 {
		m.qs = newQuestionState(req)
	}
	m.questions = append(next, req)
}

func newQuestionState(req *agent.QuestionRequest) questionState {
	return questionState{marks: make([]bool, len(req.Options))}
}

// cancelQuestions answers Cancelled to every pending question and empties the
// queue.
func (m *Model) cancelQuestions() {
	for _, r := range m.questions {
		r.Cancel()
	}
	m.questions = nil
	m.qs = questionState{}
}

// closeQuestion answers the first pending question and shows the next one.
func (m *Model) closeQuestion(answer string) {
	first := m.questions[0]
	m.questions = append([]*agent.QuestionRequest(nil), m.questions[1:]...)
	if len(m.questions) == 0 {
		m.questions = nil
		m.qs = questionState{}
	} else {
		m.qs = newQuestionState(m.questions[0])
	}
	first.Respond(answer)
}

// questionResult returns the answer of the current state and whether there
// is one (nothing selected and no free text means none).
func (m Model) questionResult() (string, bool) {
	req := m.questions[0]
	var labels []string
	for i, on := range m.qs.marks {
		if on && i < len(req.Options) {
			labels = append(labels, req.Options[i])
		}
	}
	if len(labels) > 0 {
		return agent.JoinAnswers(labels), true
	}
	if t := string(m.qs.text); strings.TrimSpace(t) != "" {
		return t, true
	}
	return "", false
}

// questionKey handles one key press while a question is pending. Ctrl+C is
// not handled here (the caller keeps the normal rule); every other key is
// swallowed, so nothing reaches the input.
func (m Model) questionKey(k tea.KeyMsg) (Model, tea.Cmd) {
	req := m.questions[0]
	n := len(req.Options)
	onFree := m.qs.row >= n
	switch k.Type {
	case tea.KeyEsc:
		m.closeQuestion(agent.Cancelled)
	case tea.KeyUp:
		m.qs.row = max(0, m.qs.row-1)
	case tea.KeyDown:
		m.qs.row = min(n, m.qs.row+1)
	case tea.KeyEnter:
		if ans, ok := m.questionResult(); ok {
			m.closeQuestion(ans)
		}
	case tea.KeyBackspace:
		if onFree && len(m.qs.text) > 0 {
			m.qs.text = append([]rune(nil), m.qs.text[:len(m.qs.text)-1]...)
		}
	case tea.KeySpace:
		if onFree {
			m.typeFree(" ")
		} else {
			m.toggleOption(req)
		}
	case tea.KeyRunes:
		s := string(k.Runes)
		switch {
		case onFree:
			m.typeFree(s)
		case s == "k":
			m.qs.row = max(0, m.qs.row-1)
		case s == "j":
			m.qs.row = min(n, m.qs.row+1)
		}
	}
	return m, nil
}

// typeFree appends the printable runes of s to the free-text buffer.
func (m *Model) typeFree(s string) {
	buf := append([]rune(nil), m.qs.text...)
	for _, r := range s {
		if r >= ' ' && r != 0x7f {
			buf = append(buf, r)
		}
	}
	m.qs.text = buf
}

// toggleOption marks the option under the cursor: single choice moves the
// mark to it, multiple choice toggles it.
func (m *Model) toggleOption(req *agent.QuestionRequest) {
	marks := append([]bool(nil), m.qs.marks...)
	if req.Multiple {
		marks[m.qs.row] = !marks[m.qs.row]
	} else {
		for i := range marks {
			marks[i] = i == m.qs.row
		}
	}
	m.qs.marks = marks
}

// questionBox renders the overlay box for the first pending question. All
// model-authored text is redacted; the typed free text is the user's own.
func (q Model) questionBox() string {
	req := q.questions[0]
	inner := max(20, min(questionMaxW, q.width-4))
	wrap := lipgloss.NewStyle().Width(inner)
	lines := []string{
		wrap.Render(questionBold.Render(questionTitle)),
		wrap.Render(q.redact(req.Question)),
	}
	for i, o := range req.Options {
		glyph := "○"
		if req.Multiple {
			glyph = "[ ]"
		}
		if i < len(q.qs.marks) && q.qs.marks[i] {
			glyph = "●"
			if req.Multiple {
				glyph = "[x]"
			}
		}
		lines = append(lines, q.questionRow(i == q.qs.row, glyph+" "+q.redact(o), inner))
	}
	free := questionFreeRow
	onFree := q.qs.row >= len(req.Options)
	if len(q.qs.text) > 0 {
		free += ": " + string(q.qs.text)
	}
	if onFree {
		free += "█"
	}
	lines = append(lines, q.questionRow(onFree, free, inner))
	lines = append(lines, wrap.Render(dimStyle.Render(questionHint)))
	return questionBorder.Render(strings.Join(lines, "\n"))
}

func (m Model) questionRow(cur bool, text string, inner int) string {
	prefix := "  "
	if cur {
		prefix = questionCursor.Render("› ")
	}
	return lipgloss.NewStyle().Width(inner).Render(prefix + text)
}

// overlayQuestion draws the question box centered over the dimmed viewport
// view vp.
func (m Model) overlayQuestion(vp string) string {
	return m.overlayCentered(vp, m.questionBox())
}
