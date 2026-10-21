package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/session"
)

// tabBarRows is the height of the tab bar: the top line of the view.
const tabBarRows = 1

// newSessionTitle is the title of a tab whose conversation has no user
// message yet.
const newSessionTitle = "new session"

var (
	tabCurrentStyle = lipgloss.NewStyle().Bold(true)
	tabOtherStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// tabTitle returns the title shown for tab i: the session title of its first
// user message (redacted), or newSessionTitle without one.
func (m *Model) tabTitle(i int) string {
	sess := m.tabAt(i).sess
	if sess == nil {
		return newSessionTitle
	}
	for _, h := range sess.History() {
		if h.Role != "user" {
			continue
		}
		if title := strings.TrimSpace(session.Title(sess.Redact(h.Content))); title != "" {
			return title
		}
		break
	}
	return newSessionTitle
}

// tabBar renders the tab bar line: one "<N>:<title>" segment per tab (the
// current one in bold, the others dim), separated by one space and cut to
// the window width. A single fresh tab still shows its one segment, so the
// bar never appears or disappears and the layout height stays fixed.
func (m Model) tabBar() string {
	segs := make([]string, len(m.tabs))
	for i := range m.tabs {
		s := strconv.Itoa(i+1) + ":" + m.tabTitle(i)
		if i == m.cur {
			segs[i] = tabCurrentStyle.Render(s)
		} else {
			segs[i] = tabOtherStyle.Render(s)
		}
	}
	line := strings.Join(segs, " ")
	if m.width > 0 {
		line = ansi.Truncate(line, m.width, "")
	}
	return line
}
