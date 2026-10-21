package tui

import (
	"errors"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
)

// pushTurnStart records that the view position start begins a submitted turn.
// The slice is copied so Model value copies never share it.
func (m *Model) pushTurnStart(start int) {
	m.turnStarts = append(slices.Clone(m.turnStarts), start)
}

// popTurnStart removes and returns the start position of the last turn.
func (m *Model) popTurnStart() (int, bool) {
	n := len(m.turnStarts)
	if n == 0 {
		return 0, false
	}
	start := m.turnStarts[n-1]
	m.turnStarts = m.turnStarts[: n-1 : n-1]
	return start, true
}

// cmdUndo implements /undo: it reverts the last turn in the session and
// removes it from the conversation view.
func cmdUndo(m Model, _ string) (Model, tea.Cmd) {
	sess := m.sess
	if sess == nil {
		m.add(blockInfo, "nothing to undo")
		return m, nil
	}
	res, err := sess.Undo()
	switch {
	case errors.Is(err, agent.ErrNothingToUndo):
		m.add(blockInfo, "nothing to undo")
		return m, nil
	case errors.Is(err, agent.ErrBusy):
		m.add(blockInfo, "busy: press Esc to cancel the running turn")
		return m, nil
	case err != nil:
		m.add(blockError, m.redact("error: "+err.Error()))
		return m, nil
	}
	if start, ok := m.popTurnStart(); ok && start <= len(m.blocks) {
		// Blocks carry the tool call state, so it goes with them.
		m.blocks = slices.Clone(m.blocks[:start])
	}
	m.streaming = false
	lines := []string{"undone:"}
	for _, p := range res.Restored {
		lines = append(lines, "restored "+p)
	}
	for _, p := range res.Deleted {
		lines = append(lines, "deleted "+p)
	}
	for _, c := range res.BashNotReverted {
		lines = append(lines, "not reverted (bash): "+c)
	}
	m.add(blockInfo, m.redact(strings.Join(lines, "\n")))
	return m, nil
}
