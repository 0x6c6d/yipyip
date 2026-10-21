package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/provider/providertest"
)

func pickerModel(t *testing.T) (tea.Model, func() string) {
	t.Helper()
	srv := providertest.New()
	t.Cleanup(srv.Close)
	m, sess := newSessionModel(t, srv)
	return m, sess.ModelID
}

func pressKey(tm tea.Model, k tea.KeyType) tea.Model {
	tm, _ = tm.Update(tea.KeyMsg{Type: k})
	return tm
}

func TestModelPicker(t *testing.T) {
	tm, cur := pickerModel(t)
	m, _ := tm.(Model).Command("/model")
	tm = m
	v := tm.View()
	if !strings.Contains(v, "> fake/m1") || !strings.Contains(v, "  fake/m2") {
		t.Fatalf("picker not shown or current not marked:\n%s", v)
	}
	// Keys go to the picker, not the input.
	tm = typeText(tm, "x")
	if got := tm.(Model).input.Value(); got != "" {
		t.Fatalf("picker leaked key into input: %q", got)
	}
	tm = pressKey(tm, tea.KeyDown)
	tm = pressKey(tm, tea.KeyDown) // clamps at the end
	if c := tm.(Model).picker.cursor; c != 1 {
		t.Fatalf("cursor = %d, want 1", c)
	}
	tm = pressKey(tm, tea.KeyEnter)
	if cur() != "fake/m2" {
		t.Fatalf("model = %q, want fake/m2", cur())
	}
	if tm.(Model).picker.open {
		t.Fatal("picker still open after Enter")
	}
	v = tm.View()
	if !strings.Contains(v, "model: fake/m2") {
		t.Errorf("missing confirmation:\n%s", v)
	}
	if !strings.Contains(tm.(Model).StatusLine(), "fake/m2") {
		t.Errorf("status line = %q", tm.(Model).StatusLine())
	}
}

func TestModelPickerEsc(t *testing.T) {
	tm, cur := pickerModel(t)
	m, _ := tm.(Model).Command("/model")
	tm = pressKey(m, tea.KeyDown)
	tm = pressKey(tm, tea.KeyEsc)
	if tm.(Model).picker.open {
		t.Fatal("picker still open after Esc")
	}
	if cur() != "fake/m1" {
		t.Fatalf("model = %q, want fake/m1", cur())
	}
	if strings.Contains(tm.View(), "  fake/m2") {
		t.Errorf("picker still visible:\n%s", tm.View())
	}
	// Up/Down at the ends never panic or leave the list.
	m, _ = tm.(Model).Command("/model")
	tm = pressKey(m, tea.KeyUp)
	tm = pressKey(tm, tea.KeyUp)
	if c := tm.(Model).picker.cursor; c != 0 {
		t.Fatalf("cursor = %d, want 0", c)
	}
}

func TestModelDirect(t *testing.T) {
	tm, cur := pickerModel(t)
	m, _ := tm.(Model).Command("/model fake/m2")
	if cur() != "fake/m2" {
		t.Fatalf("model = %q", cur())
	}
	if m.picker.open {
		t.Fatal("picker opened for direct selection")
	}
	if v := m.View(); !strings.Contains(v, "model: fake/m2") || !strings.Contains(m.StatusLine(), "fake/m2") {
		t.Errorf("view:\n%s\nstatus: %s", v, m.StatusLine())
	}
}

func TestModelUnknown(t *testing.T) {
	tm, cur := pickerModel(t)
	m, _ := tm.(Model).Command("/model nope/x")
	if cur() != "fake/m1" {
		t.Fatalf("model = %q, want fake/m1", cur())
	}
	if v := m.View(); !strings.Contains(v, "error: unknown model: nope/x") {
		t.Errorf("missing error:\n%s", v)
	}
}

func TestModelNoModels(t *testing.T) {
	m, _ := newSessionModel(t, nil)
	m, _ = m.Command("/model")
	if m.picker.open || !strings.Contains(m.View(), "no models configured") {
		t.Errorf("view:\n%s", m.View())
	}
}
