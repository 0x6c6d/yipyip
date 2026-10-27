package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// clickBar left-presses column x of the tab bar.
func clickBar(m Model, x int) (Model, tea.Cmd) {
	next, cmd := m.Update(tea.MouseMsg{X: x, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	return next.(Model), cmd
}

// zoneCol returns the first column of the zone of the given kind and tab.
func zoneCol(t *testing.T, m Model, kind tabZoneKind, tab int) int {
	t.Helper()
	_, zones := m.tabBarLayout()
	for _, z := range zones {
		if z.kind == kind && z.tab == tab {
			return z.start
		}
	}
	t.Fatalf("no zone kind %d tab %d in %q", kind, tab, tabBarLine(m))
	return 0
}

// curPos is the input cursor as line and column.
func curPos(m Model) [2]int {
	return [2]int{m.input.Line(), m.input.LineInfo().ColumnOffset}
}

func TestTabButtons(t *testing.T) {
	m, _ := newSessionModel(t, answerServer(t, "hello"))
	first := m.sess
	m = tabTurn(t, m, "tab one question")
	mustAddTab(t, &m)
	mustAddTab(t, &m)
	third := m.sess

	m, _ = clickBar(m, zoneCol(t, m, tabZoneClose, 0))
	if len(m.tabs) != 2 || m.sess != third || m.tabOf(third) != m.cur {
		t.Fatalf("after closing tab 1: %d tabs, current kept = %v", len(m.tabs), m.sess == third)
	}
	_, raw := readSavedFile(t, first.StartDir())
	if !strings.Contains(raw, "tab one question") || strings.Contains(raw, "SECRET123") {
		t.Errorf("saved file wrong: %s", raw)
	}

	mustAddTab(t, &m) // 3 tabs, the new one current
	m, _ = clickBar(m, zoneCol(t, m, tabZoneClose, 2))
	if len(m.tabs) != 2 || m.cur != 1 {
		t.Fatalf("closing the current tab: %d tabs, cur %d, want 2 and 1", len(m.tabs), m.cur)
	}

	m, _ = clickBar(m, zoneCol(t, m, tabZonePlus, -1))
	if len(m.tabs) != 3 || m.cur != 2 {
		t.Fatalf("plus: %d tabs, cur %d, want 3 and 2", len(m.tabs), m.cur)
	}

	for len(m.tabs) < maxTabs {
		mustAddTab(t, &m)
	}
	if strings.Contains(tabBarLine(m), "+") {
		t.Fatalf("bar with %d tabs has a +: %q", maxTabs, tabBarLine(m))
	}
	m, _ = sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if len(m.tabs) != maxTabs || countInfo(m, tabLimitMsg) != 1 {
		t.Fatalf("at the limit: %d tabs, %d limit lines", len(m.tabs), countInfo(m, tabLimitMsg))
	}
}

func TestTabClickSwitch(t *testing.T) {
	var got []string
	m, _ := newSessionModelOpts(t, answerServer(t, "hello"), "start", func(o *Options) {
		o.Clipboard = func(s string) error { got = append(got, s); return nil }
	})
	mustAddTab(t, &m)
	mustAddTab(t, &m)
	m.input.SetValue("draft")
	cursor := curPos(m)

	line := tabBarLine(m)
	col := func(s string) int { return ansi.StringWidth(line[:strings.Index(line, s)]) }
	m, _ = clickBar(m, col("1:"))
	if m.cur != 0 {
		t.Fatalf("click on 1: -> cur %d", m.cur)
	}
	m, _ = clickBar(m, col("2:"))
	if m.cur != 1 {
		t.Fatalf("click on 2: -> cur %d", m.cur)
	}
	m.input.SetValue("draft")
	cursor = curPos(m)

	_, zones := m.tabBarLayout()
	for _, x := range []int{zones[1].end, ansi.StringWidth(line) + 5} {
		before := m.cur
		m, _ = clickBar(m, x)
		if m.cur != before || len(m.tabs) != 3 {
			t.Errorf("click at gap/end %d changed tabs: cur %d, %d tabs", x, m.cur, len(m.tabs))
		}
	}
	if len(got) != 0 || curPos(m) != cursor {
		t.Errorf("clipboard %q, cursor %v (was %v)", got, curPos(m), cursor)
	}
}
