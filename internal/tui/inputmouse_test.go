package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInputClick(t *testing.T) {
	clip := &fakeClip{}
	m := upd(New(Options{StartDir: "/x", Clipboard: clip.write}), tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue("abc\ndefgh\nij")
	top := tabBarRows + m.viewport.Height + 1
	m.add(blockInfo, "hello conversation")

	cursor := func(m Model) (int, int) {
		li := m.input.LineInfo()
		return m.input.Line(), li.StartColumn + li.ColumnOffset
	}
	click := func(m Model, x, y int) Model {
		return upd(m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	}
	check := func(t *testing.T, m Model, line, col int) {
		t.Helper()
		if l, c := cursor(m); l != line || c != col {
			t.Fatalf("cursor = line %d col %d, want line %d col %d", l, c, line, col)
		}
	}

	t.Run("moves", func(t *testing.T) {
		m := click(m, 1+2, top+1)
		check(t, m, 1, 2)
	})
	t.Run("clamped to line end", func(t *testing.T) {
		m := click(m, 1+10, top)
		check(t, m, 0, 3)
	})
	t.Run("row 2 column 0", func(t *testing.T) {
		m := click(m, 1, top+2)
		check(t, m, 2, 0)
	})
	t.Run("outside leaves cursor", func(t *testing.T) {
		m := click(m, 1+1, top+1)
		check(t, m, 1, 1)
		for _, y := range []int{tabBarRows, 0, top + inputRows + 1} {
			m = click(m, 3, y)
			check(t, m, 1, 1)
		}
		if len(clip.texts) != 0 {
			t.Fatalf("clipboard written: %q", clip.texts)
		}
	})
}
