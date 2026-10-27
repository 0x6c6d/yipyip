#!/usr/bin/env bash
# Verification for T401 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"

probe_src t401 <<'GO'
package tui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
)

// zzvQuit reports whether the model quit: its context is cancelled, or the
// command yields tea.QuitMsg within 200 ms.
func zzvQuit(m Model, cmd tea.Cmd) bool {
	if m.ctx.Err() != nil {
		return true
	}
	if cmd == nil {
		return false
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	select {
	case msg := <-got:
		_, ok := msg.(tea.QuitMsg)
		return ok
	case <-time.After(200 * time.Millisecond):
		return false
	}
}

func TestZZVerifyT401(t *testing.T) {
	for _, how := range []string{"ctrl+w", "click"} {
		m, sess := newSessionModel(t, answerServer(t, "old answer"))
		m = tabTurn(t, m, "only question")
		var cmd tea.Cmd
		if how == "ctrl+w" {
			next, c := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
			m, cmd = next.(Model), c
		} else {
			m, cmd = clickBar(m, zoneCol(t, m, tabZoneClose, 0))
		}
		if zzvQuit(m, cmd) {
			t.Fatalf("%s on the last tab quit", how)
		}
		if len(m.tabs) != 1 || m.sess == nil || m.sess == sess || len(m.blocks) != 0 {
			t.Errorf("%s: tabs %d, new session %v, blocks %d", how, len(m.tabs), m.sess != sess, len(m.blocks))
		}
		if files := sessionFiles(t, sess.StartDir()); len(files) != 0 {
			t.Errorf("%s: the discarded tab was saved: %v", how, files)
		}
	}

	m, sess := newSessionModel(t, answerServer(t, "tab zero answer"))
	m = tabTurn(t, m, "tab zero question")
	mustAddTab(t, &m)
	m.switchTab(0)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	m = next.(Model)
	if zzvQuit(m, cmd) || len(m.tabs) != 1 {
		t.Errorf("closing one of two tabs: %d tabs left", len(m.tabs))
	}
	if files := sessionFiles(t, sess.StartDir()); len(files) != 1 {
		t.Errorf("closing one of two tabs saved %d files, want 1", len(files))
	}

	m2, _ := newSessionModel(t, answerServer(t, "x"))
	_, qcmd := m2.Command("/exit")
	if qcmd == nil {
		t.Fatal("/exit returned no command")
	}
	if _, ok := qcmd().(tea.QuitMsg); !ok {
		t.Error("/exit no longer quits")
	}
}

func TestZZVerifyT401Neg(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "a"))
	m = tabTurn(t, m, "q")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	m = next.(Model)
	if zzvQuit(m, cmd) || len(sessionFiles(t, sess.StartDir())) != 0 {
		t.Log("closing the last tab quit or saved it")
		return
	}
	t.Fatal("REJECTED: closing the last tab neither quits nor saves")
}

func TestZZVerifyT401NegErr(t *testing.T) {
	m, sess := newSessionModelOpts(t, answerServer(t, "a"), "start", func(o *Options) {
		o.NewSession = func() (*agent.Session, error) { return nil, errors.New("boom") }
	})
	m = tabTurn(t, m, "q")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	m = next.(Model)
	if zzvQuit(m, cmd) || m.sess != sess || len(m.tabs) != 1 {
		t.Log("a failing NewSession on the last-tab close quit or changed the tab")
		return
	}
	t.Fatal("REJECTED: a failing NewSession keeps the tab and the app running")
}
GO

expect_ok "T401 tests" <<'CMD'
go_tests ./internal/tui TestCloseLastTabCtrlW TestCloseLastTabClick TestCloseLastTabBusy TestCloseLastTabTwoTabs TestCloseLastTabNewSessionError TestCloseLastTabExitStillQuits
CMD
expect_ok "old quit tests are gone" <<'CMD'
! grep -rn -e 'func TestCloseLastTabQuitsAndSaves' -e 'func TestTabButtonsLastTab' internal/tui
CMD
expect_ok "phase 4 exit: -run 'CloseLastTab|CloseTab|TabButtons|ExitSave'" <<'CMD'
go test ./internal/tui -run 'CloseLastTab|CloseTab|TabButtons|ExitSave' -count=1 -v
CMD
expect_ok "probe: Ctrl+W and × on the last tab, two tabs, /exit" <<'CMD'
go_probe internal/tui t401 TestZZVerifyT401
CMD
expect_fail "negative: closing the last tab quits or saves" <<'CMD'
go_probe_rejects internal/tui t401 TestZZVerifyT401Neg
CMD
expect_fail "negative: a failing NewSession on the last-tab close quits or changes the tab" <<'CMD'
go_probe_rejects internal/tui t401 TestZZVerifyT401NegErr
CMD
expect_ok "security files and go.mod unchanged" <<'CMD'
v9_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
