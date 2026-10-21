package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

// queueModel starts the held turn "first" and returns the busy model.
func queueModel(t *testing.T) (Model, *agent.Session, *providertest.Server, func()) {
	t.Helper()
	srv, release := heldServer(t)
	m, sess, _ := newClockModel(t, srv)
	m, _ = m.Command("first")
	waitRequests(t, srv, 1)
	return m, sess, srv, release
}

// enter types text into the input and presses Enter.
func enter(m Model, text string) Model {
	m.input.SetValue(text)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return next.(Model)
}

// lineAbove returns the plain line directly above the input box.
func lineAbove(t *testing.T, m Model) string {
	t.Helper()
	lines := strings.Split(plainView(m), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "╭") && i > 0 {
			return lines[i-1]
		}
	}
	t.Fatalf("no input box in view:\n%s", plainView(m))
	return ""
}

// queueBlock returns the plain lines between the Thinking line and the input box.
func queueBlock(t *testing.T, m Model) []string {
	t.Helper()
	lines := strings.Split(plainView(m), "\n")
	start := -1
	for i, l := range lines {
		if strings.Contains(l, "✻ Thinking…") {
			start = i + 1
		}
		if strings.HasPrefix(l, "╭") {
			if start < 0 {
				t.Fatalf("no Thinking line above the input box:\n%s", plainView(m))
			}
			out := make([]string, 0, i-start)
			for _, q := range lines[start:i] {
				out = append(out, strings.TrimRight(q, " "))
			}
			return out
		}
	}
	t.Fatalf("no input box in view:\n%s", plainView(m))
	return nil
}

func lastUserContent(t *testing.T, req providertest.Request) string {
	t.Helper()
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			return req.Messages[i].Content
		}
	}
	t.Fatalf("request has no user message: %+v", req.Messages)
	return ""
}

// endTurn releases the held turn and applies its events.
func endTurn(t *testing.T, m Model, sess *agent.Session, release func()) Model {
	t.Helper()
	release()
	waitSession(t, sess)
	return m.Drain()
}

func TestQueueEnterWhileBusy(t *testing.T) {
	withColorProfile(t, termenv.ANSI256)
	m, _, srv, _ := queueModel(t)
	h := m.View()
	m = enter(m, "second")
	if v := m.input.Value(); v != "" {
		t.Fatalf("input = %q, want empty", v)
	}
	if got := strings.TrimSpace(lineAbove(t, m)); got != "> second" {
		t.Fatalf("line above the input box = %q\n%s", got, plainView(m))
	}
	if a, b := strings.Count(h, "\n"), strings.Count(m.View(), "\n"); a != b {
		t.Fatalf("view height changed: %d -> %d", a, b)
	}
	found := false
	for _, l := range strings.Split(m.View(), "\n") {
		if strings.TrimSpace(ansi.Strip(l)) == "> second" {
			found = true
			if !strings.Contains(l, "38;5;244") {
				t.Errorf("queue line lacks 38;5;244: %q", l)
			}
		}
	}
	if !found {
		t.Fatal("no queue line in view")
	}
	for _, b := range m.blocks {
		if strings.Contains(b.text, "second") {
			t.Fatalf("queued message added to the conversation: %q", b.text)
		}
	}
	for _, r := range srv.Requests() {
		for _, msg := range r.Messages {
			if strings.Contains(msg.Content, "second") {
				t.Fatalf("queued message was sent: %+v", msg)
			}
		}
	}
}

func TestQueueOrderAndOverflow(t *testing.T) {
	m, _, _, _ := queueModel(t)
	m = enter(m, "one")
	m = enter(m, "two")
	if got := queueBlock(t, m); len(got) != 2 || got[0] != "> one" || got[1] != "> two" {
		t.Fatalf("queue block = %q", got)
	}
	for i := 3; i <= 7; i++ {
		m = enter(m, fmt.Sprintf("msg %d", i))
	}
	want := []string{"> one", "> two", "> msg 3", "> msg 4", "> msg 5", "… +2 more"}
	if got := queueBlock(t, m); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("queue block = %q, want %q", got, want)
	}
	if n := len(strings.Split(plainView(m), "\n")); n != 40 {
		t.Fatalf("view has %d lines, want 40", n)
	}

	m2, _, _, _ := queueModel(t)
	m2 = enter(m2, "a\nb")
	if got := queueBlock(t, m2); len(got) != 1 || got[0] != "> a…" {
		t.Fatalf("queue block = %q, want [> a…]", got)
	}
}

