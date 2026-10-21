package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"yipyip/internal/provider/providertest"
)

// fakeClip records the texts handed to the clipboard writer.
type fakeClip struct{ texts []string }

func (f *fakeClip) write(s string) error {
	f.texts = append(f.texts, s)
	return nil
}

// newCopyModel returns an 80x24 model without a session, with a fake
// clipboard and a fake clock, holding one info block per text.
func newCopyModel(t *testing.T, texts ...string) (Model, *fakeClip, *fakeClock) {
	t.Helper()
	clip := &fakeClip{}
	clock := &fakeClock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	m := upd(New(Options{StartDir: "/x", Clipboard: clip.write, Now: clock.Now}),
		tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, s := range texts {
		m.add(blockInfo, s)
	}
	return m, clip, clock
}

// findCell returns the screen cell (display column, row) where sub starts in
// the plain view.
func findCell(t *testing.T, m Model, sub string) (int, int) {
	t.Helper()
	for y, l := range strings.Split(plainView(m), "\n") {
		if i := strings.Index(l, sub); i >= 0 {
			return ansi.StringWidth(l[:i]), y
		}
	}
	t.Fatalf("%q not in view:\n%s", sub, plainView(m))
	return 0, 0
}

func leftMouse(x, y int, a tea.MouseAction) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: a}
}

// drag presses at (x0, y0), moves to and releases at (x1, y1). It returns the
// model after the release and the command of the release.
func drag(m Model, x0, y0, x1, y1 int) (Model, tea.Cmd) {
	m = upd(m, leftMouse(x0, y0, tea.MouseActionPress))
	m = upd(m, leftMouse(x1, y1, tea.MouseActionMotion))
	nm, cmd := m.Update(leftMouse(x1, y1, tea.MouseActionRelease))
	return nm.(Model), cmd
}

func TestCopySelection(t *testing.T) {
	m, clip, clock := newCopyModel(t, "alpha beta", "gamma delta")
	x0, y0 := findCell(t, m, "beta")
	x1, y1 := findCell(t, m, "gamma")
	if y1 != y0+2 {
		t.Fatalf("blocks not one blank line apart: rows %d and %d", y0, y1)
	}
	m, cmd := drag(m, x0, y0, x1+4, y1)
	if len(clip.texts) != 1 || clip.texts[0] != "beta\n\ngamma" {
		t.Fatalf("clipboard got %q, want [\"beta\\n\\ngamma\"]", clip.texts)
	}
	if cmd == nil {
		t.Fatal("release returned no notice tick command")
	}
	if m.sel.active {
		t.Fatal("selection not cleared after release")
	}

	// Backwards, once the notice (which takes the top line) has expired: the
	// same stream from end to start.
	clock.Advance(3 * time.Second)
	m, _ = drag(m, x1+4, y1, x0, y0)
	if len(clip.texts) != 2 || clip.texts[1] != "beta\n\ngamma" {
		t.Fatalf("backwards drag got %q", clip.texts)
	}
}

func TestCopyTrailingSpacesTrimmed(t *testing.T) {
	m, clip, _ := newCopyModel(t, "short", "longer line here")
	_, y0 := findCell(t, m, "short")
	_, y1 := findCell(t, m, "longer")
	// From the first column to beyond the right edge (clamped).
	drag(m, 0, y0, 200, y1)
	if len(clip.texts) != 1 {
		t.Fatalf("clipboard got %q", clip.texts)
	}
	got := clip.texts[0]
	for _, l := range strings.Split(got, "\n") {
		if strings.HasSuffix(l, " ") {
			t.Fatalf("line %q has trailing spaces (text %q)", l, got)
		}
	}
	if !strings.Contains(got, "short") || !strings.HasSuffix(got, "longer line here") {
		t.Fatalf("unexpected text %q", got)
	}
}

func TestCopyHighlightReverse(t *testing.T) {
	m, clip, _ := newCopyModel(t, "alpha beta", "gamma delta")
	x0, y0 := findCell(t, m, "beta")
	x1, y1 := findCell(t, m, "delta")
	m = upd(m, leftMouse(x0, y0, tea.MouseActionPress))
	m = upd(m, leftMouse(x1, y1, tea.MouseActionMotion))
	v := m.View()
	if !strings.Contains(v, "\x1b[7m") {
		t.Fatalf("no reverse video while dragging:\n%q", v)
	}
	if !strings.Contains(plainView(m), "alpha beta") {
		t.Fatalf("highlight changed the text:\n%s", plainView(m))
	}
	m = upd(m, leftMouse(x1, y1, tea.MouseActionRelease))
	if strings.Contains(m.View(), "\x1b[7m") {
		t.Fatal("reverse video still shown after release")
	}
	if len(clip.texts) != 1 {
		t.Fatalf("clipboard got %q", clip.texts)
	}
}

