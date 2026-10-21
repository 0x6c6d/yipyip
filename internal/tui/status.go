package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"yipyip/internal/agent"
)

const statusSep = " · "

var dimStyle = lipgloss.NewStyle().Faint(true)

// modeHint follows the mode word on row 1; it never mentions the slash
// command, only the key.
const modeHint = "(shift+tab to toggle)"

// autoModeStyle colours the word "auto" on row 1; "manual" keeps the
// terminal's default colour.
var autoModeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E8A33D"))

// modeHintStyle colours the key hint after the mode word in both modes.
var modeHintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))

// modeLabel renders "<mode> (shift+tab to toggle)" with the mode colours.
func modeLabel(mode string) string {
	if mode == string(agent.ModeAuto) {
		mode = autoModeStyle.Render(mode)
	}
	return mode + " " + modeHintStyle.Render(modeHint)
}

var tokenRowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

// StatusLine returns both status rows joined by a newline.
func (m Model) StatusLine() string {
	row1, row2 := m.StatusRows()
	return row1 + "\n" + row2
}

// StatusRows returns the two status rows shown under the input:
//
//	<cwd> · <provider/model> · <effort> · <mode> (shift+tab to toggle)
//	in N · out N · total N · ctx used[/limit]
//
// It reads the session on every call. When row 1 is wider than the terminal,
// the cwd is shortened from the left with a leading ellipsis; the other
// fields are never dropped. The mode word "auto" is coloured amber, "manual"
// keeps the default colour, and the key hint is light grey. Row 2 is rendered
// in a grey colour.
func (m Model) StatusRows() (row1, row2 string) {
	cwd, model, effort, mode := m.opts.StartDir, m.opts.ModelID, m.opts.Effort, m.opts.Mode
	tokens, used, limit := agent.Tokens{}, 0, 0
	dim := false
	if sess := m.sess; sess != nil {
		cwd = sess.StartDir()
		model, effort, mode = sess.ModelID(), sess.Effort(), string(sess.Mode())
		tokens = sess.Tokens()
		used, limit = sess.ContextSize()
		dim = !sess.ReasoningEnabled()
	}
	if model == "" {
		model = "no model"
	}
	cwd, model = m.redact(cwd), m.redact(model)
	if dim {
		effort = dimStyle.Render(effort)
	}
	fields := []string{model, effort, modeLabel(mode)}
	if m.retryStatus != "" {
		fields = append(fields, dimStyle.Render(m.retryStatus))
	}
	rest := strings.Join(fields, statusSep)
	avail := m.width - lipgloss.Width(rest) - lipgloss.Width(statusSep)
	row1 = shortenLeft(cwd, avail) + statusSep + rest

	ctxText := fmt.Sprintf("ctx %d", used)
	if limit > 0 {
		ctxText = fmt.Sprintf("ctx %d/%d", used, limit)
	}
	row2 = tokenRowStyle.Render(strings.Join([]string{
		fmt.Sprintf("in %d", tokens.In),
		fmt.Sprintf("out %d", tokens.Out),
		fmt.Sprintf("total %d", tokens.Total),
		ctxText,
	}, statusSep))
	return row1, row2
}

// shortenLeft returns s unchanged when it fits in max cells, otherwise "…"
// followed by the longest tail of s that fits (just "…" when nothing does).
func shortenLeft(s string, max int) string {
	if lipgloss.Width(s) <= max {
		return s
	}
	runes := []rune(s)
	for i := 1; i <= len(runes); i++ {
		tail := string(runes[i:])
		if lipgloss.Width("…"+tail) <= max {
			return "…" + tail
		}
	}
	return "…"
}
