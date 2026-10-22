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
		if m.sess == nil || !m.sess.JevConfigured() {
			m.add(blockInfo, "jev is not configured")
			return m, nil
		}
		st := m.sess.JevStats()
		avg := func(t agent.Tier) string {
			if st.Turns[t] == 0 {
				return "-"
			}
			return fmt.Sprint(st.MainTokens[t] / st.Turns[t])
		}
		m.add(blockInfo, strings.Join([]string{
			fmt.Sprintf("turns routed easy: %d", st.Turns[agent.TierEasy]),
			fmt.Sprintf("turns routed medium: %d", st.Turns[agent.TierMedium]),
			fmt.Sprintf("turns routed hard: %d", st.Turns[agent.TierHard]),
			fmt.Sprintf("turns not routed: %d", st.Turns[agent.TierNone]),
			fmt.Sprintf("jev fallbacks: %d", st.Fallbacks),
			fmt.Sprintf("jev input tokens: %d", st.InputTokens),
			fmt.Sprintf("main tokens easy: %d", st.MainTokens[agent.TierEasy]),
			fmt.Sprintf("main tokens medium: %d", st.MainTokens[agent.TierMedium]),
			fmt.Sprintf("main tokens hard: %d", st.MainTokens[agent.TierHard]),
			fmt.Sprintf("main tokens not routed: %d", st.MainTokens[agent.TierNone]),
			fmt.Sprintf("avg main tokens per turn: easy %s, medium %s, hard %s, not routed %s",
				avg(agent.TierEasy), avg(agent.TierMedium), avg(agent.TierHard), avg(agent.TierNone)),
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
