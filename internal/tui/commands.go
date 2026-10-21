package tui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
)

// commandFunc handles one slash command; args is the text after the name.
type commandFunc func(m Model, args string) (Model, tea.Cmd)

// slashCommands is the dispatcher table of the slash commands, used by Enter
// and Model.Command. Later tasks replace the "not available yet" entries.
var slashCommands = map[string]commandFunc{
	"/help":    cmdHelp,
	"/new":     cmdNew,
	"/clear":   cmdNew,
	"/exit":    cmdQuit,
	"/quit":    cmdQuit,
	"/effort":  cmdEffort,
	"/mode":    cmdMode,
	"/model":   notAvailable("/model"),
	"/agents":  notAvailable("/agents"),
	"/init":    notAvailable("/init"),
	"/compact": notAvailable("/compact"),
	"/undo":    notAvailable("/undo"),
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
	m.add(blockInfo, "unknown command: "+name)
	return m, nil
}

func notAvailable(name string) commandFunc {
	return func(m Model, _ string) (Model, tea.Cmd) {
		m.add(blockInfo, name+": not available yet")
		return m, nil
	}
}

func cmdHelp(m Model, _ string) (Model, tea.Cmd) {
	m.add(blockInfo, strings.TrimRight(HelpText(), "\n"))
	return m, nil
}

func cmdQuit(m Model, _ string) (Model, tea.Cmd) { return m, m.quit() }

func cmdNew(m Model, _ string) (Model, tea.Cmd) {
	if sess := m.opts.Session; sess != nil {
		sess.Reset()
	}
	m.blocks = nil
	m.streaming = false
	m.add(blockInfo, "new session")
	return m, nil
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
	if sess := m.opts.Session; sess != nil {
		if err := sess.SetEffort(args); err != nil {
			m.add(blockError, "error: effort must be low, medium or high")
			return m, nil
		}
	} else {
		m.opts.Effort = args
	}
	m.add(blockInfo, "effort: "+args)
	return m, nil
}

func cmdMode(m Model, args string) (Model, tea.Cmd) {
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
	return m, nil
}

func (m Model) effort() string {
	if sess := m.opts.Session; sess != nil {
		return sess.Effort()
	}
	return m.opts.Effort
}

func (m Model) mode() string {
	if sess := m.opts.Session; sess != nil {
		return string(sess.Mode())
	}
	return m.opts.Mode
}

func (m *Model) setMode(mode agent.Mode) {
	if sess := m.opts.Session; sess != nil {
		sess.SetMode(mode)
		return
	}
	m.opts.Mode = string(mode)
}

func (m *Model) toggleMode() {
	if sess := m.opts.Session; sess != nil {
		sess.ToggleMode()
		return
	}
	if m.opts.Mode == string(agent.ModeAuto) {
		m.opts.Mode = string(agent.ModeManual)
	} else {
		m.opts.Mode = string(agent.ModeAuto)
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
