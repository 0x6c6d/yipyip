package tui

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
)

// cmdInit implements /init: it asks the model to inspect the repository and
// write or update AGENTS.md.
func cmdInit(m Model, _ string) (Model, tea.Cmd) {
	sess := m.sess
	if sess == nil {
		m.add(blockInfo, "no model configured")
		return m, nil
	}
	start := len(m.blocks)
	switch err := sess.Init(); {
	case err == nil:
		m.add(blockInfo, "initializing AGENTS.md ...")
		m.pushTurnStart(start)
		return m, m.startTurn()
	case errors.Is(err, agent.ErrNoModel):
		m.add(blockInfo, "no model configured")
	case errors.Is(err, agent.ErrBusy):
		m.add(blockInfo, "busy: press Esc to cancel the running turn")
	default:
		m.add(blockError, m.redact("error: "+err.Error()))
	}
	return m, nil
}
