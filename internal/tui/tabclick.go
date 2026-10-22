package tui

import tea "github.com/charmbracelet/bubbletea"

// tabClick handles a left press at column x of the tab bar: "+" opens a tab,
// "×" closes one, a title switches to it. A press outside every zone does
// nothing.
func (m Model) tabClick(x int) (tea.Model, tea.Cmd) {
	_, zones := m.tabBarLayout()
	for _, z := range zones {
		if x < z.start || x >= z.end {
			continue
		}
		switch z.kind {
		case tabZonePlus:
			return m.openTab()
		case tabZoneSwitch:
			m.switchTab(z.tab)
		case tabZoneClose:
			if z.tab == m.cur {
				return m.closeTab()
			}
			// Close another tab through closeTab, then return to the tab
			// that was current.
			keep := m.sess
			m.switchTab(z.tab)
			m, cmd := m.closeTab()
			m.switchTab(m.tabOf(keep))
			return m, cmd
		}
		return m, nil
	}
	return m, nil
}
