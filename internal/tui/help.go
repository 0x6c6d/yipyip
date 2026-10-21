// Package tui holds the terminal UI pieces of yip. This file has the static
// help text shown by `yip --help` and the /help command.
package tui

import (
	"fmt"
	"strings"
)

// CommandInfo describes one slash command.
type CommandInfo struct{ Name, Args, Help string }

var commands = []CommandInfo{
	{Name: "/model", Args: "[provider/model]", Help: "pick or set the model"},
	{Name: "/effort", Args: "[low|medium|high]", Help: "show or set the reasoning effort"},
	{Name: "/mode", Args: "[auto|manual]", Help: "switch approval mode"},
	{Name: "/agents", Args: "[id | kill id]", Help: "list, show or cancel subagents"},
	{Name: "/init", Help: "write or update AGENTS.md"},
	{Name: "/compact", Help: "summarize the conversation"},
	{Name: "/undo", Help: "undo the last turn's file changes"},
	{Name: "/sessions", Args: "[id | rm id]", Help: "list, load or remove saved sessions"},
	{Name: "/new", Help: "open a fresh session in a new tab"},
	{Name: "/clear", Help: "alias of /new"},
	{Name: "/exit", Help: "quit"},
	{Name: "/quit", Help: "alias of /exit"},
	{Name: "/help", Help: "show this help"},
}

var keys = []struct{ Key, Help string }{
	{"Enter", "send; while the agent is busy: queue the message"},
	{"Alt+Enter / Shift+Enter", "new line (Alt+Enter always; Shift+Enter if the terminal reports it)"},
	{"Esc", "cancel the running turn; queued messages return to the input"},
	{"Shift+Tab", "toggle auto/manual mode (auto is the default)"},
	{"Ctrl+C", "twice: quit"},
	{"Tab/Right", "accept a suggestion or complete a command"},
	{"Up/Down", "input history (Up first restores queued messages)"},
	{"Ctrl+O", "expand tool results"},
	{"PgUp/PgDn", "scroll"},
	{"Mouse wheel", "scroll 3 lines"},
	{"Ctrl+Home/Ctrl+End", "scroll to the first/last line (Home/End with an empty input)"},
	{"Mouse drag", "select text; releasing copies it to the clipboard"},
	{"Ctrl+T", "new tab (up to 8)"},
	{"Ctrl+W", "close the tab (saves it first)"},
	{"Ctrl+Tab/Ctrl+Shift+Tab", "next/previous tab"},
	{"Alt+1..Alt+8", "switch to tab 1..8"},
}

// Commands returns the slash commands in display order.
func Commands() []CommandInfo {
	out := make([]CommandInfo, len(commands))
	copy(out, commands)
	return out
}

// HelpText returns one line per command, followed by a Keys: block.
func HelpText() string {
	labels := make([]string, len(commands))
	width := 0
	for i, c := range commands {
		labels[i] = c.Name
		if c.Args != "" {
			labels[i] += " " + c.Args
		}
		if len(labels[i]) > width {
			width = len(labels[i])
		}
	}

	var b strings.Builder
	for i, c := range commands {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, labels[i], c.Help)
	}
	b.WriteString("\nKeys:\n")
	keyWidth := 0
	for _, k := range keys {
		if len(k.Key) > keyWidth {
			keyWidth = len(k.Key)
		}
	}
	for _, k := range keys {
		fmt.Fprintf(&b, "  %-*s  %s\n", keyWidth, k.Key, k.Help)
	}
	return b.String()
}
