#!/usr/bin/env bash
# Verification for T302 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"

probe_src t302 <<'GO'
package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/provider/providertest"
)

func TestZZVerifyT302(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "old answer"))
	m = tabTurn(t, m, "old question")
	hist := m.hist
	m.input.SetValue("/clear")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Error("/clear returned no event command for the fresh session")
	}
	if len(m.tabs) != 1 || m.sess == nil || m.sess == sess {
		t.Fatalf("tabs = %d, new session %v", len(m.tabs), m.sess != sess)
	}
	if len(m.blocks) != 1 || countInfo(m, "cleared") != 1 {
		t.Errorf("blocks = %d, cleared lines = %d, want exactly the one info line", len(m.blocks), countInfo(m, "cleared"))
	}
	if strings.Contains(belowTabBar(plainView(m)), "old answer") {
		t.Error("the old answer is still shown")
	}
	if m.hist != hist {
		t.Error("/clear replaced the input history")
	}
	if _, got := histKey(m, tea.KeyUp); got != "/clear" {
		t.Errorf("Up after /clear = %q, want /clear", got)
	}
	if files := sessionFiles(t, sess.StartDir()); len(files) != 0 {
		t.Errorf("/clear wrote session files: %v", files)
	}
	m, _ = m.Command("/new")
	if len(m.tabs) != 2 {
		t.Errorf("/new after /clear: %d tabs, want 2", len(m.tabs))
	}
	for _, l := range strings.Split(HelpText(), "\n") {
		if strings.Contains(l, "/clear") && strings.Contains(l, "alias") {
			t.Errorf("help line still an alias: %q", l)
		}
	}
}

func TestZZVerifyT302Busy(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "late", Delay: 30 * time.Second})
	t.Cleanup(srv.Close)
	m, sess := newSessionModel(t, srv)
	m.input.SetValue("slow")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	waitBusy(t, sess)
	m, _ = m.Command("/clear")
	waitSession(t, sess)
	if len(m.tabs) != 1 || m.sess == sess || countInfo(m, "cleared") != 1 || len(m.blocks) != 1 {
		t.Errorf("busy /clear: tabs %d, same session %v, cleared %d, blocks %d",
			len(m.tabs), m.sess == sess, countInfo(m, "cleared"), len(m.blocks))
	}
}

func TestZZVerifyT302Neg(t *testing.T) {
	m, _ := newSessionModel(t, answerServer(t, "a"))
	m = tabTurn(t, m, "q")
	m, _ = m.Command("/clear")
	if len(m.tabs) != 1 {
		t.Logf("/clear opened a tab: %d tabs", len(m.tabs))
		return
	}
	t.Fatal("REJECTED: /clear keeps the tab count")
}
GO

expect_ok "T302 tests and the reduced /new tests" <<'CMD'
go_tests ./internal/tui TestClearKeepsTab TestClearWithTwoTabs TestClearWhileBusy TestClearKeepsHistory TestClearNoSave TestClearKeepsGlobals TestClearNewStillAddsTab TestClearHelpEntry TestNewClearsSession TestHistorySurvivesNew
CMD
expect_ok "phase 3 exit: -run 'Clear|HistorySurvivesNew|Help'" <<'CMD'
go test ./internal/tui -run 'Clear|HistorySurvivesNew|Help' -count=1 -v
CMD
expect_ok "spec: /help lists /clear without alias" <<'CMD'
grep -n '/clear' internal/tui/help.go | grep -v alias
CMD
expect_ok "probe: /clear idle, history, no save, /new" <<'CMD'
go_probe internal/tui t302 TestZZVerifyT302
CMD
expect_ok "probe: /clear while a turn runs" <<'CMD'
go_probe internal/tui t302 TestZZVerifyT302Busy
CMD
expect_fail "negative: /clear still opens a new tab" <<'CMD'
go_probe_rejects internal/tui t302 TestZZVerifyT302Neg
CMD
expect_fail "negative: help.go still says alias of /new" <<'CMD'
no_match -n -e 'alias of /new' internal/tui/help.go
CMD
expect_ok "security files and go.mod unchanged" <<'CMD'
v9_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
