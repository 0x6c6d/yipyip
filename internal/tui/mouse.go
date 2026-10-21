package tui

import tea "github.com/charmbracelet/bubbletea"

// wheelLines is the number of lines one mouse wheel event scrolls.
const wheelLines = 3

// mouse handles a mouse event. The wheel scrolls the conversation; every
// other button and action is a no-op here (the left button is reserved for
// selection).
func (m Model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.viewport.ScrollUp(wheelLines)
	case tea.MouseButtonWheelDown:
		m.viewport.ScrollDown(wheelLines)
	}
	return m, nil
}
