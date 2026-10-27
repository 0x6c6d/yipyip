package e2e

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/provider/providertest"
	"yipyip/internal/tui"
)

const markdownAnswer = "Here is the table:\n\n| Name | Key |\n|------|:---:|\n| alpha | SECRET123 |\n| beta | **bold** |\n\nDone."

// TestMarkdownE2E checks that an assistant table is rendered with borders,
// redacted and without markdown syntax.
func TestMarkdownE2E(t *testing.T) {
	e := newEnv(t, providertest.Response{Text: markdownAnswer})
	e.submit(t, "show a table")
	view := ansi.Strip(e.Model.View())
	for _, want := range []string{"┌", "├", "└", "│ alpha", "[redacted]", "bold", "Here is the table:", "Done."} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	for _, bad := range []string{"SECRET123", "**bold**"} {
		if strings.Contains(view, bad) {
			t.Errorf("view contains %q", bad)
		}
	}
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, "|---") || strings.Contains(l, "|:---:|") {
			t.Errorf("separator line shown: %q", l)
		}
	}
}

// TestMarkdownE2ECopy selects the rendered table with the mouse and checks
// the copied text is the rendered, redacted view text.
func TestMarkdownE2ECopy(t *testing.T) {
	var copied []string
	e := newEnvServer(t, providertest.New(providertest.Response{Text: markdownAnswer}), envOpts{
		contextLimit: 8000,
		clipboard:    func(s string) error { copied = append(copied, s); return nil },
	})
	e.submit(t, "show a table")
	ax, ay, by := -1, -1, -1
	for y, l := range strings.Split(ansi.Strip(e.Model.View()), "\n") {
		if i := strings.Index(l, "alpha"); i >= 0 && ay < 0 {
			ax, ay = ansi.StringWidth(l[:i]), y
		}
		if strings.Contains(l, "beta") && by < 0 {
			by = y
		}
	}
	if ay < 0 || by < 0 {
		t.Fatal("alpha or beta not on screen")
	}
	for _, msg := range []tea.MouseMsg{
		{X: ax, Y: ay, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
		{X: 150, Y: by, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion},
		{X: 150, Y: by, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease},
	} {
		next, _ := e.Model.Update(msg)
		e.Model = next.(tui.Model)
	}
	if len(copied) != 1 {
		t.Fatalf("copied %d texts, want 1: %q", len(copied), copied)
	}
	c := copied[0]
	for _, want := range []string{"alpha", "[redacted]", "│", "beta", "bold"} {
		if !strings.Contains(c, want) {
			t.Errorf("copy lacks %q: %q", want, c)
		}
	}
	for _, bad := range []string{"SECRET123", "**", "|---"} {
		if strings.Contains(c, bad) {
			t.Errorf("copy contains %q: %q", bad, c)
		}
	}
}
