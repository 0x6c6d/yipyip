package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

func bashCall(id, cmd string) providertest.Response {
	return providertest.Response{ToolCalls: []providertest.ToolCall{
		{ID: id, Name: "bash", Arguments: `{"command":"` + cmd + `"}`},
	}}
}

// waitPrompt drains events until the approval prompt is visible.
func waitPrompt(t *testing.T, m Model) Model {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		m = m.Drain()
		if strings.Contains(m.View(), "y / n / a") {
			return m
		}
		if time.Now().After(deadline) {
			t.Fatalf("approval prompt never shown:\n%s", m.View())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func keyRunes(m Model, s string) Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return next.(Model)
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestApprovalPromptShown(t *testing.T) {
	srv := providertest.New(bashCall("c1", "echo hi"), providertest.Response{Text: "done"})
	defer srv.Close()
	m, _ := newSessionModel(t, srv)
	m, _ = m.Command("run it")
	m = waitPrompt(t, m)
	v := m.View()
	for _, want := range []string{"[main] bash", "echo hi", "y / n / a"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
}

func TestApprovalDeny(t *testing.T) {
	srv := providertest.New(bashCall("c1", "touch marker"), providertest.Response{Text: "done"})
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	m, _ = m.Command("touch it")
	m = waitPrompt(t, m)
	m = keyRunes(m, "n")
	waitSession(t, sess)
	m = m.Drain()
	if fileExists(filepath.Join(sess.StartDir(), "marker")) {
		t.Fatal("denied command ran")
	}
	if strings.Contains(m.View(), "y / n / a") {
		t.Error("prompt still shown after the answer")
	}
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	denied := false
	for _, msg := range reqs[1].Messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "denied by user") {
			denied = true
		}
	}
	if !denied {
		t.Errorf("model was not told 'denied by user': %+v", reqs[1].Messages)
	}
}

func TestApprovalYes(t *testing.T) {
	srv := providertest.New(bashCall("c1", "touch marker"), providertest.Response{Text: "done"})
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	m, _ = m.Command("touch it")
	m = waitPrompt(t, m)
	if fileExists(filepath.Join(sess.StartDir(), "marker")) {
		t.Fatal("command ran before the answer")
	}
	m = keyRunes(m, "y")
	waitSession(t, sess)
	m = m.Drain()
	if !fileExists(filepath.Join(sess.StartDir(), "marker")) {
		t.Fatal("approved command did not run")
	}
	if strings.Contains(m.View(), "y / n / a") {
		t.Error("prompt still shown after the answer")
	}
}

func TestApprovalAlways(t *testing.T) {
	srv := providertest.New(
		bashCall("c1", "touch one"), providertest.Response{Text: "first done"},
		bashCall("c2", "touch two"), providertest.Response{Text: "second done"},
	)
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	m, _ = m.Command("first")
	m = waitPrompt(t, m)
	m = keyRunes(m, "a")
	waitSession(t, sess)
	m = m.Drain()
	if !fileExists(filepath.Join(sess.StartDir(), "one")) {
		t.Fatal("first command did not run")
	}
	m, _ = m.Command("second")
	waitSession(t, sess)
	m = m.Drain()
	if !fileExists(filepath.Join(sess.StartDir(), "two")) {
		t.Fatal("second command needed another approval")
	}
	if strings.Contains(m.View(), "y / n / a") {
		t.Error("prompt shown for the second call")
	}
}

func TestApprovalAutoMode(t *testing.T) {
	srv := providertest.New(bashCall("c1", "touch marker"), providertest.Response{Text: "done"})
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	sess.SetMode(agent.ModeAuto)
	m, _ = m.Command("touch it")
	waitSession(t, sess)
	m = m.Drain()
	if !fileExists(filepath.Join(sess.StartDir(), "marker")) {
		t.Fatal("auto mode did not run the command")
	}
	if strings.Contains(m.View(), "y / n / a") {
		t.Error("prompt shown in auto mode")
	}
}

func TestApprovalIgnoresOtherKeys(t *testing.T) {
	srv := providertest.New(bashCall("c1", "touch marker"), providertest.Response{Text: "done"})
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	m, _ = m.Command("touch it")
	m = waitPrompt(t, m)
	m = keyRunes(m, "xzq")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if v := m.input.Value(); v != "" {
		t.Errorf("input = %q, other keys must not reach it", v)
	}
	if !strings.Contains(m.View(), "y / n / a") || len(m.pending) != 1 {
		t.Fatal("other keys must leave the prompt in place")
	}
	if !sess.Busy() {
		t.Fatal("turn must still wait for the answer")
	}
	// Esc denies and cancels the turn.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	waitSession(t, sess)
	m = m.Drain()
	if fileExists(filepath.Join(sess.StartDir(), "marker")) {
		t.Fatal("command ran after Esc")
	}
	if strings.Contains(m.View(), "y / n / a") {
		t.Error("prompt still shown after Esc")
	}
}
