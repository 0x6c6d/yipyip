package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

func TestStatusLineFields(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	line := m.StatusLine()
	for _, want := range []string{sess.StartDir(), "fake/m1", "medium", "manual", " · "} {
		if !strings.Contains(line, want) {
			t.Errorf("status line %q lacks %q", line, want)
		}
	}
	row1, row2 := m.StatusRows()
	if v := m.View(); !strings.Contains(v, row1) || !strings.Contains(v, row2) {
		t.Errorf("view does not show the status rows %q / %q", row1, row2)
	}
}

func TestStatusTokensAfterUsage(t *testing.T) {
	srv := providertest.New(providertest.Response{
		Text:  "ok",
		Usage: &providertest.Usage{Prompt: 100, Completion: 20},
	})
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	if !strings.Contains(m.StatusLine(), "in 0 · out 0 · total 0") {
		t.Errorf("initial status line = %q", m.StatusLine())
	}
	m, _ = m.Command("hi")
	waitSession(t, sess)
	m = m.Drain()
	if got := m.StatusLine(); !strings.Contains(got, "in 100 · out 20 · total 120") {
		t.Errorf("status line = %q", got)
	}
}

func TestStatusContextSize(t *testing.T) {
	srv := providertest.New(providertest.Response{
		Text:  "ok",
		Usage: &providertest.Usage{Prompt: 100, Completion: 20},
	})
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	m, _ = m.Command("hi")
	waitSession(t, sess)
	m = m.Drain()
	if got := m.StatusLine(); !strings.Contains(got, "ctx 100/8000") {
		t.Errorf("status line = %q", got)
	}
	// Without a model there is no limit and the "/limit" part is omitted.
	m2, _ := newSessionModel(t, nil)
	if _, row2 := m2.StatusRows(); !strings.HasSuffix(row2, "ctx 0") {
		t.Errorf("status row 2 without model = %q", row2)
	}
}

func TestStatusModeShown(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	sess.SetMode(agent.ModeAuto)
	line := m.StatusLine()
	if !strings.Contains(line, "auto") || strings.Contains(line, "manual") {
		t.Errorf("status line = %q", line)
	}
}

func TestStatusNoCost(t *testing.T) {
	srv := providertest.New(providertest.Response{
		Text:  "ok",
		Usage: &providertest.Usage{Prompt: 100, Completion: 20},
	})
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	m, _ = m.Command("hi")
	waitSession(t, sess)
	m = m.Drain()
	for name, s := range map[string]string{
		"view":   m.View(),
		"status": m.StatusLine(),
		"help":   HelpText(),
	} {
		// The temp start directory is named after this test; ignore it.
		low := strings.ToLower(strings.ReplaceAll(s, sess.StartDir(), ""))
		for _, bad := range []string{"cost", "price", "$"} {
			if strings.Contains(low, bad) {
				t.Errorf("%s contains %q:\n%s", name, bad, s)
			}
		}
	}
}

func TestStatusNarrow(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	m, _ := newSessionModel(t, srv)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	m = next.(Model)
	if !strings.Contains(m.View(), "in 0 · out 0 · total 0") {
		t.Errorf("narrow view lacks the token text:\n%s", m.View())
	}
	line := m.StatusLine()
	if !strings.Contains(line, "…") {
		t.Errorf("cwd not shortened: %q", line)
	}
	for _, want := range []string{"fake/m1", "medium", "manual", "in 0 · out 0 · total 0", "ctx 0/8000"} {
		if !strings.Contains(line, want) {
			t.Errorf("narrow status line %q lacks %q", line, want)
		}
	}
}

func TestStatusTwoRows(t *testing.T) {
	srv := providertest.New(providertest.Response{
		Text:  "ok",
		Usage: &providertest.Usage{Prompt: 100, Completion: 20},
	})
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	m, _ = m.Command("hi")
	waitSession(t, sess)
	m = m.Drain()
	row1, row2 := m.StatusRows()
	for _, want := range []string{sess.StartDir(), "fake/m1", "medium", "manual", "(shift+tab to toggle)"} {
		if !strings.Contains(row1, want) {
			t.Errorf("row 1 %q lacks %q", row1, want)
		}
	}
	for _, bad := range []string{"in ", "out ", "ctx"} {
		if strings.Contains(row1, bad) {
			t.Errorf("row 1 %q contains %q", row1, bad)
		}
	}
	for _, want := range []string{"in 100", "out 20", "total 120", "ctx"} {
		if !strings.Contains(row2, want) {
			t.Errorf("row 2 %q lacks %q", row2, want)
		}
	}
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if len(lines) < 2 {
		t.Fatalf("view too short:\n%s", m.View())
	}
	got1, got2 := strings.TrimRight(lines[len(lines)-2], " "), strings.TrimRight(lines[len(lines)-1], " ")
	if got1 != ansi.Strip(row1) || got2 != ansi.Strip(row2) {
		t.Errorf("view does not end with the two rows:\n%q\n%q", got1, got2)
	}
	if n := len(strings.Split(m.View(), "\n")); n != m.height {
		t.Errorf("view height = %d, want %d", n, m.height)
	}
}

func TestStatusRow2Color(t *testing.T) {
	withColorProfile(t, termenv.ANSI256)
	m := New(Options{StartDir: "/work/dir"})
	_, row2 := m.StatusRows()
	if !strings.Contains(row2, "38;5;244") {
		t.Errorf("row 2 %q lacks colour 244", row2)
	}
}

func TestStatusNarrowTwoRows(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	m, _ := newSessionModel(t, srv)
	long := "/" + strings.Repeat("deep/", 20) + "start"
	m.opts.Session = nil
	m.opts.StartDir, m.opts.ModelID, m.opts.Effort, m.opts.Mode = long, "fake/m1", "medium", "manual"
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m = next.(Model)
	row1, _ := m.StatusRows()
	if !strings.HasPrefix(row1, "…") {
		t.Errorf("cwd not shortened: %q", row1)
	}
	for _, want := range []string{"fake/m1", "medium", "manual", "(shift+tab to toggle)"} {
		if !strings.Contains(row1, want) {
			t.Errorf("row 1 %q lacks %q", row1, want)
		}
	}
	if w := lipgloss.Width(row1); w > 60 {
		t.Errorf("row 1 width = %d, want <= 60", w)
	}
}

func TestStatusNoCostTwoRows(t *testing.T) {
	srv := providertest.New(providertest.Response{
		Text:  "ok",
		Usage: &providertest.Usage{Prompt: 100, Completion: 20},
	})
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	m, _ = m.Command("hi")
	waitSession(t, sess)
	m = m.Drain()
	row1, row2 := m.StatusRows()
	for name, s := range map[string]string{"row 1": row1, "row 2": row2} {
		// The temp start directory is named after this test; ignore it.
		low := strings.ToLower(strings.ReplaceAll(s, sess.StartDir(), ""))
		for _, bad := range []string{"cost", "price", "$"} {
			if strings.Contains(low, bad) {
				t.Errorf("%s contains %q: %q", name, bad, s)
			}
		}
	}
}
