package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
)

// compactDoneMsg reports the end of a /compact run started by cmdCompact.
// sess is the compacted session: the outcome goes to its tab (nil = the
// current tab).
type compactDoneMsg struct {
	err  error
	sess *agent.Session
}

// cmdCompact implements /compact: it shows a dim "compacting..." line and
// returns a command that runs Session.Compact off the update loop. On success
// the session's own "compacted: N messages -> M" info event is the only
// confirmation, so nothing contradicting it is printed.
func cmdCompact(m Model, _ string) (Model, tea.Cmd) {
	sess := m.sess
	if sess == nil {
		m.add(blockInfo, "nothing to compact")
		return m, nil
	}
	m.add(blockInfo, "compacting...")
	return m, func() tea.Msg {
		return compactDoneMsg{err: sess.Compact(context.Background()), sess: sess}
	}
}

// compactDone renders the outcome of a /compact run.
func (m *Model) compactDone(err error) {
	switch {
	case err == nil:
		m.refresh() // the info event arrives through the event pump
	case errors.Is(err, agent.ErrNothingToCompact):
		m.add(blockInfo, "nothing to compact")
	case errors.Is(err, agent.ErrBusy):
		m.add(blockInfo, "busy: press Esc to cancel the running turn")
	default:
		m.add(blockError, "error: "+err.Error())
	}
}
