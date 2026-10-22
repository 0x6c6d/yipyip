package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
)

// init registers /jev; like /skills, the table entry would otherwise form an
// initialization cycle.
func init() { slashCommands["/jev"] = cmdJev }

// cmdJev is /jev: no args toggles jev for all tabs (global like the mode, not
// saved), "stats" shows the current tab's counters.
func cmdJev(m Model, args string) (Model, tea.Cmd) {
	switch args {
	case "":
		sess := m.sess
		if sess == nil || !sess.JevConfigured() {
			m.add(blockInfo, "jev is not configured")
			return m, nil
		}
		m.setJev(!sess.JevOn())
		if m.jevOn {
			m.add(blockInfo, "jev on")
		} else {
			m.add(blockInfo, "jev off")
		}
	case "stats":
		var st agent.JevStats
		if m.sess != nil {
			st = m.sess.JevStats()
		}
		m.add(blockInfo, strings.Join([]string{
			fmt.Sprintf("jev answered: %d", st.Answered),
			fmt.Sprintf("jev fallbacks: %d", st.Fallbacks),
			fmt.Sprintf("jev input tokens: %d", st.InputTokens),
			fmt.Sprintf("main tokens question: %d", st.QuestionTokens),
			fmt.Sprintf("main tokens routing: %d", st.RoutingTokens),
			fmt.Sprintf("main tokens other: %d", st.OtherTokens),
		}, "\n"))
	default:
		m.add(blockError, "error: usage: /jev [stats]")
	}
	return m, nil
}

// setJev applies the jev switch to the sessions of all tabs; new tabs inherit
// it (see addTab). Sessions without a configured jev stay off.
func (m *Model) setJev(on bool) {
	m.jevOn = on
	for _, s := range m.sessions() {
		_ = s.SetJev(on)
	}
}
