package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yipyip/internal/agent"
)

// taskWidth is the width a task is shortened to in the panel and the table.
const taskWidth = 40

// shortTask collapses whitespace and shortens s to at most n runes.
func shortTask(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// subagents returns the session's subagents (nil without a session).
func (m Model) subagents() []agent.SubInfo {
	if m.subInfo != nil {
		return m.subInfo()
	}
	if sess := m.sess; sess != nil {
		return sess.Subagents()
	}
	return nil
}

var (
	panelDotStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#D77757"))
	panelTextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// panelLines renders one line per running subagent; nil when none runs.
func (m Model) panelLines() []string {
	var lines []string
	for _, s := range m.subagents() {
		if s.Status != agent.SubRunning {
			continue
		}
		start := m.now()
		if st, ok := m.subs[s.ID]; ok {
			start = st.start
		}
		rest := m.redact(fmt.Sprintf(" %s %s (%s · %s tokens)", s.ID, shortTask(s.Task, taskWidth),
			formatElapsed(m.now().Sub(start)), formatTokens(s.Tokens)))
		line := panelDotStyle.Render("●") + panelTextStyle.Render(rest)
		lines = append(lines, lipgloss.NewStyle().MaxWidth(m.width).Render(line))
	}
	return lines
}

// withPanel puts the panel lines below the viewport view vp. The panel takes
// its lines from the top of the viewport area (keeping at least one line), so
// the total height does not change.
func (m Model) withPanel(vp string) string {
	rows := m.panelLines()
	if len(rows) == 0 {
		return vp
	}
	lines := strings.Split(vp, "\n")
	if max := len(lines) - 1; len(rows) > max {
		if max < 0 {
			max = 0
		}
		rows = rows[len(rows)-max:]
	}
	lines = lines[min(len(rows), len(lines)):]
	return strings.Join(append(lines, rows...), "\n")
}

// cmdAgents implements /agents, /agents <id> and /agents kill <id>.
func cmdAgents(m Model, args string) (Model, tea.Cmd) {
	sess := m.sess
	fields := strings.Fields(args)
	switch {
	case len(fields) == 0:
		m.add(blockInfo, m.redact(m.agentsTable()))
	case fields[0] == "kill" && len(fields) == 2:
		id := fields[1]
		if sess == nil || !m.knownSub(id) {
			m.add(blockError, m.redact("error: unknown subagent: "+id))
			return m, nil
		}
		if err := sess.CancelSubagent(id); err != nil {
			m.add(blockError, m.redact("error: "+err.Error()))
			return m, nil
		}
		m.add(blockInfo, "cancelled "+id)
	case len(fields) == 1:
		id := fields[0]
		var lines []string
		ok := false
		if sess != nil {
			lines, ok = sess.SubagentTranscript(id)
		}
		if !ok {
			m.add(blockError, m.redact("error: unknown subagent: "+id))
			return m, nil
		}
		if len(lines) == 0 {
			lines = []string{id + ": empty transcript"}
		}
		m.add(blockInfo, m.redact(strings.Join(lines, "\n")))
	default:
		m.add(blockError, "error: usage: /agents [<id> | kill <id>]")
	}
	return m, nil
}

func (m Model) knownSub(id string) bool {
	for _, s := range m.subagents() {
		if s.ID == id {
			return true
		}
	}
	return false
}

// agentsTable renders all subagents as aligned columns.
func (m Model) agentsTable() string {
	subs := m.subagents()
	if len(subs) == 0 {
		return "no subagents"
	}
	rows := [][]string{{"ID", "STATUS", "TOKENS", "TASK"}}
	for _, s := range subs {
		rows = append(rows, []string{s.ID, string(s.Status), fmt.Sprint(s.Tokens), shortTask(s.Task, taskWidth)})
	}
	widths := make([]int, 3)
	for _, r := range rows {
		for i := range widths {
			widths[i] = max(widths[i], len([]rune(r[i])))
		}
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = fmt.Sprintf("%-*s  %-*s  %*s  %s", widths[0], r[0], widths[1], r[1], widths[2], r[2], r[3])
	}
	return strings.Join(out, "\n")
}
