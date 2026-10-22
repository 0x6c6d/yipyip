package tui

import (
	"errors"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
)

// commandFunc handles one slash command; args is the text after the name.
type commandFunc func(m Model, args string) (Model, tea.Cmd)

// slashCommands is the dispatcher table of the slash commands, used by Enter
// and Model.Command.
var slashCommands = map[string]commandFunc{
	"/help":     cmdHelp,
	"/new":      cmdNew,
	"/clear":    cmdNew,
	"/exit":     cmdQuit,
	"/quit":     cmdQuit,
	"/effort":   cmdEffort,
	"/mode":     cmdMode,
	"/model":    cmdModel,
	"/agents":   cmdAgents,
	"/init":     cmdInit,
	"/compact":  cmdCompact,
	"/undo":     cmdUndo,
	"/sessions": cmdSessions,
}

// runSlash dispatches a line that starts with "/". It never reaches the model.
func (m Model) runSlash(text string) (Model, tea.Cmd) {
	name, args := text, ""
	if i := strings.IndexFunc(text, unicode.IsSpace); i >= 0 {
		name, args = text[:i], strings.TrimSpace(text[i:])
	}
	if fn, ok := slashCommands[name]; ok {
		return fn(m, args)
	}
	if m, cmd, ok := m.runSkill(text, strings.TrimPrefix(name, "/"), args); ok {
		return m, cmd
	}
	m.add(blockInfo, "unknown command: "+name)
	return m, nil
}

func cmdHelp(m Model, _ string) (Model, tea.Cmd) {
	m.add(blockInfo, strings.TrimRight(HelpText(), "\n"))
	return m, nil
}

func cmdQuit(m Model, _ string) (Model, tea.Cmd) {
	m = m.saveTabs()
	return m, m.quit()
}

// cmdNew is /new and /clear: the fresh session opens in a new tab; the old
// tab (and any saved file of it) stays as it is. At the tab limit nothing
// changes but one info line.
func cmdNew(m Model, _ string) (Model, tea.Cmd) {
	switch err := m.addTab(); {
	case errors.Is(err, errTabLimit):
		m.add(blockInfo, tabLimitMsg)
		return m, nil
	case err != nil:
		m.add(blockError, err.Error())
		return m, nil
	}
	m.add(blockInfo, "new session")
	return m, m.waitEvent()
}

func cmdEffort(m Model, args string) (Model, tea.Cmd) {
	if args == "" {
		m.add(blockInfo, "effort: "+m.effort())
		return m, nil
	}
	switch args {
	case agent.EffortLow, agent.EffortMedium, agent.EffortHigh:
	default:
		m.add(blockError, "error: effort must be low, medium or high")
		return m, nil
	}
	prev := m.currentSel()
	if sess := m.sess; sess != nil {
		if err := sess.SetEffort(args); err != nil {
			m.add(blockError, "error: effort must be low, medium or high")
			return m, nil
		}
	}
	model, _, mode := m.globals()
	m.setGlobals(model, args, mode)
	m.add(blockInfo, "effort: "+args)
	m.saveSettingsIfChanged(prev)
	return m, nil
}

func cmdMode(m Model, args string) (Model, tea.Cmd) {
	prev := m.currentSel()
	switch args {
	case "":
		m.toggleMode()
	case string(agent.ModeAuto), string(agent.ModeManual):
		m.setMode(agent.Mode(args))
	default:
		m.add(blockError, "error: mode must be auto or manual")
		return m, nil
	}
	m.add(blockInfo, "mode: "+m.mode())
	m.saveSettingsIfChanged(prev)
	return m, nil
}

func (m Model) effort() string {
	if sess := m.sess; sess != nil {
		return sess.Effort()
	}
	return m.opts.Effort
}

func (m Model) mode() string {
	if sess := m.sess; sess != nil {
		return string(sess.Mode())
	}
	return m.opts.Mode
}

// setMode sets the global mode (all tabs).
func (m *Model) setMode(mode agent.Mode) {
	model, effort, _ := m.globals()
	m.setGlobals(model, effort, string(mode))
}

// toggleMode switches the global mode between manual and auto.
func (m *Model) toggleMode() {
	if m.mode() == string(agent.ModeAuto) {
		m.setMode(agent.ModeManual)
	} else {
		m.setMode(agent.ModeAuto)
	}
}

// completions returns the command names matching the input while it starts
// with "/" and has no whitespace; nil otherwise.
func (m Model) completions() []string {
	v := m.input.Value()
	if !strings.HasPrefix(v, "/") || strings.IndexFunc(v, unicode.IsSpace) >= 0 {
		return nil
	}
	var out []string
	for _, c := range commands {
		if strings.HasPrefix(c.Name, v) {
			out = append(out, c.Name)
		}
	}
	return out
}

// completeInput applies Tab: the single match, or the longest common prefix.
func (m Model) completeInput() Model {
	matches := m.completions()
	if len(matches) == 0 {
		return m
	}
	prefix := matches[0]
	for _, s := range matches[1:] {
		for !strings.HasPrefix(s, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	if len(prefix) > len(m.input.Value()) {
		m.input.SetValue(prefix)
	}
	return m
}
