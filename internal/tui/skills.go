package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/skills"
)

// init registers /skills; the table entry would otherwise form an
// initialization cycle through shadowed, which reads slashCommands.
func init() { slashCommands["/skills"] = cmdSkills }

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

// cmdSkills is /skills: it re-reads the skill folders (new, changed and
// removed skill files take effect), shows the loader warnings, then one line
// per skill with its source and, when hidden by a built-in, a shadow note.
func cmdSkills(m Model, _ string) (Model, tea.Cmd) {
	if m.opts.StartDir != "" {
		m.skills, m.skillWarnings = skills.Load(m.opts.Home, m.opts.StartDir)
	}
	m.addSkillWarnings()
	if len(m.skills) == 0 {
		m.add(blockInfo, "no skills found")
		return m, nil
	}
	lines := make([]string, len(m.skills))
	for i, sk := range m.skills {
		line := "/" + sk.Name
		if sk.Description != "" {
			line += "  " + sk.Description
		}
		line += "  (" + string(sk.Source) + ")"
		if shadowed(sk.Name) {
			line += "  shadowed by built-in"
		}
		lines[i] = line
	}
	m.add(blockInfo, m.redact(strings.Join(lines, "\n")))
	return m, nil
}

// skillCompletions returns "/"+name of every loaded, non-shadowed skill that
// starts with the typed prefix v.
func (m Model) skillCompletions(v string) []string {
	var out []string
	for _, sk := range m.skills {
		if name := "/" + sk.Name; !shadowed(sk.Name) && strings.HasPrefix(name, v) {
			out = append(out, name)
		}
	}
	return out
}

// maxSuggestDesc is the most cells a skill description takes in the
// completion line.
const maxSuggestDesc = 40

// completionLine draws the completion matches: built-ins by name, skills as
// "/name (description)" with the redacted description cut to maxSuggestDesc.
func (m Model) completionLine(matches []string) string {
	out := make([]string, len(matches))
	for i, name := range matches {
		out[i] = name
		if _, builtin := slashCommands[name]; builtin {
			continue
		}
		for _, sk := range m.skills {
			if "/"+sk.Name == name && sk.Description != "" {
				out[i] += " (" + ansi.Truncate(m.redact(sk.Description), maxSuggestDesc, "…") + ")"
				break
			}
		}
	}
	return strings.Join(out, " ")
}
