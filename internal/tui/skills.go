package tui

import tea "github.com/charmbracelet/bubbletea"

// shadowed reports whether the skill name is hidden by a built-in slash
// command: the built-in always wins and the skill is never run by /<name>.
func shadowed(name string) bool {
	_, ok := slashCommands["/"+name]
	return ok
}

// runSkill runs the loaded, not shadowed skill /name with args; ok is false
// when there is no such skill. The message (body, plus a blank line and the
// args when given) and the typed line pass the session's redactor before they
// reach the transcript, the agent history and so the model and the session
// file. A skill grants nothing: it is an ordinary user message.
func (m Model) runSkill(text, name, args string) (Model, tea.Cmd, bool) {
	if shadowed(name) {
		return m, nil, false
	}
	for _, sk := range m.skills {
		if sk.Name != name {
			continue
		}
		msg := sk.Body
		if args != "" {
			msg += "\n\n" + args
		}
		m, cmd := m.sendUser(m.redact(text), m.redact(msg))
		return m, cmd, true
	}
	return m, nil, false
}

// addSkillWarnings adds one info line per skill skipped by the loader.
func (m *Model) addSkillWarnings() {
	for _, w := range m.skillWarnings {
		m.add(blockInfo, "warning: "+w)
	}
}
