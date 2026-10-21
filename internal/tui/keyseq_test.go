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

// feedSequence parses raw with bubbletea and feeds every resulting message
// that is not a bubbletea-internal one to the model, with "a" typed first.
func feedSequence(t *testing.T, raw string) tea.Model {
	t.Helper()
	var m tea.Model = New(Options{StartDir: "/work"})
	m = typeText(m, "a")
	for _, msg := range parseWithBubbletea(t, raw) {
		if _, isKey := msg.(tea.KeyMsg); isKey {
			continue
		}
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Fatalf("%q quit the program", raw)
			}
		}
	}
	return m
}

func TestShiftEnterFromModifyOtherKeys(t *testing.T) {
	m := feedSequence(t, "\x1b[27;2;13~")
	if got := m.(Model).input.Value(); got != "a\n" {
		t.Fatalf("input after ESC[27;2;13~ = %q, want %q", got, "a\n")
	}
}

func TestShiftEnterFromCSIu(t *testing.T) {
	m := feedSequence(t, "\x1b[13;2u")
	if got := m.(Model).input.Value(); got != "a\n" {
		t.Fatalf("input after ESC[13;2u = %q, want %q", got, "a\n")
	}
}

func TestUnknownCSINotShiftEnterIgnored(t *testing.T) {
	for _, raw := range []string{"\x1b[27;2;14~", "\x1b[27;5;13~", "\x1b[14;2u", "\x1b[27;2~", "\x1b[27;x;13~", "\x1b[99;99;99~"} {
		m := feedSequence(t, raw)
		if got := m.(Model).input.Value(); got != "a" {
			t.Errorf("input after %q = %q, want %q", raw, got, "a")
		}
	}
	// Messages that are not unknown CSI sequences pass through untouched.
	var m tea.Model = New(Options{StartDir: "/work"})
	m, _ = m.Update(struct{}{})
	m, _ = m.Update([]byte("\x1b[27;2;13~"))
	if got := m.(Model).input.Value(); got != "" {
		t.Fatalf("foreign messages changed the input: %q", got)
	}
}

func TestCtrlLetterFromModifyOtherKeys(t *testing.T) {
	msgs := parseWithBubbletea(t, "\x1b[27;5;99~")
	for _, msg := range msgs {
		if k, ok := keyFromCSI(msg); ok {
			if k.String() != "ctrl+c" {
				t.Fatalf("ESC[27;5;99~ = %q, want ctrl+c", k.String())
			}
			return
		}
	}
	t.Fatalf("no unknown CSI message among %v", msgs)
}

func TestAltEnterStillNewline(t *testing.T) {
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
