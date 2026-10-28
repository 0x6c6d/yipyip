package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/agent"
	"yipyip/internal/crashlog"
)

const (
	maxArgsLen     = 200
	collapsedLines = 3
	mainAgentID    = "main"
)

var (
	toolBulletDim = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	toolBulletOK  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	toolBulletErr = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

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

// toolSummaryKeys maps a tool to the JSON argument shown in its header.
var toolSummaryKeys = map[string]string{
	"bash":        "command",
	"read_file":   "path",
	"write_file":  "path",
	"edit_file":   "path",
	"glob":        "pattern",
	"grep":        "pattern",
	"spawn_agent": "task",
}

// toolSummary returns the one-line header summary of a call from its
// (already redacted) JSON arguments.
func toolSummary(tool, args string) string {
	if key, ok := toolSummaryKeys[tool]; ok {
		var obj map[string]json.RawMessage
		if json.Unmarshal([]byte(args), &obj) == nil {
			var v string
			if json.Unmarshal(obj[key], &v) == nil && obj[key] != nil {
				v = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(v)
				if r := []rune(v); len(r) > maxArgsLen {
					v = string(r[:maxArgsLen-1]) + "…"
				}
				return v
			}
		}
	}
	return compactArgs(args)
}

var userStyle = lipgloss.NewStyle().Background(lipgloss.Color("236"))

// indentContinuation indents every line but the first by two spaces.
func indentContinuation(s string) string {
	return strings.ReplaceAll(s, "\n", "\n  ")
}

// renderBlockHook is a test-only hook called at the start of a block render;
// nil in production.
var renderBlockHook func(b *block)

// renderErrorText is what a block shows when its render panicked; it never
// contains block text.
const renderErrorText = "[render error]"

// renderBlock renders one conversation block in Claude Code style, without
// borders. All text passes the redactor before styling. An empty result means
// the block shows nothing. A panic in the render is recovered, logged without
// block text, and replaced by a one-line error.
func (m *Model) renderBlock(b block, width int) (out string) {
	defer func() {
		if v := recover(); v != nil {
			crashlog.Recovered("render block", v)
			out = errorStyle.Render(renderErrorText)
		}
	}()
	if renderBlockHook != nil {
		renderBlockHook(&b)
	}
	return m.renderBlockText(b, width)
}

// renderBlockText does the unguarded render of one block.
func (m *Model) renderBlockText(b block, width int) string {
	if b.kind == blockTool {
		return m.renderTool(b.call, width)
	}
	if b.kind == blockSubagent {
		return m.renderSubagent(b.text)
	}
	text := m.redact(b.text)
	switch b.kind {
	case blockAssistant:
		text = strings.TrimRight(text, "\r\n")
		if strings.TrimSpace(text) == "" {
			return ""
		}
		out := renderMarkdown(text, width-2)
		// Secret guard: rendering may join pieces into a secret (for example
		// "**SEC**RET123"); then the redacted source is shown unrendered.
		if s := ansi.Strip(out); m.redact(s) != s {
			out = text
		}
		return "● " + indentContinuation(out)
	case blockError:
		return errorStyle.Render(text)
	case blockInfo:
		return infoStyle.Render(text)
	case blockMarker:
		if rest, ok := strings.CutPrefix(text, "✻"); ok {
			return thinkingStyle.Render("✻") + thinkingDim.Render(rest)
		}
		return thinkingDim.Render(text)
	}
	if strings.HasPrefix(text, "> ") {
		text = indentContinuation(strings.TrimRight(text, "\r\n"))
		st := userStyle
		if width > 0 {
			st = st.Width(width)
		}
		return st.Render(text)
	}
	return text
}

// renderTool renders a tool call block in Claude Code style, without borders.
func (m *Model) renderTool(c *toolCall, _ int) string {
	bullet := toolBulletDim
	if c.hasRes {
		bullet = toolBulletOK
		if c.isError {
			bullet = toolBulletErr
		}
	}
	header := bullet.Render("●") + " "
	if c.agentID != "" && c.agentID != mainAgentID {
		header += "[" + c.agentID + "] "
	}
	header += lipgloss.NewStyle().Bold(true).Render(c.tool) + "(" + toolSummary(c.tool, m.redact(c.args)) + ")"
	if !c.hasRes {
		return header
	}
	res, hint := m.resultLines(c)
	for i, l := range res {
		prefix := "     "
		if i == 0 {
			prefix = "  ⎿  "
		}
		if c.isError {
			l = errorStyle.Render(l)
		}
		header += "\n" + prefix + l
	}
	if hint != "" {
		header += "\n     " + infoStyle.Render(hint)
	}
	return header
}

// resultLines returns the redacted result lines, collapsed unless expanded,
// plus the collapse hint (empty when nothing is hidden).
func (m *Model) resultLines(c *toolCall) (lines []string, hint string) {
	text := strings.TrimRight(m.redact(c.result), "\r\n")
	if text == "" {
		return nil, ""
	}
	lines = strings.Split(text, "\n")
	if m.expanded || len(lines) <= collapsedLines {
		return lines, ""
	}
	more := len(lines) - collapsedLines
	return lines[:collapsedLines], fmt.Sprintf("… +%d lines (ctrl+o to expand)", more)
}
