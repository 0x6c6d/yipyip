package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
	if !strings.Contains(m.View(), line) {
		t.Errorf("view does not show the status line %q", line)
	}
}

func TestStatusTokensAfterUsage(t *testing.T) {
	srv := providertest.New(providertest.Response{
		Text:  "ok",
		Usage: &providertest.Usage{Prompt: 100, Completion: 20},
	})
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	if !strings.Contains(m.StatusLine(), "in 0 / out 0 / total 0") {
		t.Errorf("initial status line = %q", m.StatusLine())
	}
	m, _ = m.Command("hi")
	waitSession(t, sess)
	m = m.Drain()
	if got := m.StatusLine(); !strings.Contains(got, "in 100 / out 20 / total 120") {
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
	if got := m2.StatusLine(); !strings.HasSuffix(got, "ctx 0") {
		t.Errorf("status line without model = %q", got)
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
	if !strings.Contains(m.View(), "in 0 / out 0 / total 0") {
		t.Errorf("narrow view lacks the token text:\n%s", m.View())
	}
	line := m.StatusLine()
	if !strings.Contains(line, "…") {
		t.Errorf("cwd not shortened: %q", line)
	}
	for _, want := range []string{"fake/m1", "medium", "manual", "in 0 / out 0 / total 0", "ctx 0/8000"} {
		if !strings.Contains(line, want) {
			t.Errorf("narrow status line %q lacks %q", line, want)
		}
	}
}
