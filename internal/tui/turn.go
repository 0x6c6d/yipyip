package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yipyip/internal/agent"
)

// tickMsg is the one-second turn clock tick.
type tickMsg time.Time

// tickCmd schedules the next turn clock tick.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

var (
	thinkingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#D77757"))
	thinkingDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// isMainAgent reports whether an event comes from the main agent.
func isMainAgent(id string) bool { return id == "" || id == agent.MainAgentID }

// startTurn marks a turn as started now and returns the first tick command.
func (m *Model) startTurn() tea.Cmd {
	m.turnActive = true
	m.interrupted = false
	m.turnStart = m.now()
	m.openCalls = 0
	m.turnOut0 = 0
	if sess := m.sess; sess != nil {
		m.turnOut0 = sess.Tokens().Out
	}
	return tickCmd()
}

// thinkingLine renders the busy line; "" while no turn runs.
func (m Model) thinkingLine() string {
	if !m.turnActive {
		return ""
	}
	verb := "Thinking…"
	if m.openCalls > 0 {
		verb = "Working…"
	}
	n := 0
	if sess := m.sess; sess != nil {
		n = sess.Tokens().Out - m.turnOut0
	}
	parts := formatElapsed(m.now().Sub(m.turnStart))
	if n > 0 {
		parts += " · ↓ " + formatTokens(n) + " tokens"
	}
	parts += " · esc to interrupt"
	line := thinkingStyle.Render("✻ "+verb) + thinkingDim.Render(" ("+parts+")")
	return lipgloss.NewStyle().MaxWidth(m.width).Render(line)
}

// withThinking puts the busy line below the viewport view vp (and its panel).
// It takes its line from the top of the viewport area (keeping at least one
// line), so the total height does not change.
func (m Model) withThinking(vp string) string {
	line := m.thinkingLine()
	if line == "" {
		return vp
	}
	lines := strings.Split(vp, "\n")
	if len(lines) > 1 {
		lines = lines[1:]
	}
	return strings.Join(append(lines, line), "\n")
}

// formatElapsed formats d as whole seconds: "12s", "1m 15s".
func formatElapsed(d time.Duration) string {
	s := int(d / time.Second)
	if s < 0 {
		s = 0
	}
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm %02ds", s/60, s%60)
}

// formatTokens formats a token count: "999", "1.2k", "1.5M".
func formatTokens(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
}
