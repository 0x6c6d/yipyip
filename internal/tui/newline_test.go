package tui

import (
	"bytes"
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// recorder is a tiny model that records every message it receives and quits
// when its input is read to the end.
type recorder struct{ msgs *[]tea.Msg }

func (r recorder) Init() tea.Cmd { return nil }
func (r recorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	*r.msgs = append(*r.msgs, msg)
	return r, nil
}
func (r recorder) View() string { return "" }

// parseWithBubbletea runs raw through bubbletea's own input parser and
// returns the messages it produced. The unknown-CSI message type is
// unexported, so this is the only way to build one.
func parseWithBubbletea(t *testing.T, raw string) []tea.Msg {
	t.Helper()
	var msgs []tea.Msg
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := tea.NewProgram(recorder{&msgs}, tea.WithInput(bytes.NewReader([]byte(raw))),
		tea.WithOutput(&bytes.Buffer{}), tea.WithoutRenderer(), tea.WithContext(ctx),
		tea.WithoutSignals())
	go func() {
		time.Sleep(300 * time.Millisecond)
		p.Quit()
	}()
	_, _ = p.Run()
	return msgs
}

func TestNewline(t *testing.T) {
	for _, raw := range []string{"\x1b[27;2;13~", "\x1b[13;2u"} {
		var m tea.Model = New(Options{StartDir: "/work"})
		m = typeText(m, "a")
		for _, msg := range parseWithBubbletea(t, raw) {
			var cmd tea.Cmd
			m, cmd = m.Update(msg)
			if cmd != nil {
				if _, quit := cmd().(tea.QuitMsg); quit {
					t.Fatalf("%q quit the program", raw)
				}
			}
		}
		mm := m.(Model)
		if got := mm.input.Value(); got != "a" {
			t.Fatalf("input after %q = %q, want %q", raw, got, "a")
		}
		if len(mm.blocks) != 0 {
			t.Fatalf("%q submitted the input (%d blocks)", raw, len(mm.blocks))
		}
	}
	var m tea.Model = New(Options{StartDir: "/work"})
	m = typeText(m, "a")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if got := m.(Model).input.Value(); got != "a\n" {
		t.Fatalf("input after Alt+Enter = %q, want %q", got, "a\n")
	}
}

func TestAltEnterUnchanged(t *testing.T) {
	var m tea.Model = New(Options{StartDir: "/work"})
	m = typeText(m, "a")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	mm := m.(Model)
	if got := mm.input.Value(); got != "a\n" {
		t.Fatalf("input after Alt+Enter = %q, want %q", got, "a\n")
	}
	if len(mm.blocks) != 0 {
		t.Fatalf("Alt+Enter submitted the input (%d blocks)", len(mm.blocks))
	}
}
