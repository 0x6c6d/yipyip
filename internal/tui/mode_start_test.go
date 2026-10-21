package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

// newAutoSessionModel builds the model the way cmd/yip does: the session is
// switched to auto mode and the model is created with Mode "auto".
func newAutoSessionModel(t *testing.T, srv *providertest.Server) (Model, *agent.Session) {
	t.Helper()
	_, sess := newSessionModel(t, srv)
	sess.SetMode(agent.ModeAuto)
	m := New(Options{StartDir: sess.StartDir(), Session: sess, Mode: string(agent.ModeAuto)})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return next.(Model), sess
}

func TestModeStartAutoLikeCmd(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	m, _ := newAutoSessionModel(t, srv)
	if _, plain := modeRow1(m); !strings.Contains(plain, "auto (shift+tab to toggle)") {
		t.Errorf("row 1 = %q, want auto mode", plain)
	}
}

func TestModeAutoBashUnprompted(t *testing.T) {
	srv := providertest.New(bashCall("c1", "echo hi"), providertest.Response{Text: "done"})
	defer srv.Close()
	m, sess := newAutoSessionModel(t, srv)
	m, _ = m.Command("run it")
	waitSession(t, sess)
	m = m.Drain()
	v := m.View()
	if strings.Contains(v, "y / n / a") || len(m.pending) != 0 {
		t.Errorf("approval prompt shown in auto mode:\n%s", v)
	}
	if !strings.Contains(v, "hi") || !strings.Contains(v, "done") {
		t.Errorf("view lacks the tool result or the reply:\n%s", v)
	}
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	ran := false
	for _, msg := range reqs[1].Messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "hi") && !strings.Contains(msg.Content, "denied") {
			ran = true
		}
	}
	if !ran {
		t.Errorf("tool result hi not sent back: %+v", reqs[1].Messages)
	}
}

func TestModeManualPromptsAgain(t *testing.T) {
	srv := providertest.New(bashCall("c1", "touch marker.txt"), providertest.Response{Text: "done"})
	defer srv.Close()
	m, sess := newAutoSessionModel(t, srv)
	m, _ = m.Command("/mode manual")
	if sess.Mode() != agent.ModeManual {
		t.Fatalf("mode = %q after /mode manual", sess.Mode())
	}
	m, _ = m.Command("touch it")
	m = waitPrompt(t, m)
	if fileExists(filepath.Join(sess.StartDir(), "marker.txt")) {
		t.Fatal("command ran before the answer")
	}
	m = keyRunes(m, "n")
	waitSession(t, sess)
	m = m.Drain()
	if fileExists(filepath.Join(sess.StartDir(), "marker.txt")) {
		t.Fatal("denied command ran")
	}
	if strings.Contains(m.View(), "y / n / a") {
		t.Error("prompt still shown after the answer")
	}
}
