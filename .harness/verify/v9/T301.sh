#!/usr/bin/env bash
# Verification for T301 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"

probe_src t301 <<'GO'
package tui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

func TestZZVerifyT301(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "old answer"))
	m = tabTurn(t, m, "old question")
	mustAddTab(t, &m)
	m.switchTab(0)
	model, effort, mode := m.globals()
	hist := m.hist
	if err := m.resetTab(true); err != nil {
		t.Fatalf("resetTab(true): %v", err)
	}
	if len(m.tabs) != 2 || m.cur != 0 {
		t.Fatalf("tabs = %d, cur = %d, want 2 and 0", len(m.tabs), m.cur)
	}
	if m.sess == nil || m.sess == sess || m.tabs[0].sess != m.sess {
		t.Fatal("the current tab did not get a new session in place")
	}
	if len(m.blocks) != 0 {
		t.Errorf("blocks = %d, want 0", len(m.blocks))
	}
	if m.hist != hist {
		t.Error("resetTab(true) replaced the input history")
	}
	if m.tabOf(sess) != -1 {
		t.Error("the old session still belongs to a tab")
	}
	if a, b, c := m.globals(); a != model || b != effort || c != mode {
		t.Errorf("globals %s/%s/%s, want %s/%s/%s", a, b, c, model, effort, mode)
	}
	if n := len(m.sess.History()); n != 1 {
		t.Errorf("fresh history = %d entries, want 1", n)
	}
	if files := sessionFiles(t, sess.StartDir()); len(files) != 0 {
		t.Errorf("resetTab wrote session files: %v", files)
	}
	h2 := m.hist
	if err := m.resetTab(false); err != nil {
		t.Fatalf("resetTab(false): %v", err)
	}
	if m.hist == h2 || m.hist == nil {
		t.Error("resetTab(false) kept the input history")
	}
}

func TestZZVerifyT301Busy(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "late", Delay: 30 * time.Second})
	t.Cleanup(srv.Close)
	m, sess := newSessionModel(t, srv)
	m.input.SetValue("slow")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	waitBusy(t, sess)
	if err := m.resetTab(true); err != nil {
		t.Fatalf("resetTab: %v", err)
	}
	waitSession(t, sess)
	if m.turnActive || m.sess == sess || len(m.queue) != 0 || len(m.tabs) != 1 {
		t.Errorf("after a busy reset: turnActive %v, same session %v, queue %d, tabs %d",
			m.turnActive, m.sess == sess, len(m.queue), len(m.tabs))
	}
}

func TestZZVerifyT301Neg(t *testing.T) {
	m, sess := newSessionModelOpts(t, answerServer(t, "a"), "start", func(o *Options) {
		o.NewSession = func() (*agent.Session, error) { return nil, errors.New("boom") }
	})
	m = tabTurn(t, m, "keep")
	n := len(m.blocks)
	err := m.resetTab(true)
	if err == nil || m.sess != sess || len(m.blocks) != n || len(m.tabs) != 1 {
		t.Logf("a failing NewSession changed the tab: err %v, same session %v, blocks %d (was %d)", err, m.sess == sess, len(m.blocks), n)
		return
	}
	t.Fatal("REJECTED: a failing NewSession leaves the tab unchanged")
}
GO

expect_ok "T301 tests" <<'CMD'
go_tests ./internal/tui TestResetTabKeepHistory TestResetTabFresh TestResetTabBusy TestResetTabDeniesApproval TestResetTabNewSessionError TestResetTabKeepsSavedFile
CMD
expect_ok "probe: in-place reset" <<'CMD'
go_probe internal/tui t301 TestZZVerifyT301
CMD
expect_ok "probe: reset of a busy tab ends the old turn" <<'CMD'
go_probe internal/tui t301 TestZZVerifyT301Busy
CMD
expect_fail "negative: a failing NewSession changes the tab" <<'CMD'
go_probe_rejects internal/tui t301 TestZZVerifyT301Neg
CMD
expect_ok "tab tests still pass" <<'CMD'
go test ./internal/tui -run 'Tab|Close|Reset|ExitSave' -count=1
CMD
expect_ok "security files and go.mod unchanged" <<'CMD'
v9_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
