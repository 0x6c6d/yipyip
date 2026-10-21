package tui

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yipyip/internal/agent"
)

// picker is the state of the /model picker. It holds only plain values, so
// copies of the Model never share it.
type picker struct {
	open   bool
	items  []string // provider/model ids, snapshot taken when the picker opened
	cursor int
	cur    string // id that was active when the picker opened
}

// cmdModel implements /model: a picker without argument, direct selection
// with one.
func cmdModel(m Model, args string) (Model, tea.Cmd) {
	sess := m.sess
	if args == "" {
		var items []string
		if sess != nil {
			items = sess.Models()
		}
		if len(items) == 0 {
			m.add(blockInfo, "no models configured")
			return m, nil
		}
		cur := sess.ModelID()
		cursor := 0
		for i, id := range items {
			if id == cur {
				cursor = i
			}
		}
		m.picker = picker{open: true, items: append([]string(nil), items...), cursor: cursor, cur: cur}
		return m, nil
	}
	return m.setModel(args), nil
}

// setModel switches the session to id and reports the result.
func (m Model) setModel(id string) Model {
	sess := m.sess
	if sess == nil {
		m.add(blockError, "error: unknown model: "+id)
		return m
	}
	prev := m.currentSel()
	if err := sess.SetModel(id); err != nil {
		if errors.Is(err, agent.ErrUnknownModel) {
			m.add(blockError, "error: unknown model: "+id)
		} else {
			m.add(blockError, "error: "+err.Error())
		}
		return m
	}
	_, effort, mode := m.globals()
	m.setGlobals(id, effort, mode)
	m.add(blockInfo, "model: "+id)
	m.saveSettingsIfChanged(prev)
	return m
}

// pickerKey handles one message while the picker is open. Ctrl+C is not
// handled here (the caller keeps the normal rule); everything else is
// swallowed.
func (m Model) pickerKey(msg tea.Msg) (Model, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	p := &m.picker
	switch km.String() {
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "j":
		if p.cursor < len(p.items)-1 {
			p.cursor++
		}
	case "esc":
		m.picker = picker{}
	case "enter":
		var id string
		if p.cursor >= 0 && p.cursor < len(p.items) {
			id = p.items[p.cursor]
		}
		m.picker = picker{}
		if id != "" {
			m = m.setModel(id)
		}
	}
	return m, nil
}

// pickerLines renders the picker rows: "> id" marks the current model,
// the cursor row is bold.
func (m Model) pickerLines() []string {
	p := m.picker
	lines := make([]string, len(p.items))
	for i, id := range p.items {
		mark := "  "
		if id == p.cur {
			mark = "> "
		}
		line := mark + id
		if i == p.cursor {
			line = lipgloss.NewStyle().Bold(true).Reverse(true).Render(line)
		}
		lines[i] = line
	}
	return lines
}

// overlayPicker replaces the bottom lines of the viewport view vp with the
// picker rows (keeping at least one viewport line).
func (m Model) overlayPicker(vp string) string {
	rows := m.pickerLines()
	lines := strings.Split(vp, "\n")
	if max := len(lines) - 1; len(rows) > max {
		// Keep the window around the cursor when the list does not fit.
		start := m.picker.cursor - max + 1
		if start < 0 {
			start = 0
		}
		if max < 0 {
			max = 0
		}
		rows = rows[start : start+min(max, len(rows)-start)]
	}
	keep := len(lines) - len(rows)
	if keep < 0 {
		keep = 0
	}
	return strings.Join(append(append([]string(nil), lines[:keep]...), rows...), "\n")
}
