package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"yipyip/internal/agent"
)

const (
	maxArgsLen      = 200
	collapsedLines  = 3
	mainAgentID     = "main"
	toolBlockMargin = 2 // border columns of a tool block
)

var toolBorderStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("8"))

// toolCall is the data of a tool call block. Raw strings are redacted at
// render time.
type toolCall struct {
	agentID string
	tool    string
	args    string
	result  string
	hasRes  bool
	isError bool
	closed  bool // no result will arrive any more
}

// addToolCall appends a tool call block.
func (m *Model) addToolCall(ev agent.Event) {
	m.blocks = append(m.blocks, block{kind: blockTool, call: &toolCall{agentID: ev.AgentID, tool: ev.Tool, args: ev.Args}})
	m.streaming = false
	m.refresh()
}

// addToolResult attaches a result to the oldest open call of the same tool
// and agent; without such a call it becomes a plain block.
func (m *Model) addToolResult(ev agent.Event) {
	for _, b := range m.blocks {
		c := b.call
		if b.kind == blockTool && !c.hasRes && !c.closed && c.tool == ev.Tool && c.agentID == ev.AgentID {
			c.result, c.hasRes, c.isError = ev.Text, true, ev.IsError
			m.streaming = false
			m.refresh()
			return
		}
	}
	if ev.IsError {
		m.add(blockError, ev.Text)
		return
	}
	m.add(blockPlain, ev.Text)
}

// closeToolCalls marks all calls without a result as finished.
func (m *Model) closeToolCalls() {
	for _, b := range m.blocks {
		if b.kind == blockTool {
			b.call.closed = true
		}
	}
}

// compactArgs renders raw JSON arguments on one line, at most maxArgsLen runes.
func compactArgs(raw string) string {
	raw = strings.TrimSpace(raw)
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(raw)); err == nil {
		raw = buf.String()
	} else {
		raw = strings.Join(strings.Fields(raw), " ")
	}
	if r := []rune(raw); len(r) > maxArgsLen {
		raw = string(r[:maxArgsLen-1]) + "…"
	}
	return raw
}

// renderTool renders a tool call block of the given total width.
func (m *Model) renderTool(c *toolCall, width int) string {
	header := c.tool
	if args := compactArgs(m.redact(c.args)); args != "" {
		header += " " + args
	}
	if c.agentID != "" && c.agentID != mainAgentID {
		header = "[" + c.agentID + "] " + header
	}
	body := lipgloss.NewStyle().Bold(true).Render(header)
	if c.hasRes {
		if res := m.resultText(c); res != "" {
			if c.isError {
				res = errorStyle.Render(res)
			}
			body += "\n" + res
		}
	}
	inner := width - toolBlockMargin
	if inner < 1 {
		inner = 1
	}
	return toolBorderStyle.Width(inner).Render(body)
}

// resultText returns the redacted result, collapsed unless expanded.
func (m *Model) resultText(c *toolCall) string {
	text := strings.TrimRight(m.redact(c.result), "\r\n")
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if m.expanded || len(lines) <= collapsedLines {
		return text
	}
	more := len(lines) - collapsedLines
	return strings.Join(lines[:collapsedLines], "\n") +
		"\n" + infoStyle.Render(fmt.Sprintf("… (%d more lines, ctrl+o to expand)", more))
}
