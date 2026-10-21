package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
)

// scrollModel returns a model with a 200-block conversation, at the bottom.
func scrollModel(t *testing.T) Model {
	t.Helper()
	nm, _ := New(Options{StartDir: "/x"}).Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m := nm.(Model)
	for i := 0; i < 200; i++ {
		m.add(blockInfo, fmt.Sprintf("line %03d", i))
	}
	return m
}

func upd(m Model, msg tea.Msg) Model {
	nm, _ := m.Update(msg)
	return nm.(Model)
}

func TestScrollWheel(t *testing.T) {
	m := scrollModel(t)
	if !m.viewport.AtBottom() {
		t.Fatal("expected the view at the bottom")
	}
	start := m.viewport.YOffset
	m = upd(m, tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.viewport.YOffset != start-3 {
		t.Fatalf("wheel up: YOffset %d, want %d", m.viewport.YOffset, start-3)
	}
	m = upd(m, tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.viewport.YOffset != start {
		t.Fatalf("wheel down: YOffset %d, want %d", m.viewport.YOffset, start)
	}
	m = upd(m, tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.viewport.YOffset != start {
		t.Fatalf("left button scrolled: YOffset %d", m.viewport.YOffset)
	}
}

func TestScrollCtrlHomeEnd(t *testing.T) {
	m := scrollModel(t)
	m = upd(m, tea.KeyMsg{Type: tea.KeyCtrlHome})
	if v := plainView(m); !strings.Contains(v, "line 000") {
		t.Fatalf("Ctrl+Home: no line 000 in view:\n%s", v)
	}
	m = upd(m, tea.KeyMsg{Type: tea.KeyCtrlEnd})
	v := plainView(m)
	if !strings.Contains(v, "line 199") || strings.Contains(v, "line 000") {
		t.Fatalf("Ctrl+End: unexpected view:\n%s", v)
	}
	// Also with text in the input.
	m.input.SetValue("abc")
	m = upd(m, tea.KeyMsg{Type: tea.KeyCtrlHome})
	if v := plainView(m); !strings.Contains(v, "line 000") {
		t.Fatalf("Ctrl+Home with text: no line 000 in view")
	}
}

func TestScrollHomeEndEmptyInput(t *testing.T) {
	m := scrollModel(t)
	m = upd(m, tea.KeyMsg{Type: tea.KeyHome})
	if v := plainView(m); !strings.Contains(v, "line 000") {
		t.Fatalf("Home: no line 000 in view:\n%s", v)
	}
	m = upd(m, tea.KeyMsg{Type: tea.KeyEnd})
	if v := plainView(m); !strings.Contains(v, "line 199") {
		t.Fatalf("End: no line 199 in view:\n%s", v)
	}
}

func TestScrollHomeEndWithText(t *testing.T) {
	m := scrollModel(t)
	m.input.SetValue("abc")
	m.input.CursorEnd()
	before := m.viewport.YOffset
	m = upd(m, tea.KeyMsg{Type: tea.KeyHome})
	if m.viewport.YOffset != before {
		t.Fatalf("Home with text scrolled: %d -> %d", before, m.viewport.YOffset)
	}
	if col := m.input.LineInfo().ColumnOffset; col != 0 {
		t.Fatalf("cursor column %d, want 0", col)
	}
	m = upd(m, tea.KeyMsg{Type: tea.KeyEnd})
	if m.viewport.YOffset != before {
		t.Fatalf("End with text scrolled")
	}
	if col := m.input.LineInfo().ColumnOffset; col != 3 {
		t.Fatalf("cursor column %d, want 3", col)
	}
}

func TestScrollNoSnapBack(t *testing.T) {
	m := scrollModel(t)
	m = upd(m, tea.KeyMsg{Type: tea.KeyPgUp})
	m = upd(m, tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	off := m.viewport.YOffset
	if m.viewport.AtBottom() {
		t.Fatal("expected to be scrolled up")
	}
	m.add(blockInfo, "line 200")
	if m.viewport.YOffset != off {
		t.Fatalf("new block moved the view: %d -> %d", off, m.viewport.YOffset)
	}
	m = feed(m, agent.Event{Kind: agent.EvText, AgentID: "main", Text: "streamed delta"}).(Model)
	if m.viewport.YOffset != off {
		t.Fatalf("text delta moved the view: %d -> %d", off, m.viewport.YOffset)
	}

	m = upd(m, tea.KeyMsg{Type: tea.KeyCtrlEnd})
	m.add(blockInfo, "line 201")
	if !m.viewport.AtBottom() || !strings.Contains(plainView(m), "line 201") {
		t.Fatal("at the bottom a new block must be followed")
	}
}