func TestQueueSentAtTurnEnd(t *testing.T) {
	m, sess, srv, release := queueModel(t)
	m = enter(m, "second")
	m = enter(m, "third")
	m = endTurn(t, m, sess, release)
	if len(m.queue) != 0 {
		t.Fatalf("queue = %q, want empty", m.queue)
	}
	if v := plainView(m); !strings.Contains(v, "✻ Thinking…") {
		t.Fatalf("no Thinking line while the queued turn runs:\n%s", v)
	}
	v := plainView(m)
	mk, i2, i3 := strings.Index(v, "✻ Worked for"), strings.Index(v, "> second"), strings.Index(v, "> third")
	if mk < 0 || i2 < 0 || i3 < 0 || !(mk < i2 && i2 < i3) {
		t.Fatalf("want marker, > second, > third in order (%d %d %d):\n%s", mk, i2, i3, v)
	}
	waitSession(t, sess)
	m = m.Drain()
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	if got := lastUserContent(t, reqs[1]); got != "second\nthird" {
		t.Fatalf("last user message = %q, want %q", got, "second\nthird")
	}
	if n := len(markerLines(m)); n != 2 {
		t.Fatalf("%d marker lines, want 2:\n%s", n, plainView(m))
	}
	if v := plainView(m); strings.Contains(v, "Thinking…") {
		t.Fatalf("Thinking line after the queued turn:\n%s", v)
	}
}

func TestQueueSlashRunsImmediately(t *testing.T) {
	m, sess, _, _ := queueModel(t)
	if sess.Mode() != agent.ModeManual {
		t.Fatalf("mode = %q, want manual", sess.Mode())
	}
	m = enter(m, "/mode auto")
	if sess.Mode() != agent.ModeAuto {
		t.Fatalf("mode = %q, want auto", sess.Mode())
	}
	if len(m.queue) != 0 {
		t.Fatalf("slash command was queued: %q", m.queue)
	}
	if v := plainView(m); !strings.Contains(v, "mode: auto") {
		t.Fatalf("no info line:\n%s", v)
	}
}

func TestQueueInHistory(t *testing.T) {
	m, sess, _, release := queueModel(t)
	m = enter(m, "second")
	m = endTurn(t, m, sess, release)
	if len(m.queue) != 0 {
		t.Fatalf("queue = %q, want empty", m.queue)
	}
	if _, got := histKey(m, tea.KeyUp); got != "second" {
		t.Fatalf("Up: input = %q, want %q", got, "second")
	}
}

func TestQueuePasteExpanded(t *testing.T) {
	m, sess, srv, release := queueModel(t)
	m = pasteInto(m, "alpha\nbeta\ngamma")
	if v := m.input.Value(); v != "[Pasted text #1 +3 lines]" {
		t.Fatalf("input = %q", v)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if len(m.queue) != 1 {
		t.Fatalf("queue = %q, want one message", m.queue)
	}
	if v := plainView(m); !strings.Contains(v, "> [Pasted text #1 +3 lines]") || strings.Contains(v, "gamma") {
		t.Fatalf("view:\n%s", v)
	}
	m = endTurn(t, m, sess, release)
	waitSession(t, sess)
	m.Drain()
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	if got := lastUserContent(t, reqs[1]); got != "alpha\nbeta\ngamma" {
		t.Fatalf("last user message = %q", got)
	}
}

func TestQueueNoBusyLine(t *testing.T) {
	m, sess, _, release := queueModel(t)
	m = enter(m, "second")
	m = enter(m, "third")
	if v := plainView(m); strings.Contains(v, "busy: press Esc") {
		t.Fatalf("queueing showed the busy line:\n%s", v)
	}
	m = endTurn(t, m, sess, release)
	waitSession(t, sess)
	m = m.Drain()
	if v := plainView(m); strings.Contains(v, "busy: press Esc") {
		t.Fatalf("sending the queue showed the busy line:\n%s", v)
	}
	for _, b := range m.blocks {
		if strings.Contains(b.text, "busy: press Esc") {
			t.Fatalf("busy block %q", b.text)
		}
	}
}
