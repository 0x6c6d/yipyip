package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

func subEv(kind agent.EventKind, id, tool, args string) EventMsg {
	return EventMsg{Event: agent.Event{Kind: kind, AgentID: id, Tool: tool, Args: args}}
}

// subModel is newClockModel with a tall window and some earlier conversation.
func subModel(t *testing.T) (Model, *fakeClock) {
	t.Helper()
	m, _, clock := newClockModel(t, nil)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 60})
	m = mm.(Model)
	// The panel and the Thinking line overwrite the top viewport lines, so
	// keep some older conversation above the blocks under test.
	m.add(blockInfo, "earlier\n\n\n")
	return m, clock
}

func subLines(m Model) []string { return strings.Split(ansi.Strip(m.View()), "\n") }

func subHasLine(lines []string, want string) bool {
	for _, l := range lines {
		if strings.TrimRight(l, " ") == want {
			return true
		}
	}
	return false
}

func TestSubagentBlockSingle(t *testing.T) {
	m, _ := subModel(t)
	info := []agent.SubInfo{{ID: "a1", Task: "do it", Status: agent.SubRunning}}
	m.subInfo = func() []agent.SubInfo { return info }
	for _, e := range []EventMsg{
		subEv(agent.EvSubagent, "a1", "", ""),
		subEv(agent.EvToolCall, "a1", "bash", `{"command":"ls"}`),
		subEv(agent.EvToolResult, "a1", "bash", ""),
		subEv(agent.EvToolCall, "a1", "read_file", `{"path":"x"}`),
		subEv(agent.EvToolResult, "a1", "read_file", ""),
	} {
		mm, _ := m.Update(e)
		m = mm.(Model)
	}
	if n := strings.Count(ansi.Strip(m.View()), "● Subagent a1 ("); n != 1 {
		t.Errorf("want one block, got %d:\n%s", n, m.View())
	}
}

func TestSubagentBlockRunning(t *testing.T) {
	m, clock := subModel(t)
	info := []agent.SubInfo{{ID: "a1", Task: "list files", Status: agent.SubRunning, Tokens: 1200}}
	m.subInfo = func() []agent.SubInfo { return info }
	for _, e := range []EventMsg{
		subEv(agent.EvSubagent, "a1", "", ""),
		subEv(agent.EvToolCall, "a1", "read_file", `{"path":"x"}`),
		subEv(agent.EvToolCall, "a1", "bash", `{"command":"ls"}`),
	} {
		mm, _ := m.Update(e)
		m = mm.(Model)
	}
	clock.Advance(14 * time.Second)
	mm, _ := m.Update(tickMsg(clock.Now()))
	m = mm.(Model)
	lines := subLines(m)
	for _, want := range []string{
		"● Subagent a1 (list files)",
		"  ⎿  bash(ls)",
		"  ⎿  Running… (2 tool uses · 1.2k tokens · 14s)",
	} {
		if !subHasLine(lines, want) {
			t.Errorf("missing line %q:\n%s", want, m.View())
		}
	}
}

func TestSubagentBlockEnded(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	for _, tc := range []struct {
		status  agent.SubStatus
		word    string
		colour  string
		errLine bool
	}{
		{agent.SubDone, "Done (", "32m", false},
		{agent.SubFailed, "Failed (", "31m", true},
		{agent.SubCancelled, "Cancelled (", "38;5;244", false},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			m, clock := subModel(t)
			info := []agent.SubInfo{{ID: "a1", Task: "t", Status: agent.SubRunning, Tokens: 50}}
			m.subInfo = func() []agent.SubInfo { return info }
			for _, e := range []EventMsg{
				subEv(agent.EvSubagent, "a1", "", ""),
				subEv(agent.EvToolCall, "a1", "bash", `{"command":"ls"}`),
			} {
				mm, _ := m.Update(e)
				m = mm.(Model)
			}
			clock.Advance(5 * time.Second)
			info[0].Status, info[0].Err = tc.status, "boom SECRET123\nsecond line"
			mm, _ := m.Update(subEv(agent.EvSubagent, "a1", "", ""))
			m = mm.(Model)
			clock.Advance(30 * time.Second) // frozen after the end
			mm, _ = m.Update(tickMsg(clock.Now()))
			m = mm.(Model)

			plain := ansi.Strip(m.View())
			if !strings.Contains(plain, tc.word+"1 tool use · 50 tokens · 5s)") {
				t.Errorf("missing %q ending:\n%s", tc.word, plain)
			}
			if got := strings.Contains(plain, "     boom"); got != tc.errLine {
				t.Errorf("error line present = %v, want %v:\n%s", got, tc.errLine, plain)
			}
			if strings.Contains(plain, "second line") {
				t.Errorf("more than the first error line:\n%s", plain)
			}
			if !strings.Contains(m.View(), tc.colour) {
				t.Errorf("colour %s missing in %q", tc.colour, m.View())
			}
		})
	}
}

func TestSubagentNoSeparateBlocks(t *testing.T) {
	m, _ := subModel(t)
	m.subInfo = func() []agent.SubInfo {
		return []agent.SubInfo{{ID: "a1", Task: "t", Status: agent.SubRunning}}
	}
	for _, e := range []EventMsg{
		subEv(agent.EvToolCall, agent.MainAgentID, "spawn_agent", `{"task":"t"}`),
		subEv(agent.EvSubagent, "a1", "", ""),
		subEv(agent.EvToolCall, "a1", "bash", `{"command":"ls"}`),
		subEv(agent.EvToolResult, "a1", "bash", "out"),
		subEv(agent.EvToolResult, agent.MainAgentID, "spawn_agent", "a1"),
	} {
		mm, _ := m.Update(e)
		m = mm.(Model)
	}
	v := ansi.Strip(m.View())
	for _, bad := range []string{"spawn_agent(", "[a1]"} {
		if strings.Contains(v, bad) {
			t.Errorf("view contains %q:\n%s", bad, v)
		}
	}
	if !strings.Contains(v, "● Subagent a1 (") {
		t.Errorf("no subagent block:\n%s", v)
	}
}

func TestSubagentRealSpawn(t *testing.T) {
	m, sess := agentsModel(t, providertest.Response{Text: "sub finished"})
	m, _ = m.Command(agentsMainPrompt)
	waitSession(t, sess)
	m = m.Drain()
	v := ansi.Strip(m.View())
	if n := strings.Count(v, "● Subagent a1 (TASK-SUB"); n != 1 {
		t.Fatalf("want one block, got %d:\n%s", n, v)
	}
	if !strings.Contains(v, "Done (") {
		t.Errorf("block not ended:\n%s", v)
	}
	if strings.Contains(v, "spawn_agent(") || strings.Contains(v, "[a1]") {
		t.Errorf("separate blocks shown:\n%s", v)
	}
}
