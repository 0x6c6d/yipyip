package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"yipyip/internal/agent"
)

// subState is the TUI-side state of one subagent's live block. Times and
// counts are tracked from events and the model clock.
type subState struct {
	id         string
	start, end time.Time
	toolUses   int
	lastCall   string // Name(summary) of the latest call, redacted
	ended      bool
}

var (
	subRunningStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#D77757"))
	subDoneStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	subFailedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	subCancelledStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// ensureSub returns the state of subagent id, creating it and appending its
// live block on first sight.
func (m *Model) ensureSub(id string) *subState {
	if s, ok := m.subs[id]; ok {
		return s
	}
	if m.subs == nil {
		m.subs = map[string]*subState{}
	}
	s := &subState{id: id, start: m.now()}
	m.subs[id] = s
	m.add(blockSubagent, id)
	return s
}

// subToolCall counts a subagent tool call and remembers it as the latest.
func (m *Model) subToolCall(ev agent.Event) {
	s := m.ensureSub(ev.AgentID)
	s.toolUses++
	s.lastCall = ev.Tool + "(" + toolSummary(ev.Tool, m.redact(ev.Args)) + ")"
	m.refresh()
}

// subStatusEvent handles EvSubagent: it creates the block and marks the
// subagent ended once its status is no longer running.
func (m *Model) subStatusEvent(id string) {
	s := m.ensureSub(id)
	if !s.ended {
		if info, ok := m.subInfoOf(id); ok && info.Status != agent.SubRunning {
			s.ended, s.end = true, m.now()
		}
	}
	m.refresh()
}

// subInfoOf looks up the SubInfo of subagent id.
func (m *Model) subInfoOf(id string) (agent.SubInfo, bool) {
	for _, i := range m.subagents() {
		if i.ID == id {
			return i, true
		}
	}
	return agent.SubInfo{}, false
}

// anySubRunning reports whether a live subagent block is still running.
func (m *Model) anySubRunning() bool {
	for _, s := range m.subs {
		if !s.ended {
			return true
		}
	}
	return false
}

// renderSubagent renders the live block of subagent id.
func (m *Model) renderSubagent(id string) string {
	s, ok := m.subs[id]
	if !ok {
		return ""
	}
	info, _ := m.subInfoOf(id)
	status, style := "Running…", subRunningStyle
	elapsed := m.now().Sub(s.start)
	if s.ended {
		elapsed = s.end.Sub(s.start)
		switch info.Status {
		case agent.SubFailed:
			status, style = "Failed", subFailedStyle
		case agent.SubCancelled:
			status, style = "Cancelled", subCancelledStyle
		default:
			status, style = "Done", subDoneStyle
		}
	}
	uses := fmt.Sprintf("%d tool uses", s.toolUses)
	if s.toolUses == 1 {
		uses = "1 tool use"
	}
	stats := fmt.Sprintf(" (%s · %s tokens · %s)", uses, formatTokens(info.Tokens), formatElapsed(elapsed))

	out := style.Render("●") + " " + m.redact("Subagent "+id+" ("+shortTask(info.Task, taskWidth)+")")
	if s.lastCall != "" {
		out += "\n  ⎿  " + toolBulletDim.Render(m.redact(s.lastCall))
	}
	out += "\n  ⎿  " + style.Render(status) + thinkingDim.Render(m.redact(stats))
	if s.ended && info.Status == agent.SubFailed {
		first, _, _ := strings.Cut(strings.TrimSpace(m.redact(info.Err)), "\n")
		if first = strings.TrimSpace(first); first != "" {
			out += "\n     " + subFailedStyle.Render(first)
		}
	}
	return out
}
