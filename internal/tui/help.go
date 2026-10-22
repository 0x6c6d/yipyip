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
	{Name: "/jev", Args: "[stats]", Help: "toggle jev turn routing; /jev stats shows routing counters"},
	{Name: "/skills", Help: "list the loaded skills"},
	{Name: "/new", Help: "open a fresh session in a new tab"},
	{Name: "/clear", Help: "alias of /new"},
	{Name: "/exit", Help: "quit"},
	{Name: "/quit", Help: "alias of /exit"},
	{Name: "/help", Help: "show this help"},
}

var keys = []struct{ Key, Help string }{
	{"Enter", "send; while the agent is busy: queue the message"},
	{"Alt+Enter", "new line"},
	{"Esc", "cancel the running turn; queued messages return to the input"},
	{"Shift+Tab", "toggle auto/manual mode (auto is the default)"},
	{"Ctrl+C", "twice: quit"},
	{"Tab/Right", "accept a suggestion or complete a command"},
	{"Up/Down", "input history (Up first restores queued messages)"},
	{"Ctrl+O", "expand tool results"},
	{"PgUp/PgDn, Mouse wheel", "scroll (the wheel scrolls 3 lines)"},
	{"Ctrl+Home/Ctrl+End", "scroll to the first/last line (Home/End with an empty input)"},
	{"Mouse drag", "select text; releasing copies it (in the input, a click moves the cursor)"},
	{"Ctrl+T / Ctrl+W", "new tab (up to 8) / close the tab (saves it first)"},
	{"Click tab / × / +", "switch tab / close it / new tab"},
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
	fmt.Fprintf(&b, "  %-*s  %s\n", width, "/<skill> [args]", "run a skill (see /skills); built-in commands win")
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
	b.WriteString("\nWeb search:\n")
	b.WriteString("  web_search  search the web (Brave) when a \"search\" block is configured, key from BRAVE_API_KEY; asks for approval in manual mode\n")
	return b.String()
}