func TestCopyNotice(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	srv, release := heldServer(t)
	m, sess, clock := newClockModel(t, srv)
	clip := &fakeClip{}
	m.clip = clip.write
	m = finishHeld(t, m, sess, clock, release, 75*time.Second)

	x, y := findCell(t, m, "✻ Worked")
	m, _ = drag(m, x, y, x+len([]rune("✻ Worked"))-1, y)
	if len(clip.texts) != 1 || clip.texts[0] != "✻ Worked" {
		t.Fatalf("clipboard got %q", clip.texts)
	}
	const notice = "8 characters copied"
	lines := strings.Split(plainView(m), "\n")
	at := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == notice {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no notice %q in view:\n%s", notice, plainView(m))
	}
	if at+1 >= len(lines) || !strings.HasPrefix(lines[at+1], "╭") {
		t.Fatalf("notice not directly above the input box:\n%s", plainView(m))
	}
	if !strings.Contains(m.View(), "\x1b[38;2;177;185;249m"+notice) {
		t.Fatalf("notice not rendered in #B1B9F9:\n%q", m.View())
	}

	clock.Advance(3 * time.Second)
	if strings.Contains(plainView(m), notice) {
		t.Fatalf("notice still shown after 3s:\n%s", plainView(m))
	}
}

func TestCopyClickNoDrag(t *testing.T) {
	m, clip, _ := newCopyModel(t, "alpha beta", "gamma delta")
	x, y := findCell(t, m, "beta")
	m = upd(m, leftMouse(x, y, tea.MouseActionPress))
	nm, cmd := m.Update(leftMouse(x, y, tea.MouseActionRelease))
	m = nm.(Model)
	if len(clip.texts) != 0 {
		t.Fatalf("a click copied %q", clip.texts)
	}
	if cmd != nil {
		t.Fatal("a click returned a command")
	}
	if strings.Contains(plainView(m), "characters copied") {
		t.Fatalf("a click showed the notice:\n%s", plainView(m))
	}

	// A press outside the conversation (input box, status rows) does nothing.
	_, by := findCell(t, m, "╭")
	m, _ = drag(m, 1, by+1, 5, 0)
	if len(clip.texts) != 0 || strings.Contains(plainView(m), "characters copied") {
		t.Fatalf("a drag from the input box copied %q", clip.texts)
	}
}

func TestCopySecretNeverCopied(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "the key is SECRET123 ok"})
	defer srv.Close()
	m, sess, _ := newClockModel(t, srv)
	clip := &fakeClip{}
	m.clip = clip.write
	m, _ = m.Command("hi")
	waitSession(t, sess)
	m = m.Drain()

	x, y := findCell(t, m, "the key is")
	m, _ = drag(m, x, y, 1000, y)
	if len(clip.texts) != 1 {
		t.Fatalf("clipboard got %q", clip.texts)
	}
	got := clip.texts[0]
	if strings.Contains(got, "SECRET123") {
		t.Fatalf("the secret reached the clipboard: %q", got)
	}
	if !strings.Contains(got, "[redacted]") || !strings.HasPrefix(got, "the key is") {
		t.Fatalf("unexpected copied text %q", got)
	}
	if strings.Contains(m.View(), "SECRET123") {
		t.Fatal("the secret reached the screen")
	}
}

func TestCopyFinishedMarkerPlain(t *testing.T) {
	srv, release := heldServer(t)
	m, sess, clock := newClockModel(t, srv)
	clip := &fakeClip{}
	m.clip = clip.write
	m = finishHeld(t, m, sess, clock, release, 75*time.Second)

	const marker = "✻ Worked for 1m 15s"
	x, y := findCell(t, m, marker)
	drag(m, x, y, x+ansi.StringWidth(marker)-1, y)
	if len(clip.texts) != 1 || clip.texts[0] != marker {
		t.Fatalf("clipboard got %q, want %q", clip.texts, marker)
	}
	if strings.Contains(clip.texts[0], "\x1b") {
		t.Fatalf("copied text has escape sequences: %q", clip.texts[0])
	}
}
