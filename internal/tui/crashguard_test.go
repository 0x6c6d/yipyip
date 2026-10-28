package tui

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// guardRecorder collects what the guard hands to rec.
type guardRecorder struct {
	mu     sync.Mutex
	values []any
	stacks []string
}

// rec records v and the current stack.
func (r *guardRecorder) rec(v any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = append(r.values, v)
	r.stacks = append(r.stacks, string(debug.Stack()))
}

// snapshot returns copies of the recorded values and stacks.
func (r *guardRecorder) snapshot() ([]any, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]any(nil), r.values...), append([]string(nil), r.stacks...)
}

// panicUpdateModel panics on its first key message.
type panicUpdateModel struct{}

func (panicUpdateModel) Init() tea.Cmd { return func() tea.Msg { return tea.KeyMsg{} } }
func (m panicUpdateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		panic("update boom")
	}
	return m, nil
}
func (panicUpdateModel) View() string { return "" }

// panicViewModel panics in View.
type panicViewModel struct{}

func (panicViewModel) Init() tea.Cmd                         { return nil }
func (m panicViewModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (panicViewModel) View() string                          { panic("view boom") }

// panicCmdModel returns a batch whose second command panics.
type panicCmdModel struct{}

func (panicCmdModel) Init() tea.Cmd {
	return tea.Batch(func() tea.Msg { return nil }, panicCmdModelBoom)
}
func (m panicCmdModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (panicCmdModel) View() string                          { return "" }

// panicCmdModelBoom is the panicking command of panicCmdModel.
func panicCmdModelBoom() tea.Msg { panic("cmd boom") }

// runGuarded runs m guarded in a headless program and returns Run's error.
func runGuarded(t *testing.T, m tea.Model, r *guardRecorder) error {
	t.Helper()
	p := tea.NewProgram(Guard(m, r.rec), tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		p.Kill()
		t.Fatal("program did not stop")
		return nil
	}
}

// checkGuardPanic asserts one record with value want and a stack naming fn.
func checkGuardPanic(t *testing.T, err error, r *guardRecorder, want, fn string) {
	t.Helper()
	if !errors.Is(err, tea.ErrProgramPanic) {
		t.Fatalf("Run error = %v, want ErrProgramPanic", err)
	}
	vals, stacks := r.snapshot()
	if len(vals) != 1 {
		t.Fatalf("recorded %d panics, want 1: %v", len(vals), vals)
	}
	if fmt.Sprint(vals[0]) != want {
		t.Fatalf("recorded value %v, want %q", vals[0], want)
	}
	if !strings.Contains(stacks[0], fn) {
		t.Fatalf("stack does not name %s:\n%s", fn, stacks[0])
	}
}

func TestGuardUpdatePanic(t *testing.T) {
	var r guardRecorder
	err := runGuarded(t, panicUpdateModel{}, &r)
	checkGuardPanic(t, err, &r, "update boom", "panicUpdateModel.Update")
}

func TestGuardViewPanic(t *testing.T) {
	var r guardRecorder
	err := runGuarded(t, panicViewModel{}, &r)
	checkGuardPanic(t, err, &r, "view boom", "panicViewModel.View")
}

func TestGuardCmdPanic(t *testing.T) {
	var r guardRecorder
	err := runGuarded(t, panicCmdModel{}, &r)
	checkGuardPanic(t, err, &r, "cmd boom", "panicCmdModelBoom")
}

// countModel is a plain value model: it counts messages and quits on "q".
type countModel struct{ n int }

type passMsg struct{ v int }

func (countModel) Init() tea.Cmd { return func() tea.Msg { return passMsg{1} } }
func (m countModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.n++
	switch msg := msg.(type) {
	case passMsg:
		return m, tea.Batch(func() tea.Msg { return passMsg{msg.v + 1} }, func() tea.Msg { return passMsg{msg.v + 2} })
	case tea.KeyMsg:
		return m, nil
	}
	return m, func() tea.Msg { return passMsg{7} }
}
func (m countModel) View() string { return fmt.Sprintf("n=%d", m.n) }

func TestGuardPassThrough(t *testing.T) {
	var r guardRecorder
	plain := countModel{}
	g := Guard(plain, r.rec)

	if g.View() != plain.View() {
		t.Fatalf("View = %q, want %q", g.View(), plain.View())
	}
	if got, want := g.Init()(), plain.Init()(); got != want {
		t.Fatalf("Init msg = %v, want %v", got, want)
	}

	// A key: no command, the new model counts like the plain one.
	g2, cmd := g.Update(tea.KeyMsg{})
	p2, pcmd := plain.Update(tea.KeyMsg{})
	if cmd != nil || pcmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if g2.View() != p2.View() || g2.View() != "n=1" {
		t.Fatalf("View after Update = %q, want %q", g2.View(), p2.View())
	}
	if g.View() != "n=0" {
		t.Fatalf("original guarded model changed: %q", g.View())
	}
	if _, ok := g2.(guard); !ok {
		t.Fatalf("Update returned %T, want a guarded model", g2)
	}

	// A batch: the elements yield the same messages as the plain ones.
	_, cmd = g2.Update(passMsg{1})
	_, pcmd = p2.Update(passMsg{1})
	gb, ok1 := cmd().(tea.BatchMsg)
	pb, ok2 := pcmd().(tea.BatchMsg)
	if !ok1 || !ok2 || len(gb) != len(pb) {
		t.Fatalf("batch = %v / %v", gb, pb)
	}
	for i := range gb {
		if got, want := gb[i](), pb[i](); got != want {
			t.Fatalf("batch[%d] = %v, want %v", i, got, want)
		}
	}

	// A plain command.
	_, cmd = g2.Update("other")
	if got := cmd(); got != (passMsg{7}) {
		t.Fatalf("cmd msg = %v, want %v", got, passMsg{7})
	}
	if vals, _ := r.snapshot(); len(vals) != 0 {
		t.Fatalf("recorded %v without a panic", vals)
	}

	// A full program run quits normally and records nothing.
	quit := Guard(quitModel{}, r.rec)
	p := tea.NewProgram(quit, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	final, err := p.Run()
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	if final.View() != "done" {
		t.Fatalf("final View = %q", final.View())
	}
	if vals, _ := r.snapshot(); len(vals) != 0 {
		t.Fatalf("recorded %v without a panic", vals)
	}
}

// quitModel quits after its first message.
type quitModel struct{ done bool }

func (quitModel) Init() tea.Cmd { return func() tea.Msg { return passMsg{} } }
func (m quitModel) Update(tea.Msg) (tea.Model, tea.Cmd) {
	m.done = true
	return m, tea.Quit
}
func (m quitModel) View() string {
	if m.done {
		return "done"
	}
	return ""
}
