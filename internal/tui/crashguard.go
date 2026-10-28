package tui

import tea "github.com/charmbracelet/bubbletea"

// guard wraps a tea.Model so that a panic in Init, Update, View or in one of
// the returned commands is passed to rec before it continues (re-panics), so
// Bubble Tea still restores the terminal. It is a value type, like the model
// it wraps.
type guard struct {
	m   tea.Model
	rec func(v any)
}

// Guard returns m wrapped so that every panic in m's Init, Update and View
// and in the commands they return (including the elements of a tea.BatchMsg)
// is first handed to rec and then re-panicked. tea.Sequence messages cannot
// be inspected and are left as they are. Update returns a guarded copy of
// the new model.
func Guard(m tea.Model, rec func(v any)) tea.Model {
	return guard{m: m, rec: rec}
}

// catch is deferred by every guarded call: it records a panic and re-panics.
func (g guard) catch() {
	if r := recover(); r != nil {
		if g.rec != nil {
			g.rec(r)
		}
		panic(r)
	}
}

// Init calls the wrapped model's Init under the guard.
func (g guard) Init() tea.Cmd {
	defer g.catch()
	return g.wrapCmd(g.m.Init())
}

// Update calls the wrapped model's Update under the guard and returns a
// guarded copy of the new model.
func (g guard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer g.catch()
	m, cmd := g.m.Update(msg)
	return guard{m: m, rec: g.rec}, g.wrapCmd(cmd)
}

// View calls the wrapped model's View under the guard.
func (g guard) View() string {
	defer g.catch()
	return g.m.View()
}

// wrapCmd returns c running under the guard; a tea.BatchMsg it yields has
// each of its commands wrapped as well. nil stays nil.
func (g guard) wrapCmd(c tea.Cmd) tea.Cmd {
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		defer g.catch()
		msg := c()
		if b, ok := msg.(tea.BatchMsg); ok {
			out := make(tea.BatchMsg, len(b))
			for i, bc := range b {
				out[i] = g.wrapCmd(bc)
			}
			return out
		}
		return msg
	}
}
