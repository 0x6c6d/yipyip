package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"

	"yipyip/internal/provider/providertest"
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

func TestInputSelect(t *testing.T) {
	m, clip, _ := newCopyModel(t, "alpha beta")
	m.input.SetValue("hello world")
	x, y := findCell(t, m, "hello world")
	m, cmd := drag(m, x, y, x+len("hello world")-1, y)
	if len(clip.texts) != 1 || clip.texts[0] != "hello world" {
		t.Fatalf("clipboard got %q, want [\"hello world\"]", clip.texts)
	}
	if cmd == nil {
		t.Fatal("release returned no notice tick command")
	}
	if !strings.Contains(plainView(m), "11 characters copied") {
		t.Fatalf("no notice in view:\n%s", plainView(m))
	}
	if m.input.Value() != "hello world" {
		t.Fatalf("drag changed the input to %q", m.input.Value())
	}

	// The second row of a 2-line input only, dragged past its end.
	m.input.SetValue("first row\nsecond row")
	top := tabBarRows + m.viewport.Height + 1
	m, _ = drag(m, 1, top+1, 1+40, top+1)
	if len(clip.texts) != 2 || clip.texts[1] != "second row" {
		t.Fatalf("second row drag got %q", clip.texts)
	}

	// A press and release at the same cell copies nothing.
	m = upd(m, leftMouse(3, top, tea.MouseActionPress))
	nm, cmd := m.Update(leftMouse(3, top, tea.MouseActionRelease))
	m = nm.(Model)
	if len(clip.texts) != 2 || cmd != nil {
		t.Fatalf("a click copied %q", clip.texts)
	}
	if m.inSel.active {
		t.Fatal("input selection not cleared after release")
	}
}

func TestInputSelectHighlight(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	m, clip, _ := newCopyModel(t)
	m.input.SetValue("hello world")
	x, y := findCell(t, m, "hello world")
	m = upd(m, leftMouse(x, y, tea.MouseActionPress))
	m = upd(m, leftMouse(x+4, y, tea.MouseActionMotion))
	rows := strings.Split(m.View(), "\n")
	if !strings.Contains(rows[y], "\x1b[7mhello") {
		t.Fatalf("no reverse video on the input row while dragging:\n%q", rows[y])
	}
	if !strings.Contains(plainView(m), "hello world") {
		t.Fatalf("highlight changed the text:\n%s", plainView(m))
	}
	m = upd(m, leftMouse(x+4, y, tea.MouseActionRelease))
	// The textarea cursor is reverse video too; the selection is not.
	if rows := strings.Split(m.View(), "\n"); strings.Contains(rows[y], "\x1b[7mhello") {
		t.Fatalf("reverse video still shown after release:\n%q", rows[y])
	}
	if len(clip.texts) != 1 || clip.texts[0] != "hello" {
		t.Fatalf("clipboard got %q", clip.texts)
	}
}

