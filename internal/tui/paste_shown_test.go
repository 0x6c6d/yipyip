package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// blocksText joins the text of every transcript block.
func blocksText(m Model) string {
	var parts []string
	for _, b := range m.blocks {
		parts = append(parts, b.text)
	}
	return strings.Join(parts, "\n")
}

func TestPasteShown(t *testing.T) {
	srv := suggestServer(t)
	m, sess := newSessionModel(t, srv)
	m = pasteInto(m, "line one\nline two\nline three")
	if v := plainView(m); !strings.Contains(v, "[Pasted text") {
		t.Fatalf("input does not show the placeholder:\n%s", v)
	}
	m, content := pasteSend(t, m, sess, srv)
	if content != "line one\nline two\nline three" {
		t.Fatalf("request content = %q", content)
	}
	if b := blocksText(m); !strings.Contains(b, "> line one\nline two\nline three") || strings.Contains(b, "[Pasted text") {
		t.Fatalf("transcript blocks = %q", b)
	}
	if v := plainView(m); !strings.Contains(v, "line one") || !strings.Contains(v, "line three") || strings.Contains(v, "[Pasted text") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestPasteShownQueued(t *testing.T) {
	m, sess, srv, release := queueModel(t)
	m = pasteInto(m, "line one\nline two\nline three")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if len(m.queue) != 1 {
		t.Fatalf("queue = %q, want one message", m.queue)
	}
	m = endTurn(t, m, sess, release)
	waitSession(t, sess)
	m.Drain()
	if got := lastUserContent(t, srv.Requests()[1]); got != "line one\nline two\nline three" {
		t.Fatalf("last user message = %q", got)
	}
	if b := blocksText(m); !strings.Contains(b, "> line one\nline two\nline three") || strings.Contains(b, "[Pasted text") {
		t.Fatalf("transcript blocks = %q", b)
	}
}
