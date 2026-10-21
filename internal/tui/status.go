package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"yipyip/internal/agent"
)

const statusSep = " · "

var dimStyle = lipgloss.NewStyle().Faint(true)

// StatusLine returns the single status line shown under the input:
//
//	<cwd> · <provider/model> · <effort> · <mode> · in N / out N / total N · ctx used[/limit]
//
// It reads the session on every call. When the line is wider than the
// terminal, the cwd is shortened from the left with a leading ellipsis; the
// other fields are never dropped.
func (m Model) StatusLine() string {
	cwd, model, effort, mode := m.opts.StartDir, m.opts.ModelID, m.opts.Effort, m.opts.Mode
	tokens, used, limit := agent.Tokens{}, 0, 0
	dim := false
	if sess := m.opts.Session; sess != nil {
		cwd = sess.StartDir()
		model, effort, mode = sess.ModelID(), sess.Effort(), string(sess.Mode())
		tokens = sess.Tokens()
		used, limit = sess.ContextSize()
		dim = !sess.ReasoningEnabled()
	}
	if model == "" {
		model = "no model"
	}
	if dim {
		effort = dimStyle.Render(effort)
	}
	ctxText := fmt.Sprintf("ctx %d", used)
	if limit > 0 {
		ctxText = fmt.Sprintf("ctx %d/%d", used, limit)
	}
	rest := strings.Join([]string{
		model,
		effort,
		mode,
		fmt.Sprintf("in %d / out %d / total %d", tokens.In, tokens.Out, tokens.Total),
		ctxText,
	}, statusSep)

	avail := m.width - lipgloss.Width(rest) - lipgloss.Width(statusSep)
	return shortenLeft(cwd, avail) + statusSep + rest
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