func TestInputSelectSecretNeverCopied(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	m, _ := newSessionModel(t, srv)
	m = upd(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	clip := &fakeClip{}
	m.clip = clip.write
	m.input.SetValue("key SECRET123 end")

	x, y := findCell(t, m, "key ")
	m, _ = drag(m, x, y, 1000, y)
	if len(clip.texts) != 1 {
		t.Fatalf("clipboard got %q", clip.texts)
	}
	got := clip.texts[0]
	if strings.Contains(got, "SECRET123") {
		t.Fatalf("the secret reached the clipboard: %q", got)
	}
	if got != "key [redacted] end" {
		t.Fatalf("unexpected copied text %q", got)
	}
	if !strings.Contains(plainView(m), "18 characters copied") {
		t.Fatalf("notice does not count the redacted text:\n%s", plainView(m))
	}
}

// rUpd is Update followed by View, as the Bubble Tea runtime renders after
// every message.
func rUpd(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	m = next.(Model)
	_ = m.View()
	return m
}

// rModel builds a model with a session at w×h and types value into it
// (alt+enter for newlines), rendering after every message.
func rModel(t *testing.T, w, h int, value string) Model {
	t.Helper()
	srv := providertest.New()
	t.Cleanup(srv.Close)
	m, _ := newSessionModel(t, srv)
	m = rUpd(m, tea.WindowSizeMsg{Width: w, Height: h})
	for _, r := range value {
		if r == '\n' {
			m = rUpd(m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
			continue
		}
		m = rUpd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.input.Value() != value {
		t.Fatalf("typed %q, input holds %q", value, m.input.Value())
	}
	return m
}

// rAt returns the screen cell of the first rune of tok in the rendered view.
func rAt(t *testing.T, m Model, tok string) (int, int) {
	t.Helper()
	for y, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if i := strings.Index(l, tok); i >= 0 {
			return runewidth.StringWidth(l[:i]), y
		}
	}
	t.Fatalf("token %q not on screen:\n%s", tok, ansi.Strip(m.View()))
	return 0, 0
}

// rClick presses and releases at (x, y), then delivers a cursor blink and a
// same-size resize like the runtime does between clicks.
func rClick(m Model, x, y int) Model {
	m = rUpd(m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = rUpd(m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	m = rUpd(m, textarea.Blink())
	return rUpd(m, tea.WindowSizeMsg{Width: m.width, Height: m.height})
}

func rCur(m Model) (int, int) {
	li := m.input.LineInfo()
	return m.input.Line(), li.StartColumn + li.ColumnOffset
}

// rWant checks the cursor and that a typed '#' lands right there.
func rWant(t *testing.T, m Model, line, col int) {
	t.Helper()
	if l, c := rCur(m); l != line || c != col {
		t.Fatalf("cursor line %d col %d, want line %d col %d\n%s", l, c, line, col, ansi.Strip(m.View()))
	}
	before := strings.Split(m.input.Value(), "\n")
	m = rUpd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'#'}})
	after := strings.Split(m.input.Value(), "\n")
	r := []rune(before[line])
	want := string(r[:col]) + "#" + string(r[col:])
	if len(after) != len(before) || after[line] != want {
		t.Fatalf("typed '#' gave %q, want line %d = %q", after, line, want)
	}
}

func TestInputClickMultiLine(t *testing.T) {
	m := rModel(t, 80, 24, "alpha\nbeQta\ngamma")
	x, y := rAt(t, m, "Qta")
	rWant(t, rClick(m, x, y), 1, 2)
	x, y = rAt(t, m, "alpha")
	rWant(t, rClick(m, x+1, y), 0, 1)
	x, y = rAt(t, m, "gamma")
	rWant(t, rClick(m, x+30, y), 2, 5)
}

func TestInputClickWrapped(t *testing.T) {
	m := rModel(t, 40, 24, strings.Repeat("a", 50)+"Qbc")
	x, y := rAt(t, m, "Qbc")
	rWant(t, rClick(m, x, y), 0, 50)
}

func TestInputClickScrolled(t *testing.T) {
	m := rModel(t, 80, 24, "r0\nr1\nr2\nr3\nr4 Q4\nr5\nr6")
	x, y := rAt(t, m, "Q4")
	rWant(t, rClick(m, x, y), 4, 3)
	x, y = rAt(t, m, "r5")
	rWant(t, rClick(m, x+1, y), 5, 1)
}

func TestInputClickWideRunes(t *testing.T) {
	m := rModel(t, 80, 24, "x\n日本語Qz")
	x, y := rAt(t, m, "Qz")
	rWant(t, rClick(m, x, y), 1, 3)
	x, y = rAt(t, m, "本")
	rWant(t, rClick(m, x+1, y), 1, 1)
	rWant(t, rClick(m, x, y), 1, 1)
}

func TestInputClickOutside(t *testing.T) {
	m := rModel(t, 80, 24, "alpha\nbeQta\ngamma")
	m.add(blockInfo, "some conversation text")
	m = rUpd(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	x, y := rAt(t, m, "Qta")
	m = rClick(m, x, y)
	for _, yy := range []int{0, 1, 2, y - 4, 22, 23} {
		for _, xx := range []int{0, 3, 10} {
			m = rClick(m, xx, yy)
			if l, c := rCur(m); l != 1 || c != 2 {
				t.Fatalf("click at %d,%d moved the cursor to %d,%d", xx, yy, l, c)
			}
		}
	}
}

// TestInputClickScrolledEqualRows: with repeated rows the scroll offset cannot
// be told from the rendered text; each click must still hit its own row.
func TestInputClickScrolledEqualRows(t *testing.T) {
	m := rModel(t, 80, 24, "a\na\na\na\na\na\na")
	top := tabBarRows + m.viewport.Height + 1
	for _, line := range []int{4, 5, 6, 4, 6} {
		rWant(t, rClick(m, 1, top+line-4), line, 0)
	}
}
